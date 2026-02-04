package redis

import (
	"context"
	"fmt"

	"waverless/pkg/config"

	"github.com/go-redis/redis/v8"
)

// RedisClient Redis client wrapper
type RedisClient struct {
	client *redis.Client
}

// NewRedisClient creates Redis client
func NewRedisClient(cfg *config.Config) (*RedisClient, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})

	// Test connection
	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to redis: %w", err)
	}

	return &RedisClient{client: client}, nil
}

// GetClient retrieves the underlying Redis client
func (r *RedisClient) GetClient() *redis.Client {
	return r.client
}

// Close closes the Redis connection
func (r *RedisClient) Close() error {
	return r.client.Close()
}

// RedisStore is the Redis storage aggregator
// Unified management of all Redis-related operations
type RedisStore struct {
	client   *redis.Client
	Lock     *LockStore
	Cache    *CacheStore
	Draining *DrainingStore
}

// NewRedisStore creates a Redis storage aggregator
func NewRedisStore(client *redis.Client) *RedisStore {
	return &RedisStore{
		client:   client,
		Lock:     NewLockStore(client),
		Cache:    NewCacheStore(client),
		Draining: NewDrainingStore(client),
	}
}

// NewRedisStoreFromConfig creates a Redis storage aggregator from config
func NewRedisStoreFromConfig(cfg *config.Config) (*RedisStore, error) {
	redisClient, err := NewRedisClient(cfg)
	if err != nil {
		return nil, err
	}
	return NewRedisStore(redisClient.GetClient()), nil
}

// GetClient gets the underlying Redis client
func (s *RedisStore) GetClient() *redis.Client {
	return s.client
}

// Close closes the Redis connection
func (s *RedisStore) Close() error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

// Ping tests the Redis connection
func (s *RedisStore) Ping(ctx context.Context) error {
	if s.client == nil {
		return fmt.Errorf("redis client is nil")
	}
	return s.client.Ping(ctx).Err()
}
