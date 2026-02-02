// Package image provides image validation functionality for the Waverless platform.
package image

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"

	"waverless/pkg/interfaces"

	"github.com/go-redis/redis/v8"
)

const (
	// DefaultCacheDuration is the default TTL for cached validation results (1 hour)
	DefaultCacheDuration = 1 * time.Hour

	// cacheKeyPrefix is the prefix for Redis cache keys
	cacheKeyPrefix = "image:validation:"
)

// CacheConfig contains configuration for the image validation cache.
type CacheConfig struct {
	// DefaultTTL is the default TTL for cached validation results
	DefaultTTL time.Duration
}

// DefaultCacheConfig returns the default cache configuration.
func DefaultCacheConfig() *CacheConfig {
	return &CacheConfig{
		DefaultTTL: DefaultCacheDuration,
	}
}

// ImageValidationCache provides caching for image validation results.
// It supports both Redis-based caching (primary) and in-memory caching (fallback).
// The cache is thread-safe and supports TTL-based expiration.
//
// Cache key format: image:validation:{sha256(image)}
// This ensures consistent key length and avoids special characters in keys.
//
// Validates: Requirements 2.5
type ImageValidationCache struct {
	// In-memory cache (fallback when Redis is unavailable)
	mu    sync.RWMutex
	items map[string]*cacheItem

	// Redis client (optional, for distributed caching)
	redisClient *redis.Client

	// Configuration
	config *CacheConfig
}

// cacheItem represents a cached validation result with expiration.
type cacheItem struct {
	result    *interfaces.ImageValidationResult
	expiresAt time.Time
}

// redisCacheValue represents the JSON structure stored in Redis.
// This matches the design document specification.
type redisCacheValue struct {
	Valid      bool      `json:"valid"`
	Exists     bool      `json:"exists"`
	Accessible bool      `json:"accessible"`
	Error      string    `json:"error,omitempty"`
	Warning    string    `json:"warning,omitempty"`
	CheckedAt  time.Time `json:"checkedAt"`
}

// NewImageValidationCache creates a new ImageValidationCache with in-memory storage only.
// Use WithRedis to add Redis support.
func NewImageValidationCache() *ImageValidationCache {
	cache := &ImageValidationCache{
		items:  make(map[string]*cacheItem),
		config: DefaultCacheConfig(),
	}

	// Start background cleanup goroutine for in-memory cache
	go cache.cleanup()

	return cache
}

// NewImageValidationCacheWithConfig creates a new ImageValidationCache with custom configuration.
func NewImageValidationCacheWithConfig(config *CacheConfig) *ImageValidationCache {
	if config == nil {
		config = DefaultCacheConfig()
	}

	cache := &ImageValidationCache{
		items:  make(map[string]*cacheItem),
		config: config,
	}

	// Start background cleanup goroutine for in-memory cache
	go cache.cleanup()

	return cache
}

// WithRedis configures the cache to use Redis as the primary storage.
// The in-memory cache is used as a fallback when Redis is unavailable.
// Returns the cache instance for method chaining.
func (c *ImageValidationCache) WithRedis(client *redis.Client) *ImageValidationCache {
	c.redisClient = client
	return c
}

// generateCacheKey generates a cache key for the given image.
// Uses SHA256 hash to ensure consistent key length and avoid special characters.
// Format: image:validation:{sha256(image)}
func generateCacheKey(image string) string {
	hash := sha256.Sum256([]byte(image))
	return cacheKeyPrefix + hex.EncodeToString(hash[:])
}

// Get retrieves a cached validation result for the given image.
// It first tries Redis (if configured), then falls back to in-memory cache.
// Returns nil if the image is not in the cache or has expired.
//
// Validates: Requirements 2.5
func (c *ImageValidationCache) Get(image string) *interfaces.ImageValidationResult {
	// Try Redis first if available
	if c.redisClient != nil {
		result := c.getFromRedis(image)
		if result != nil {
			return result
		}
	}

	// Fall back to in-memory cache
	return c.getFromMemory(image)
}

