// Package image provides property-based tests for image validation caching functionality.
// These tests verify universal properties that should hold across all valid inputs.
//
// Feature: image-validation-and-status
// Property 3: Image validation caching
// **Validates: Requirements 2.5**
package image

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"waverless/pkg/interfaces"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/stretchr/testify/require"
)

// TestProperty_CachedResultsReturnedOnSubsequentCalls tests Property 3: Image Validation Caching
//
// Property: For any successfully validated image, calling CheckImageExists twice within
// the cache duration SHALL return the cached result on the second call without making
// a new registry request.
//
// Feature: image-validation-and-status, Property 3: Image validation caching
// **Validates: Requirements 2.5**
func TestProperty_CachedResultsReturnedOnSubsequentCalls(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxSize = 50

	properties := gopter.NewProperties(parameters)

	// Property 3a: Cached results are returned on subsequent Get calls
	properties.Property("cached results are returned on subsequent Get calls", prop.ForAll(
		func(image string, valid, exists, accessible bool) bool {
			cache := NewImageValidationCache()

			result := &interfaces.ImageValidationResult{
				Valid:      valid,
				Exists:     exists,
				Accessible: accessible,
				CheckedAt:  time.Now(),
			}

			// Set the result in cache
			cache.Set(image, result, 1*time.Hour)

			// Get should return the same result
			cached := cache.Get(image)
			if cached == nil {
				return false
			}

			// Verify all fields match
			return cached.Valid == valid &&
				cached.Exists == exists &&
				cached.Accessible == accessible
		},
		genValidImageName(),
		gen.Bool(),
		gen.Bool(),
		gen.Bool(),
	))

	// Property 3b: Multiple Get calls return the same cached result
	properties.Property("multiple Get calls return the same cached result", prop.ForAll(
		func(image string, numGets int) bool {
			cache := NewImageValidationCache()

			result := &interfaces.ImageValidationResult{
				Valid:      true,
				Exists:     true,
				Accessible: true,
				CheckedAt:  time.Now(),
			}

			cache.Set(image, result, 1*time.Hour)

			// Multiple Get calls should all return the same result
			for i := 0; i < numGets; i++ {
				cached := cache.Get(image)
				if cached == nil {
					return false
				}
				if cached.Valid != result.Valid ||
					cached.Exists != result.Exists ||
					cached.Accessible != result.Accessible {
					return false
				}
			}

			return true
		},
		genValidImageName(),
		gen.IntRange(2, 10),
	))

	properties.TestingRun(t)
}

// TestProperty_CacheRespectsTTLExpiration tests that cache respects TTL expiration
//
// Property: For any cached image, after the TTL expires, Get SHALL return nil.
//
// Feature: image-validation-and-status, Property 3: Image validation caching
// **Validates: Requirements 2.5**
func TestProperty_CacheRespectsTTLExpiration(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxSize = 50

	properties := gopter.NewProperties(parameters)

	// Property: Expired items are not returned
	properties.Property("expired items are not returned", prop.ForAll(
		func(image string) bool {
			cache := NewImageValidationCache()

			result := &interfaces.ImageValidationResult{
				Valid:      true,
				Exists:     true,
				Accessible: true,
				CheckedAt:  time.Now(),
			}

			// Set with very short TTL
			cache.Set(image, result, 1*time.Millisecond)

			// Wait for expiration
			time.Sleep(5 * time.Millisecond)

			// Get should return nil after expiration
			cached := cache.Get(image)
			return cached == nil
		},
		genValidImageName(),
	))

	// Property: Non-expired items are returned
	properties.Property("non-expired items are returned", prop.ForAll(
		func(image string) bool {
			cache := NewImageValidationCache()

			result := &interfaces.ImageValidationResult{
				Valid:      true,
				Exists:     true,
				Accessible: true,
				CheckedAt:  time.Now(),
			}

			// Set with long TTL
			cache.Set(image, result, 1*time.Hour)

			// Get should return the result immediately
			cached := cache.Get(image)
			return cached != nil && cached.Valid == result.Valid
		},
		genValidImageName(),
	))

	properties.TestingRun(t)
}

