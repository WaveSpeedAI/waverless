package redis

import (
	"context"
	"fmt"
	"strings"
	"time"

	"waverless/pkg/logger"

	"github.com/go-redis/redis/v8"
)

// Draining state management constants
const (
	// Redis key prefix for draining workers: draining:{provider}:{workerID}
	DrainingKeyPrefix = "draining:"
	// Default TTL for draining worker keys (1 hour - safety cleanup)
	DefaultDrainingTTL = 1 * time.Hour
)

// DrainingStore is the draining state storage
// Used to track workers being drained, supports multi-replica safety
type DrainingStore struct {
	client *redis.Client
	ttl    time.Duration
}

// NewDrainingStore creates a draining state storage
func NewDrainingStore(client *redis.Client) *DrainingStore {
	return &DrainingStore{
		client: client,
		ttl:    DefaultDrainingTTL,
	}
}

// NewDrainingStoreWithTTL creates a draining state storage with custom TTL
func NewDrainingStoreWithTTL(client *redis.Client, ttl time.Duration) *DrainingStore {
	return &DrainingStore{
		client: client,
		ttl:    ttl,
	}
}

// drainingKey returns the Redis key for a draining worker
// provider: provider name (e.g., "novita", "k8s")
// workerID: worker identifier
func drainingKey(provider, workerID string) string {
	return fmt.Sprintf("%s%s:%s", DrainingKeyPrefix, provider, workerID)
}

// IsDraining checks if a worker is in draining state
// Used by WorkerService.PullJob to check if tasks should be assigned to this worker
func (s *DrainingStore) IsDraining(ctx context.Context, provider, workerID string) (bool, error) {
	if workerID == "" {
		return false, nil
	}

	if s.client == nil {
		// Redis not configured, cannot track draining state
		return false, nil
	}

	key := drainingKey(provider, workerID)
	exists, err := s.client.Exists(ctx, key).Result()
	if err != nil {
		logger.WarnCtx(ctx, "Failed to check draining state for worker %s: %v", workerID, err)
		return false, nil // Fail open - don't block task dispatch on Redis errors
	}

	if exists > 0 {
		logger.DebugCtx(ctx, "Worker %s is draining (found in Redis)", workerID)
		return true, nil
	}

	return false, nil
}

// MarkDraining marks a worker as draining state
// Called during scale-down to prevent new task assignment
func (s *DrainingStore) MarkDraining(ctx context.Context, provider, workerID string) error {
	if s.client == nil {
		logger.WarnCtx(ctx, "Redis not configured, cannot mark worker %s as draining", workerID)
		return nil
	}

	key := drainingKey(provider, workerID)
	drainTime := time.Now().Format(time.RFC3339)

	err := s.client.Set(ctx, key, drainTime, s.ttl).Err()
	if err != nil {
		return fmt.Errorf("failed to mark worker %s as draining: %w", workerID, err)
	}

	logger.InfoCtx(ctx, "Marked worker %s as draining in Redis (TTL: %v)", workerID, s.ttl)
	return nil
}

// ClearDraining clears a worker's draining state
// Called after worker is confirmed offline/deleted
func (s *DrainingStore) ClearDraining(ctx context.Context, provider, workerID string) error {
	if s.client == nil {
		return nil
	}

	key := drainingKey(provider, workerID)
	err := s.client.Del(ctx, key).Err()
	if err != nil {
		logger.WarnCtx(ctx, "Failed to clear draining state for worker %s: %v", workerID, err)
		return err
	}

	logger.InfoCtx(ctx, "Cleared draining state for worker %s from Redis", workerID)
	return nil
}

// GetDrainingWorkers gets all draining workers for a specific provider
// Used for debugging and monitoring
func (s *DrainingStore) GetDrainingWorkers(ctx context.Context, provider string) []string {
	if s.client == nil {
		return nil
	}

	// Scan for all draining worker keys for this provider
	var workers []string
	pattern := fmt.Sprintf("%s%s:*", DrainingKeyPrefix, provider)
	prefix := fmt.Sprintf("%s%s:", DrainingKeyPrefix, provider)

	iter := s.client.Scan(ctx, 0, pattern, 100).Iterator()
	for iter.Next(ctx) {
		key := iter.Val()
		// Extract workerID from key
		workerID := strings.TrimPrefix(key, prefix)
		workers = append(workers, workerID)
	}

	if err := iter.Err(); err != nil {
		logger.WarnCtx(ctx, "Error scanning draining workers for provider %s: %v", provider, err)
	}

	return workers
}

// GetAllDrainingWorkers gets draining workers for all providers
// Returns map[provider][]workerID
func (s *DrainingStore) GetAllDrainingWorkers(ctx context.Context) map[string][]string {
	if s.client == nil {
		return nil
	}

	result := make(map[string][]string)
	pattern := DrainingKeyPrefix + "*"

	iter := s.client.Scan(ctx, 0, pattern, 100).Iterator()
	for iter.Next(ctx) {
		key := iter.Val()
		// Parse key: draining:{provider}:{workerID}
		parts := strings.TrimPrefix(key, DrainingKeyPrefix)
		idx := strings.Index(parts, ":")
		if idx > 0 {
			provider := parts[:idx]
			workerID := parts[idx+1:]
			result[provider] = append(result[provider], workerID)
		}
	}

	if err := iter.Err(); err != nil {
		logger.WarnCtx(ctx, "Error scanning all draining workers: %v", err)
	}

	return result
}

// GetDrainingInfo gets draining info for a worker (including start time)
func (s *DrainingStore) GetDrainingInfo(ctx context.Context, provider, workerID string) (time.Time, bool, error) {
	if s.client == nil {
		return time.Time{}, false, nil
	}

	key := drainingKey(provider, workerID)
	value, err := s.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("failed to get draining info: %w", err)
	}

	drainTime, err := time.Parse(time.RFC3339, value)
	if err != nil {
		// Value exists but can't parse time, still consider it draining
		return time.Time{}, true, nil
	}

	return drainTime, true, nil
}