// getFromRedis retrieves a cached validation result from Redis.
// Returns nil if not found, expired, or on error.
func (c *ImageValidationCache) getFromRedis(image string) *interfaces.ImageValidationResult {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	key := generateCacheKey(image)
	data, err := c.redisClient.Get(ctx, key).Bytes()
	if err != nil {
		// Key not found or Redis error - fall back to memory cache
		return nil
	}

	var cached redisCacheValue
	if err := json.Unmarshal(data, &cached); err != nil {
		// Invalid JSON - delete the corrupted entry
		_ = c.redisClient.Del(ctx, key)
		return nil
	}

	return &interfaces.ImageValidationResult{
		Valid:      cached.Valid,
		Exists:     cached.Exists,
		Accessible: cached.Accessible,
		Error:      cached.Error,
		Warning:    cached.Warning,
		CheckedAt:  cached.CheckedAt,
	}
}

// getFromMemory retrieves a cached validation result from in-memory cache.
// Returns nil if not found or expired.
func (c *ImageValidationCache) getFromMemory(image string) *interfaces.ImageValidationResult {
	c.mu.RLock()
	defer c.mu.RUnlock()

	item, ok := c.items[image]
	if !ok {
		return nil
	}

	// Check if expired
	if time.Now().After(item.expiresAt) {
		return nil
	}

	return item.result
}

// Set stores a validation result in the cache with the given TTL.
// It stores in both Redis (if configured) and in-memory cache.
// If ttl is 0, the default TTL from configuration is used.
//
// Validates: Requirements 2.5
func (c *ImageValidationCache) Set(image string, result *interfaces.ImageValidationResult, ttl time.Duration) {
	if ttl == 0 {
		ttl = c.config.DefaultTTL
	}

	// Store in Redis if available
	if c.redisClient != nil {
		c.setInRedis(image, result, ttl)
	}

	// Always store in memory cache as fallback
	c.setInMemory(image, result, ttl)
}

// setInRedis stores a validation result in Redis.
func (c *ImageValidationCache) setInRedis(image string, result *interfaces.ImageValidationResult, ttl time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	key := generateCacheKey(image)
	value := redisCacheValue{
		Valid:      result.Valid,
		Exists:     result.Exists,
		Accessible: result.Accessible,
		Error:      result.Error,
		Warning:    result.Warning,
		CheckedAt:  result.CheckedAt,
	}

	data, err := json.Marshal(value)
	if err != nil {
		// JSON marshal error - skip Redis, use memory cache only
		return
	}

	// Set with TTL - ignore errors, memory cache is the fallback
	_ = c.redisClient.Set(ctx, key, data, ttl).Err()
}

// setInMemory stores a validation result in the in-memory cache.
func (c *ImageValidationCache) setInMemory(image string, result *interfaces.ImageValidationResult, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items[image] = &cacheItem{
		result:    result,
		expiresAt: time.Now().Add(ttl),
	}
}

// Delete removes an image from both Redis and in-memory cache.
func (c *ImageValidationCache) Delete(image string) {
	// Delete from Redis if available
	if c.redisClient != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		key := generateCacheKey(image)
		_ = c.redisClient.Del(ctx, key)
	}

	// Delete from memory cache
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, image)
}

// Clear removes all items from both Redis (with prefix) and in-memory cache.
// Note: This only clears items with the image validation prefix in Redis.
func (c *ImageValidationCache) Clear() {
	// Clear Redis entries with our prefix if available
	if c.redisClient != nil {
		c.clearRedis()
	}

	// Clear memory cache
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]*cacheItem)
}

// clearRedis removes all image validation entries from Redis.
func (c *ImageValidationCache) clearRedis() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Use SCAN to find and delete keys with our prefix
	// This is safer than KEYS for production use
	var cursor uint64
	for {
		keys, nextCursor, err := c.redisClient.Scan(ctx, cursor, cacheKeyPrefix+"*", 100).Result()
		if err != nil {
			return
		}

		if len(keys) > 0 {
			_ = c.redisClient.Del(ctx, keys...).Err()
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
}

// cleanup periodically removes expired items from the in-memory cache.
func (c *ImageValidationCache) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		c.removeExpired()
	}
}

// removeExpired removes all expired items from the in-memory cache.
// Redis handles expiration automatically via TTL.
func (c *ImageValidationCache) removeExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for key, item := range c.items {
		if now.After(item.expiresAt) {
			delete(c.items, key)
		}
	}
}

// Size returns the number of items in the in-memory cache.
// Note: This does not include Redis entries.
func (c *ImageValidationCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.items)
}

// HasRedis returns true if Redis is configured for this cache.
func (c *ImageValidationCache) HasRedis() bool {
	return c.redisClient != nil
}

// GetConfig returns the cache configuration.
func (c *ImageValidationCache) GetConfig() *CacheConfig {
	return c.config
}