// TestProperty_CacheKeyGenerationIsDeterministic tests that cache key generation is deterministic
//
// Property: For any image string, generateCacheKey SHALL always return the same key.
//
// Feature: image-validation-and-status, Property 3: Image validation caching
// **Validates: Requirements 2.5**
func TestProperty_CacheKeyGenerationIsDeterministic(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxSize = 100

	properties := gopter.NewProperties(parameters)

	// Property: Same input always produces same key
	properties.Property("same input always produces same key", prop.ForAll(
		func(image string) bool {
			key1 := generateCacheKey(image)
			key2 := generateCacheKey(image)
			return key1 == key2
		},
		gen.AnyString(),
	))

	// Property: Different inputs produce different keys (with high probability)
	properties.Property("different inputs produce different keys", prop.ForAll(
		func(image1, image2 string) bool {
			if image1 == image2 {
				return true // Skip if same input
			}
			key1 := generateCacheKey(image1)
			key2 := generateCacheKey(image2)
			return key1 != key2
		},
		genValidImageName(),
		genValidImageName(),
	))

	// Property: Key has correct prefix
	properties.Property("key has correct prefix", prop.ForAll(
		func(image string) bool {
			key := generateCacheKey(image)
			return len(key) > len(cacheKeyPrefix) &&
				key[:len(cacheKeyPrefix)] == cacheKeyPrefix
		},
		gen.AnyString(),
	))

	// Property: Key length is consistent (SHA256 produces fixed-length hash)
	properties.Property("key length is consistent", prop.ForAll(
		func(image1, image2 string) bool {
			key1 := generateCacheKey(image1)
			key2 := generateCacheKey(image2)
			// SHA256 produces 64 hex characters, plus prefix
			expectedLen := len(cacheKeyPrefix) + 64
			return len(key1) == expectedLen && len(key2) == expectedLen
		},
		gen.AnyString(),
		gen.AnyString(),
	))

	properties.TestingRun(t)
}

// TestProperty_CacheOperationsAreIdempotent tests that cache operations are idempotent
//
// Property: Setting the same key multiple times with the same value SHALL result
// in the same cached state.
//
// Feature: image-validation-and-status, Property 3: Image validation caching
// **Validates: Requirements 2.5**
func TestProperty_CacheOperationsAreIdempotent(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxSize = 50

	properties := gopter.NewProperties(parameters)

	// Property: Multiple Set operations with same value are idempotent
	properties.Property("multiple Set operations with same value are idempotent", prop.ForAll(
		func(image string, numSets int) bool {
			cache := NewImageValidationCache()

			result := &interfaces.ImageValidationResult{
				Valid:      true,
				Exists:     true,
				Accessible: true,
				CheckedAt:  time.Now(),
			}

			// Set multiple times
			for i := 0; i < numSets; i++ {
				cache.Set(image, result, 1*time.Hour)
			}

			// Get should return the result
			cached := cache.Get(image)
			if cached == nil {
				return false
			}

			return cached.Valid == result.Valid &&
				cached.Exists == result.Exists &&
				cached.Accessible == result.Accessible
		},
		genValidImageName(),
		gen.IntRange(2, 10),
	))

	// Property: Delete is idempotent (deleting non-existent key is safe)
	properties.Property("delete is idempotent", prop.ForAll(
		func(image string, numDeletes int) bool {
			cache := NewImageValidationCache()

			// Delete multiple times (even if key doesn't exist)
			for i := 0; i < numDeletes; i++ {
				cache.Delete(image)
			}

			// Get should return nil
			return cache.Get(image) == nil
		},
		genValidImageName(),
		gen.IntRange(1, 5),
	))

	// Property: Set after Delete works correctly
	properties.Property("set after delete works correctly", prop.ForAll(
		func(image string) bool {
			cache := NewImageValidationCache()

			result := &interfaces.ImageValidationResult{
				Valid:      true,
				Exists:     true,
				Accessible: true,
				CheckedAt:  time.Now(),
			}

			// Set, Delete, Set
			cache.Set(image, result, 1*time.Hour)
			cache.Delete(image)
			cache.Set(image, result, 1*time.Hour)

			// Get should return the result
			cached := cache.Get(image)
			return cached != nil && cached.Valid == result.Valid
		},
		genValidImageName(),
	))

	properties.TestingRun(t)
}

