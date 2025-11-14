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
