package autoscaler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"waverless/pkg/logger"
)

const (
	// 分布式锁相关常量
	autoscalerLockKey     = "autoscaler:global-lock"
	lockTTL               = 30 * time.Second // 锁的 TTL，防止死锁
	lockAcquireTimeout    = 5 * time.Second  // 获取锁的超时时间
	lockExtendInterval    = 10 * time.Second // 锁续期间隔
	maxLockHoldDuration   = 2 * time.Minute  // 最大持有锁时间
)

// DistributedLock 分布式锁接口
type DistributedLock interface {
	// TryLock 尝试获取锁
	TryLock(ctx context.Context) (bool, error)

	// Unlock 释放锁
	Unlock(ctx context.Context) error

	// IsHeld 检查是否持有锁
	IsHeld() bool
}

// RedisDistributedLock Redis 分布式锁实现
type RedisDistributedLock struct {
	client       *redis.Client
	lockKey      string
	lockValue    string // 唯一标识，防止释放其他实例的锁
	ttl          time.Duration
	isHeld       bool
	acquiredAt   time.Time
	stopRenew    chan struct{}
	renewStopped bool // 标记续期是否已停止，防止重复关闭 channel
	mu           sync.Mutex // 保护并发访问
}

// NewRedisDistributedLock 创建 Redis 分布式锁
// lockKey: 锁的键名，用于区分不同的锁（如 "autoscaler:global-lock", "cleanup:worker-lock"）
func NewRedisDistributedLock(client *redis.Client, lockKey string) *RedisDistributedLock {
	if lockKey == "" {
		lockKey = autoscalerLockKey // 默认使用 autoscaler 锁
	}
	return &RedisDistributedLock{
		client:    client,
		lockKey:   lockKey,
		lockValue: fmt.Sprintf("%s-%d-%d", lockKey, time.Now().UnixNano(), randomInt()),
		ttl:       lockTTL,
		isHeld:    false,
		stopRenew: make(chan struct{}),
	}
}

// TryLock 尝试获取锁（带超时）
func (l *RedisDistributedLock) TryLock(ctx context.Context) (bool, error) {
	if l.client == nil {
		logger.Warn("redis client is nil, skipping distributed lock (running in single-instance mode)")
		l.isHeld = true
		return true, nil
	}

	// 使用带超时的 context
	acquireCtx, cancel := context.WithTimeout(ctx, lockAcquireTimeout)
	defer cancel()

	// 尝试获取锁（使用 SET NX EX）
	acquired, err := l.client.SetNX(acquireCtx, l.lockKey, l.lockValue, l.ttl).Result()
	if err != nil {
		return false, fmt.Errorf("failed to acquire lock: %w", err)
	}

	if acquired {
		l.mu.Lock()
		l.isHeld = true
		l.acquiredAt = time.Now()
		
		// 🔥 CRITICAL FIX: 每次获取锁时创建新的 stopRenew channel
		// 这样可以支持多次 TryLock/Unlock 循环
		l.stopRenew = make(chan struct{})
		l.renewStopped = false
		l.mu.Unlock()

		// 启动锁续期协程
		go l.renewLock(ctx)

		logger.DebugCtx(ctx, "autoscaler lock acquired successfully")
		return true, nil
	}

	logger.DebugCtx(ctx, "autoscaler lock already held by another instance")
	return false, nil
}

// Unlock 释放锁
func (l *RedisDistributedLock) Unlock(ctx context.Context) error {
	l.mu.Lock()
	if !l.isHeld {
		l.mu.Unlock()
		return nil
	}

	if l.client == nil {
		l.isHeld = false
		l.mu.Unlock()
		return nil
	}

	// 🔥 CRITICAL FIX: 安全地停止续期协程，防止重复关闭 channel
	if !l.renewStopped {
		l.renewStopped = true
		close(l.stopRenew)
	}
	l.mu.Unlock()

	// 使用 Lua 脚本确保只删除自己的锁
	luaScript := `
		if redis.call("get", KEYS[1]) == ARGV[1] then
			return redis.call("del", KEYS[1])
		else
			return 0
		end
	`

	result, err := l.client.Eval(ctx, luaScript, []string{l.lockKey}, l.lockValue).Result()
	if err != nil {
		return fmt.Errorf("failed to release lock: %w", err)
	}

	l.mu.Lock()
	l.isHeld = false
	l.mu.Unlock()

	if result.(int64) == 1 {
		logger.DebugCtx(ctx, "autoscaler lock released successfully")
	} else {
		logger.WarnCtx(ctx, "lock was already released or held by another instance")
	}

	return nil
}

// IsHeld 检查是否持有锁
func (l *RedisDistributedLock) IsHeld() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.isHeld
}

// renewLock 自动续期锁（后台协程）
func (l *RedisDistributedLock) renewLock(ctx context.Context) {
	ticker := time.NewTicker(lockExtendInterval)
	defer ticker.Stop()

	for {
		select {
		case <-l.stopRenew:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			// 检查是否已经持有锁太久
			l.mu.Lock()
			holdDuration := time.Since(l.acquiredAt)
			l.mu.Unlock()
			
			if holdDuration > maxLockHoldDuration {
				logger.WarnCtx(ctx, "lock held for too long (%.0f seconds), will be released by main goroutine",
					holdDuration.Seconds())
				// 🔥 CRITICAL FIX: 不要在续期协程中调用 Unlock，避免重复关闭 channel
				// 只标记锁为未持有，让 defer 中的 Unlock 处理
				l.mu.Lock()
				l.isHeld = false
				l.mu.Unlock()
				return
			}

			// 使用 Lua 脚本续期（只续期自己的锁）
			luaScript := `
				if redis.call("get", KEYS[1]) == ARGV[1] then
					return redis.call("expire", KEYS[1], ARGV[2])
				else
					return 0
				end
			`

			result, err := l.client.Eval(ctx, luaScript,
				[]string{l.lockKey},
				l.lockValue,
				int(l.ttl.Seconds())).Result()

			if err != nil {
				logger.WarnCtx(ctx, "failed to renew lock: %v", err)
				l.mu.Lock()
				l.isHeld = false
				l.mu.Unlock()
				return
			}

			if result.(int64) == 0 {
				logger.WarnCtx(ctx, "lock renewal failed, lock lost")
				l.mu.Lock()
				l.isHeld = false
				l.mu.Unlock()
				return
			}

			logger.DebugCtx(ctx, "autoscaler lock renewed")
		}
	}
}

// randomInt 生成随机整数（简单实现）
func randomInt() int64 {
	return time.Now().UnixNano() % 1000000
}