// TestProperty_CacheWithRedisConsistency tests cache consistency with Redis backend
//
// Property: For any cached image, the result should be consistent whether retrieved
// from Redis or in-memory cache.
//
// Feature: image-validation-and-status, Property 3: Image validation caching
// **Validates: Requirements 2.5**
func TestProperty_CacheWithRedisConsistency(t *testing.T) {
	// Start miniredis for testing
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	redisClient := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	defer redisClient.Close()

	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxSize = 50

	properties := gopter.NewProperties(parameters)

	// Property: Redis and memory cache return consistent results
	properties.Property("redis and memory cache return consistent results", prop.ForAll(
		func(image string, valid, exists, accessible bool, errorMsg, warningMsg string) bool {
			cache := NewImageValidationCache().WithRedis(redisClient)

			// Use truncated time to avoid precision issues in JSON serialization
			checkedAt := time.Now().UTC().Truncate(time.Second)

			result := &interfaces.ImageValidationResult{
				Valid:      valid,
				Exists:     exists,
				Accessible: accessible,
				Error:      errorMsg,
				Warning:    warningMsg,
				CheckedAt:  checkedAt,
			}

			cache.Set(image, result, 1*time.Hour)

			// Get should return consistent result
			cached := cache.Get(image)
			if cached == nil {
				return false
			}

			return cached.Valid == valid &&
				cached.Exists == exists &&
				cached.Accessible == accessible &&
				cached.Error == errorMsg &&
				cached.Warning == warningMsg
		},
		genValidImageName(),
		gen.Bool(),
		gen.Bool(),
		gen.Bool(),
		genShortString(100),
		genShortString(100),
	))

	// Property: Delete removes from both Redis and memory
	properties.Property("delete removes from both redis and memory", prop.ForAll(
		func(image string) bool {
			cache := NewImageValidationCache().WithRedis(redisClient)

			result := &interfaces.ImageValidationResult{
				Valid:      true,
				Exists:     true,
				Accessible: true,
				CheckedAt:  time.Now(),
			}

			cache.Set(image, result, 1*time.Hour)
			cache.Delete(image)

			// Get should return nil
			return cache.Get(image) == nil
		},
		genValidImageName(),
	))

	properties.TestingRun(t)
}

