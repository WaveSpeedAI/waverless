// Package image provides image validation functionality for the Waverless platform.
package image

import (
	"testing"
	"time"

	"waverless/pkg/interfaces"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewImageValidationCache tests the creation of a new cache.
func TestNewImageValidationCache(t *testing.T) {
	cache := NewImageValidationCache()
	require.NotNil(t, cache)
	assert.Equal(t, 0, cache.Size())
	assert.False(t, cache.HasRedis())
}

// TestNewImageValidationCacheWithConfig tests cache creation with custom config.
func TestNewImageValidationCacheWithConfig(t *testing.T) {
	config := &CacheConfig{
		DefaultTTL: 30 * time.Minute,
	}
	cache := NewImageValidationCacheWithConfig(config)
	require.NotNil(t, cache)
	assert.Equal(t, 30*time.Minute, cache.GetConfig().DefaultTTL)
}

// TestImageValidationCache_InMemory tests in-memory cache operations.
func TestImageValidationCache_InMemory(t *testing.T) {
	cache := NewImageValidationCache()

	// Test Set and Get
	result := &interfaces.ImageValidationResult{
		Valid:      true,
		Exists:     true,
		Accessible: true,
		CheckedAt:  time.Now(),
	}

	cache.Set("nginx:latest", result, 1*time.Hour)
	assert.Equal(t, 1, cache.Size())

	// Get should return the cached result
	cached := cache.Get("nginx:latest")
	require.NotNil(t, cached)
	assert.True(t, cached.Valid)
	assert.True(t, cached.Exists)
	assert.True(t, cached.Accessible)

	// Get non-existent key should return nil
	notFound := cache.Get("nonexistent:image")
	assert.Nil(t, notFound)

	// Test Delete
	cache.Delete("nginx:latest")
	assert.Equal(t, 0, cache.Size())
	assert.Nil(t, cache.Get("nginx:latest"))
}

// TestImageValidationCache_Expiration tests TTL-based expiration.
func TestImageValidationCache_Expiration(t *testing.T) {
	cache := NewImageValidationCache()

	result := &interfaces.ImageValidationResult{
		Valid:      true,
		Exists:     true,
		Accessible: true,
		CheckedAt:  time.Now(),
	}

	// Set with very short TTL
	cache.Set("nginx:latest", result, 10*time.Millisecond)

	// Should be available immediately
	cached := cache.Get("nginx:latest")
	require.NotNil(t, cached)

	// Wait for expiration
	time.Sleep(20 * time.Millisecond)

	// Should be expired now
	expired := cache.Get("nginx:latest")
	assert.Nil(t, expired)
}

// TestImageValidationCache_Clear tests clearing the cache.
func TestImageValidationCache_Clear(t *testing.T) {
	cache := NewImageValidationCache()

	// Add multiple items
	result := &interfaces.ImageValidationResult{
		Valid:      true,
		Exists:     true,
		Accessible: true,
		CheckedAt:  time.Now(),
	}

	cache.Set("nginx:latest", result, 1*time.Hour)
	cache.Set("redis:7", result, 1*time.Hour)
	cache.Set("postgres:15", result, 1*time.Hour)

	assert.Equal(t, 3, cache.Size())

	// Clear all
	cache.Clear()
	assert.Equal(t, 0, cache.Size())
}

// TestImageValidationCache_WithRedis tests Redis-backed cache operations.
func TestImageValidationCache_WithRedis(t *testing.T) {
	// Start miniredis for testing
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	// Create Redis client
	redisClient := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	defer redisClient.Close()

	// Create cache with Redis
	cache := NewImageValidationCache().WithRedis(redisClient)
	assert.True(t, cache.HasRedis())

	// Test Set and Get with Redis
	result := &interfaces.ImageValidationResult{
		Valid:      true,
		Exists:     true,
		Accessible: true,
		Error:      "",
		Warning:    "",
		CheckedAt:  time.Now().UTC().Truncate(time.Second),
	}

	cache.Set("nginx:latest", result, 1*time.Hour)

	// Get should return the cached result from Redis
	cached := cache.Get("nginx:latest")
	require.NotNil(t, cached)
	assert.True(t, cached.Valid)
	assert.True(t, cached.Exists)
	assert.True(t, cached.Accessible)

	// Verify it's actually in Redis
	key := generateCacheKey("nginx:latest")
	exists := mr.Exists(key)
	assert.True(t, exists)

	// Test Delete removes from Redis
	cache.Delete("nginx:latest")
	exists = mr.Exists(key)
	assert.False(t, exists)
}

// TestImageValidationCache_RedisFallback tests fallback to memory when Redis fails.
func TestImageValidationCache_RedisFallback(t *testing.T) {
	// Start miniredis
	mr, err := miniredis.Run()
	require.NoError(t, err)

	// Create Redis client
	redisClient := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	defer redisClient.Close()

	// Create cache with Redis
	cache := NewImageValidationCache().WithRedis(redisClient)

	result := &interfaces.ImageValidationResult{
		Valid:      true,
		Exists:     true,
		Accessible: true,
		CheckedAt:  time.Now(),
	}

	// Set value (goes to both Redis and memory)
	cache.Set("nginx:latest", result, 1*time.Hour)

	// Stop Redis to simulate failure
	mr.Close()

	// Get should still work from memory cache
	cached := cache.Get("nginx:latest")
	require.NotNil(t, cached)
	assert.True(t, cached.Valid)
}

// TestImageValidationCache_RedisWithErrorAndWarning tests caching results with error/warning.
func TestImageValidationCache_RedisWithErrorAndWarning(t *testing.T) {
	// Start miniredis
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	redisClient := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	defer redisClient.Close()

	cache := NewImageValidationCache().WithRedis(redisClient)

	// Test with error message
	result := &interfaces.ImageValidationResult{
		Valid:      true,
		Exists:     false,
		Accessible: false,
		Error:      "image not found",
		Warning:    "please check image name",
		CheckedAt:  time.Now().UTC().Truncate(time.Second),
	}

	cache.Set("nonexistent:image", result, 1*time.Hour)

	cached := cache.Get("nonexistent:image")
	require.NotNil(t, cached)
	assert.True(t, cached.Valid)
	assert.False(t, cached.Exists)
	assert.False(t, cached.Accessible)
	assert.Equal(t, "image not found", cached.Error)
	assert.Equal(t, "please check image name", cached.Warning)
}

// TestImageValidationCache_RedisTTL tests that Redis TTL is set correctly.
func TestImageValidationCache_RedisTTL(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	redisClient := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	defer redisClient.Close()

	cache := NewImageValidationCache().WithRedis(redisClient)

	result := &interfaces.ImageValidationResult{
		Valid:      true,
		Exists:     true,
		Accessible: true,
		CheckedAt:  time.Now(),
	}

	// Set with 1 hour TTL
	cache.Set("nginx:latest", result, 1*time.Hour)

	// Check TTL in Redis (miniredis stores TTL)
	key := generateCacheKey("nginx:latest")
	ttl := mr.TTL(key)
	assert.True(t, ttl > 0, "TTL should be set")
	assert.True(t, ttl <= 1*time.Hour, "TTL should be <= 1 hour")
}

// TestImageValidationCache_DefaultTTL tests that default TTL is used when 0 is passed.
func TestImageValidationCache_DefaultTTL(t *testing.T) {
	config := &CacheConfig{
		DefaultTTL: 2 * time.Hour,
	}
	cache := NewImageValidationCacheWithConfig(config)

	result := &interfaces.ImageValidationResult{
		Valid:      true,
		Exists:     true,
		Accessible: true,
		CheckedAt:  time.Now(),
	}

	// Set with 0 TTL (should use default)
	cache.Set("nginx:latest", result, 0)

	// Should still be cached
	cached := cache.Get("nginx:latest")
	require.NotNil(t, cached)
}

// TestGenerateCacheKey tests the cache key generation.
func TestGenerateCacheKey(t *testing.T) {
	tests := []struct {
		name  string
		image string
	}{
		{"simple image", "nginx"},
		{"image with tag", "nginx:latest"},
		{"image with registry", "gcr.io/project/image:tag"},
		{"image with special chars", "registry.example.com:5000/namespace/image:v1.0.0"},
		{"image with digest", "nginx@sha256:abc123def456"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := generateCacheKey(tt.image)

			// Key should have the correct prefix
			assert.True(t, len(key) > len(cacheKeyPrefix))
			assert.Contains(t, key, cacheKeyPrefix)

			// Key should be deterministic
			key2 := generateCacheKey(tt.image)
			assert.Equal(t, key, key2)

			// Different images should have different keys
			differentKey := generateCacheKey(tt.image + "-different")
			assert.NotEqual(t, key, differentKey)
		})
	}
}

// TestImageValidationCache_ClearWithRedis tests clearing cache including Redis.
func TestImageValidationCache_ClearWithRedis(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	redisClient := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	defer redisClient.Close()

	cache := NewImageValidationCache().WithRedis(redisClient)

	result := &interfaces.ImageValidationResult{
		Valid:      true,
		Exists:     true,
		Accessible: true,
		CheckedAt:  time.Now(),
	}

	// Add multiple items
	cache.Set("nginx:latest", result, 1*time.Hour)
	cache.Set("redis:7", result, 1*time.Hour)
	cache.Set("postgres:15", result, 1*time.Hour)

	assert.Equal(t, 3, cache.Size())

	// Clear all
	cache.Clear()
	assert.Equal(t, 0, cache.Size())

	// Verify Redis is also cleared
	for _, image := range []string{"nginx:latest", "redis:7", "postgres:15"} {
		key := generateCacheKey(image)
		exists := mr.Exists(key)
		assert.False(t, exists, "Key %s should be deleted from Redis", key)
	}
}
