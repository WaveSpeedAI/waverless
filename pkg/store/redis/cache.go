package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"waverless/pkg/logger"

	"github.com/go-redis/redis/v8"
)

// CacheStore is the general cache storage
type CacheStore struct {
	client *redis.Client
}

// NewCacheStore creates a cache storage
func NewCacheStore(client *redis.Client) *CacheStore {
	return &CacheStore{client: client}
}

// Set sets cache value
func (s *CacheStore) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	if s.client == nil {
		return nil
	}

	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal cache value: %w", err)
	}

	if err := s.client.Set(ctx, key, data, ttl).Err(); err != nil {
		return fmt.Errorf("failed to set cache: %w", err)
	}

	return nil
}

// Get gets cache value
func (s *CacheStore) Get(ctx context.Context, key string, dest any) (bool, error) {
	if s.client == nil {
		return false, nil
	}

	data, err := s.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to get cache: %w", err)
	}

	if err := json.Unmarshal(data, dest); err != nil {
		return false, fmt.Errorf("failed to unmarshal cache value: %w", err)
	}

	return true, nil
}

// Delete deletes cache
func (s *CacheStore) Delete(ctx context.Context, key string) error {
	if s.client == nil {
		return nil
	}

	if err := s.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("failed to delete cache: %w", err)
	}

	return nil
}

// Exists checks if cache exists
func (s *CacheStore) Exists(ctx context.Context, key string) (bool, error) {
	if s.client == nil {
		return false, nil
	}

	count, err := s.client.Exists(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("failed to check cache existence: %w", err)
	}

	return count > 0, nil
}

// SetString sets string cache
func (s *CacheStore) SetString(ctx context.Context, key, value string, ttl time.Duration) error {
	if s.client == nil {
		return nil
	}

	if err := s.client.Set(ctx, key, value, ttl).Err(); err != nil {
		return fmt.Errorf("failed to set string cache: %w", err)
	}

	return nil
}

// GetString gets string cache
func (s *CacheStore) GetString(ctx context.Context, key string) (string, bool, error) {
	if s.client == nil {
		return "", false, nil
	}

	value, err := s.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("failed to get string cache: %w", err)
	}

	return value, true, nil
}

// Expire sets expiration time
func (s *CacheStore) Expire(ctx context.Context, key string, ttl time.Duration) error {
	if s.client == nil {
		return nil
	}

	if err := s.client.Expire(ctx, key, ttl).Err(); err != nil {
		return fmt.Errorf("failed to set expiration: %w", err)
	}

	return nil
}

// TTL gets remaining expiration time
func (s *CacheStore) TTL(ctx context.Context, key string) (time.Duration, error) {
	if s.client == nil {
		return 0, nil
	}

	ttl, err := s.client.TTL(ctx, key).Result()
	if err != nil {
		return 0, fmt.Errorf("failed to get TTL: %w", err)
	}

	return ttl, nil
}

// Scan scans matching keys
func (s *CacheStore) Scan(ctx context.Context, pattern string, count int64) ([]string, error) {
	if s.client == nil {
		return nil, nil
	}

	var keys []string
	iter := s.client.Scan(ctx, 0, pattern, count).Iterator()
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}

	if err := iter.Err(); err != nil {
		logger.WarnCtx(ctx, "Error scanning keys with pattern %s: %v", pattern, err)
		return keys, err
	}

	return keys, nil
}

// Incr increments
func (s *CacheStore) Incr(ctx context.Context, key string) (int64, error) {
	if s.client == nil {
		return 0, nil
	}

	val, err := s.client.Incr(ctx, key).Result()
	if err != nil {
		return 0, fmt.Errorf("failed to increment: %w", err)
	}

	return val, nil
}

// Decr decrements
func (s *CacheStore) Decr(ctx context.Context, key string) (int64, error) {
	if s.client == nil {
		return 0, nil
	}

	val, err := s.client.Decr(ctx, key).Result()
	if err != nil {
		return 0, fmt.Errorf("failed to decrement: %w", err)
	}

	return val, nil
}