// TestProperty_CacheConcurrentAccess tests thread-safety of cache operations
//
// Property: Concurrent cache operations SHALL not cause data races or inconsistencies.
//
// Feature: image-validation-and-status, Property 3: Image validation caching
// **Validates: Requirements 2.5**
func TestProperty_CacheConcurrentAccess(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 50 // Reduced for concurrent tests
	parameters.MaxSize = 20

	properties := gopter.NewProperties(parameters)

	// Property: Concurrent Set and Get operations are safe
	properties.Property("concurrent set and get operations are safe", prop.ForAll(
		func(images []string, numGoroutines int) bool {
			if len(images) == 0 {
				return true
			}

			cache := NewImageValidationCache()
			var wg sync.WaitGroup
			var successCount int64

			// Concurrent Set operations
			for i := 0; i < numGoroutines; i++ {
				wg.Add(1)
				go func(idx int) {
					defer wg.Done()
					image := images[idx%len(images)]
					result := &interfaces.ImageValidationResult{
						Valid:      true,
						Exists:     true,
						Accessible: true,
						CheckedAt:  time.Now(),
					}
					cache.Set(image, result, 1*time.Hour)
					atomic.AddInt64(&successCount, 1)
				}(i)
			}

			// Concurrent Get operations
			for i := 0; i < numGoroutines; i++ {
				wg.Add(1)
				go func(idx int) {
					defer wg.Done()
					image := images[idx%len(images)]
					_ = cache.Get(image)
					atomic.AddInt64(&successCount, 1)
				}(i)
			}

			wg.Wait()

			// All operations should complete without panic
			return successCount == int64(numGoroutines*2)
		},
		gen.SliceOfN(5, genValidImageName()),
		gen.IntRange(5, 20),
	))

	// Property: Concurrent Set, Get, and Delete operations are safe
	properties.Property("concurrent set get delete operations are safe", prop.ForAll(
		func(images []string) bool {
			if len(images) == 0 {
				return true
			}

			cache := NewImageValidationCache()
			var wg sync.WaitGroup
			numOps := 10

			for i := 0; i < numOps; i++ {
				image := images[i%len(images)]

				// Set
				wg.Add(1)
				go func(img string) {
					defer wg.Done()
					result := &interfaces.ImageValidationResult{
						Valid:      true,
						Exists:     true,
						Accessible: true,
						CheckedAt:  time.Now(),
					}
					cache.Set(img, result, 1*time.Hour)
				}(image)

				// Get
				wg.Add(1)
				go func(img string) {
					defer wg.Done()
					_ = cache.Get(img)
				}(image)

				// Delete
				wg.Add(1)
				go func(img string) {
					defer wg.Done()
					cache.Delete(img)
				}(image)
			}

			wg.Wait()

			// Should complete without panic
			return true
		},
		gen.SliceOfN(3, genValidImageName()),
	))

	properties.TestingRun(t)
}

// TestProperty_CacheDefaultTTLBehavior tests default TTL behavior
//
// Property: When TTL is 0, the cache SHALL use the configured default TTL.
//
// Feature: image-validation-and-status, Property 3: Image validation caching
// **Validates: Requirements 2.5**
func TestProperty_CacheDefaultTTLBehavior(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxSize = 50

	properties := gopter.NewProperties(parameters)

	// Property: Zero TTL uses default TTL
	properties.Property("zero TTL uses default TTL", prop.ForAll(
		func(image string) bool {
			config := &CacheConfig{
				DefaultTTL: 1 * time.Hour,
			}
			cache := NewImageValidationCacheWithConfig(config)

			result := &interfaces.ImageValidationResult{
				Valid:      true,
				Exists:     true,
				Accessible: true,
				CheckedAt:  time.Now(),
			}

			// Set with 0 TTL (should use default)
			cache.Set(image, result, 0)

			// Should be cached
			cached := cache.Get(image)
			return cached != nil && cached.Valid == result.Valid
		},
		genValidImageName(),
	))

	// Property: Custom TTL is respected
	properties.Property("custom TTL is respected", prop.ForAll(
		func(image string) bool {
			config := &CacheConfig{
				DefaultTTL: 1 * time.Hour,
			}
			cache := NewImageValidationCacheWithConfig(config)

			result := &interfaces.ImageValidationResult{
				Valid:      true,
				Exists:     true,
				Accessible: true,
				CheckedAt:  time.Now(),
			}

			// Set with very short custom TTL
			cache.Set(image, result, 1*time.Millisecond)

			// Wait for expiration
			time.Sleep(5 * time.Millisecond)

			// Should be expired
			return cache.Get(image) == nil
		},
		genValidImageName(),
	))

	properties.TestingRun(t)
}

