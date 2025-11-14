package autoscaler

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
)

func TestDistributedLock_SingleInstance(t *testing.T) {
	// Setup mini redis
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	defer client.Close()

	lock := NewRedisDistributedLock(client, "test-lock")
	ctx := context.Background()

	// Test acquiring lock
	acquired, err := lock.TryLock(ctx)
	assert.NoError(t, err)
	assert.True(t, acquired)
	assert.True(t, lock.IsHeld())

	// Test unlocking
	err = lock.Unlock(ctx)
	assert.NoError(t, err)
	assert.False(t, lock.IsHeld())
}

func TestDistributedLock_MultipleInstances(t *testing.T) {
	// Setup mini redis
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	defer client.Close()

	lock1 := NewRedisDistributedLock(client, "test-lock-multi")
	lock2 := NewRedisDistributedLock(client, "test-lock-multi")
	ctx := context.Background()

	// Lock 1 acquires the lock
	acquired1, err := lock1.TryLock(ctx)
	assert.NoError(t, err)
	assert.True(t, acquired1)

	// Lock 2 tries to acquire (should fail)
	acquired2, err := lock2.TryLock(ctx)
	assert.NoError(t, err)
	assert.False(t, acquired2, "second lock should not be acquired")

	// Release lock 1
	err = lock1.Unlock(ctx)
	assert.NoError(t, err)

	// Now lock 2 should be able to acquire
	acquired2, err = lock2.TryLock(ctx)
	assert.NoError(t, err)
	assert.True(t, acquired2, "second lock should be acquired after first release")

	err = lock2.Unlock(ctx)
	assert.NoError(t, err)
}

func TestDistributedLock_AutoExpire(t *testing.T) {
	// Setup mini redis
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	defer client.Close()

	lock1 := NewRedisDistributedLock(client, "test-lock-expire")
	lock2 := NewRedisDistributedLock(client, "test-lock-expire")
	ctx := context.Background()

	// Lock 1 acquires
	acquired1, err := lock1.TryLock(ctx)
	assert.NoError(t, err)
	assert.True(t, acquired1)

	// Fast forward time in miniredis to simulate TTL expiration
	mr.FastForward(lockTTL + time.Second)

	// Lock 2 should now be able to acquire
	acquired2, err := lock2.TryLock(ctx)
	assert.NoError(t, err)
	assert.True(t, acquired2, "lock should be available after TTL expiration")

	err = lock2.Unlock(ctx)
	assert.NoError(t, err)
}

func TestDistributedLock_NilClient(t *testing.T) {
	// Test graceful degradation when Redis is not available
	lock := NewRedisDistributedLock(nil, "test-lock-nil")
	ctx := context.Background()

	// Should still work (single-instance mode)
	acquired, err := lock.TryLock(ctx)
	assert.NoError(t, err)
	assert.True(t, acquired)
	assert.True(t, lock.IsHeld())

	err = lock.Unlock(ctx)
	assert.NoError(t, err)
	assert.False(t, lock.IsHeld())
}

func TestDistributedLock_PreventDoubleLock(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	defer client.Close()

	lock1 := NewRedisDistributedLock(client, "test-lock-double")
	lock2 := NewRedisDistributedLock(client, "test-lock-double")
	ctx := context.Background()

	// Both try to acquire simultaneously
	acquired1, err1 := lock1.TryLock(ctx)
	acquired2, err2 := lock2.TryLock(ctx)

	assert.NoError(t, err1)
	assert.NoError(t, err2)

	// Exactly one should succeed
	assert.True(t, acquired1 != acquired2, "exactly one lock should be acquired")

	// Clean up
	if acquired1 {
		lock1.Unlock(ctx)
	}
	if acquired2 {
		lock2.Unlock(ctx)
	}
}
