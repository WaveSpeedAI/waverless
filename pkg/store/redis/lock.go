package redis

import (
	"context"
	"fmt"
	"sync"
	"time"

	"waverless/pkg/logger"

	"github.com/go-redis/redis/v8"
)

// Distributed lock related constants
const (
	DefaultLockTTL             = 30 * time.Second // Lock TTL to prevent deadlocks
	DefaultLockAcquireTimeout  = 5 * time.Second  // Timeout for acquiring lock
	DefaultLockExtendInterval  = 10 * time.Second // Lock renewal interval
	DefaultMaxLockHoldDuration = 2 * time.Minute  // Maximum lock hold duration
)

// DistributedLock is the distributed lock interface
type DistributedLock interface {
	// TryLock attempts to acquire the lock
	TryLock(ctx context.Context) (bool, error)

	// Unlock releases the lock
	Unlock(ctx context.Context) error

	// IsHeld checks if the lock is held
	IsHeld() bool
}

// LockOptions is the lock configuration options
type LockOptions struct {
	TTL             time.Duration // Lock TTL
	AcquireTimeout  time.Duration // Timeout for acquiring lock
	ExtendInterval  time.Duration // Lock renewal interval
	MaxHoldDuration time.Duration // Maximum lock hold duration
}

// DefaultLockOptions returns default lock configuration
func DefaultLockOptions() *LockOptions {
	return &LockOptions{
		TTL:             DefaultLockTTL,
		AcquireTimeout:  DefaultLockAcquireTimeout,
		ExtendInterval:  DefaultLockExtendInterval,
		MaxHoldDuration: DefaultMaxLockHoldDuration,
	}
}

// LockStore is the distributed lock storage
type LockStore struct {
	client *redis.Client
}

// NewLockStore creates a distributed lock storage
func NewLockStore(client *redis.Client) *LockStore {
	return &LockStore{client: client}
}

// NewLock creates a new distributed lock
// lockKey: the key name for the lock, used to distinguish different locks (e.g., "autoscaler:global-lock", "cleanup:worker-lock")
func (s *LockStore) NewLock(lockKey string, opts *LockOptions) DistributedLock {
	if opts == nil {
		opts = DefaultLockOptions()
	}
	return &redisDistributedLock{
		client:          s.client,
		lockKey:         lockKey,
		lockValue:       fmt.Sprintf("%s-%d-%d", lockKey, time.Now().UnixNano(), randomInt()),
		ttl:             opts.TTL,
		acquireTimeout:  opts.AcquireTimeout,
		extendInterval:  opts.ExtendInterval,
		maxHoldDuration: opts.MaxHoldDuration,
		isHeld:          false,
		stopRenew:       make(chan struct{}),
	}
}

// redisDistributedLock is the Redis distributed lock implementation
type redisDistributedLock struct {
	client          *redis.Client
	lockKey         string
	lockValue       string // Unique identifier to prevent releasing other instance's lock
	ttl             time.Duration
	acquireTimeout  time.Duration
	extendInterval  time.Duration
	maxHoldDuration time.Duration
	isHeld          bool
	acquiredAt      time.Time
	stopRenew       chan struct{}
	renewStopped    bool       // Flag to indicate if renewal has stopped, prevents closing channel twice
	mu              sync.Mutex // Protects concurrent access
}

// TryLock attempts to acquire the lock (with timeout)
func (l *redisDistributedLock) TryLock(ctx context.Context) (bool, error) {
	if l.client == nil {
		logger.Warn("redis client is nil, skipping distributed lock (running in single-instance mode)")
		l.isHeld = true
		return true, nil
	}

	// Use context with timeout
	acquireCtx, cancel := context.WithTimeout(ctx, l.acquireTimeout)
	defer cancel()

	// Try to acquire lock (using SET NX EX)
	acquired, err := l.client.SetNX(acquireCtx, l.lockKey, l.lockValue, l.ttl).Result()
	if err != nil {
		return false, fmt.Errorf("failed to acquire lock: %w", err)
	}

	if acquired {
		l.mu.Lock()
		l.isHeld = true
		l.acquiredAt = time.Now()

		// Create new stopRenew channel each time lock is acquired
		// This supports multiple TryLock/Unlock cycles
		l.stopRenew = make(chan struct{})
		l.renewStopped = false
		l.mu.Unlock()

		// Start lock renewal goroutine
		go l.renewLock(ctx)

		logger.DebugCtx(ctx, "distributed lock acquired successfully: %s", l.lockKey)
		return true, nil
	}

	logger.DebugCtx(ctx, "distributed lock already held by another instance: %s", l.lockKey)
	return false, nil
}

// Unlock releases the lock
func (l *redisDistributedLock) Unlock(ctx context.Context) error {
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

	// Safely stop renewal goroutine, prevent closing channel twice
	if !l.renewStopped {
		l.renewStopped = true
		close(l.stopRenew)
	}
	l.mu.Unlock()

	// Use Lua script to ensure only deleting own lock
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
		logger.DebugCtx(ctx, "distributed lock released successfully: %s", l.lockKey)
	} else {
		logger.WarnCtx(ctx, "lock was already released or held by another instance: %s", l.lockKey)
	}

	return nil
}

// IsHeld checks if the lock is held
func (l *redisDistributedLock) IsHeld() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.isHeld
}

// renewLock automatically renews the lock (background goroutine)
func (l *redisDistributedLock) renewLock(ctx context.Context) {
	ticker := time.NewTicker(l.extendInterval)
	defer ticker.Stop()

	for {
		select {
		case <-l.stopRenew:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Check if lock has been held for too long
			l.mu.Lock()
			holdDuration := time.Since(l.acquiredAt)
			l.mu.Unlock()

			if holdDuration > l.maxHoldDuration {
				logger.WarnCtx(ctx, "lock held for too long (%.0f seconds), will be released: %s",
					holdDuration.Seconds(), l.lockKey)
				// Don't call Unlock in renewal goroutine to avoid closing channel twice
				// Just mark lock as not held, let defer Unlock handle it
				l.mu.Lock()
				l.isHeld = false
				l.mu.Unlock()
				return
			}

			// Use Lua script to renew (only renew own lock)
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
				logger.WarnCtx(ctx, "failed to renew lock %s: %v", l.lockKey, err)
				l.mu.Lock()
				l.isHeld = false
				l.mu.Unlock()
				return
			}

			if result.(int64) == 0 {
				logger.WarnCtx(ctx, "lock renewal failed, lock lost: %s", l.lockKey)
				l.mu.Lock()
				l.isHeld = false
				l.mu.Unlock()
				return
			}

			logger.DebugCtx(ctx, "distributed lock renewed: %s", l.lockKey)
		}
	}
}

// randomInt generates a random integer (simple implementation)
func randomInt() int64 {
	return time.Now().UnixNano() % 1000000
}