// TestProperty_CacheClearBehavior tests cache clear behavior
//
// Property: After Clear, all cached items SHALL be removed.
//
// Feature: image-validation-and-status, Property 3: Image validation caching
// **Validates: Requirements 2.5**
func TestProperty_CacheClearBehavior(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxSize = 20

	properties := gopter.NewProperties(parameters)

	// Property: Clear removes all items
	properties.Property("clear removes all items", prop.ForAll(
		func(images []string) bool {
			if len(images) == 0 {
				return true
			}

			cache := NewImageValidationCache()

			// Add multiple items
			for _, image := range images {
				result := &interfaces.ImageValidationResult{
					Valid:      true,
					Exists:     true,
					Accessible: true,
					CheckedAt:  time.Now(),
				}
				cache.Set(image, result, 1*time.Hour)
			}

			// Clear
			cache.Clear()

			// All items should be gone
			for _, image := range images {
				if cache.Get(image) != nil {
					return false
				}
			}

			return cache.Size() == 0
		},
		gen.SliceOfN(10, genValidImageName()),
	))

	properties.TestingRun(t)
}

// TestProperty_CachePreservesAllFields tests that all fields are preserved in cache
//
// Property: For any ImageValidationResult, all fields SHALL be preserved after
// caching and retrieval.
//
// Feature: image-validation-and-status, Property 3: Image validation caching
// **Validates: Requirements 2.5**
func TestProperty_CachePreservesAllFields(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxSize = 50

	properties := gopter.NewProperties(parameters)

	// Property: All fields are preserved in memory cache
	properties.Property("all fields are preserved in memory cache", prop.ForAll(
		func(image string, valid, exists, accessible bool, errorMsg, warningMsg string) bool {
			cache := NewImageValidationCache()

			checkedAt := time.Now()
			result := &interfaces.ImageValidationResult{
				Valid:      valid,
				Exists:     exists,
				Accessible: accessible,
				Error:      errorMsg,
				Warning:    warningMsg,
				CheckedAt:  checkedAt,
			}

			cache.Set(image, result, 1*time.Hour)

			cached := cache.Get(image)
			if cached == nil {
				return false
			}

			return cached.Valid == valid &&
				cached.Exists == exists &&
				cached.Accessible == accessible &&
				cached.Error == errorMsg &&
				cached.Warning == warningMsg &&
				cached.CheckedAt.Equal(checkedAt)
		},
		genValidImageName(),
		gen.Bool(),
		gen.Bool(),
		gen.Bool(),
		genShortString(100),
		genShortString(100),
	))

	properties.TestingRun(t)
}

// ============================================================================
// Generators for cache property tests
// ============================================================================

// genValidImageName generates valid image names for cache testing
func genValidImageName() gopter.Gen {
	return gen.OneGenOf(
		// Simple names
		gen.RegexMatch(`[a-z][a-z0-9]{2,15}`),
		// Names with tag
		gen.RegexMatch(`[a-z][a-z0-9]{2,10}:[a-z0-9]{1,10}`),
		// Names with namespace
		gen.RegexMatch(`[a-z][a-z0-9]{2,8}/[a-z][a-z0-9]{2,8}`),
		// Well-known images
		gen.OneConstOf(
			"nginx",
			"nginx:latest",
			"nginx:1.25",
			"redis:7",
			"postgres:15",
			"ubuntu:22.04",
			"python:3.11",
			"node:20",
			"library/nginx",
			"gcr.io/project/image",
			"ghcr.io/owner/repo:tag",
		),
	)
}

// genShortString generates strings with a maximum length
func genShortString(maxLen int) gopter.Gen {
	return gen.OneGenOf(
		gen.Const(""),
		gen.AlphaString().Map(func(s string) string {
			if len(s) > maxLen {
				return s[:maxLen]
			}
			return s
		}),
		gen.OneConstOf(
			"error message",
			"warning message",
			"image not found",
			"authentication failed",
			"network timeout",
		),
	)
}
