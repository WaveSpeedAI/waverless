package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"waverless/internal/model"

	"github.com/go-redis/redis/v8"
)

const (
	workerKeyPrefixActive = "worker:"        // Active worker data
	workerSetKeyActive    = "workers:active" // Active worker set
	workerDataTTL         = 5 * time.Minute  // Worker data TTL
)

// WorkerRepository manages worker data in Redis (ephemeral data with TTL)
type WorkerRepository struct {
	redis *redis.Client
}

// NewWorkerRepository creates Worker repository
func NewWorkerRepository(redisClient *RedisClient) *WorkerRepository {
	return &WorkerRepository{
		redis: redisClient.GetClient(),
	}
}

// Save saves Worker information
func (r *WorkerRepository) Save(ctx context.Context, worker *model.Worker) error {
	key := workerKeyPrefixActive + worker.ID
	data, err := json.Marshal(worker)
	if err != nil {
		return fmt.Errorf("failed to marshal worker: %w", err)
	}

	pipe := r.redis.Pipeline()
	pipe.Set(ctx, key, data, workerDataTTL)
	pipe.SAdd(ctx, workerSetKeyActive, worker.ID)
	pipe.Expire(ctx, workerSetKeyActive, workerDataTTL*2)

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to save worker: %w", err)
	}

	return nil
}

// Get retrieves Worker information
func (r *WorkerRepository) Get(ctx context.Context, workerID string) (*model.Worker, error) {
	key := workerKeyPrefixActive + workerID
	data, err := r.redis.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, fmt.Errorf("worker not found: %s", workerID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get worker: %w", err)
	}

	var worker model.Worker
	if err := json.Unmarshal([]byte(data), &worker); err != nil {
		return nil, fmt.Errorf("failed to unmarshal worker: %w", err)
	}

	return &worker, nil
}

// GetAll retrieves all online Workers
func (r *WorkerRepository) GetAll(ctx context.Context) ([]*model.Worker, error) {
	workerIDs, err := r.redis.SMembers(ctx, workerSetKeyActive).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get worker list: %w", err)
	}

	if len(workerIDs) == 0 {
		return []*model.Worker{}, nil
	}

	// OPTIMIZATION: Use pipeline to batch fetch all workers in one round-trip
	pipe := r.redis.Pipeline()
	cmds := make([]*redis.StringCmd, 0, len(workerIDs))

	for _, workerID := range workerIDs {
		key := workerKeyPrefixActive + workerID
		cmds = append(cmds, pipe.Get(ctx, key))
	}

	_, err = pipe.Exec(ctx)
	if err != nil && err != redis.Nil {
		// Pipeline failed, fall back to individual gets
		workers := make([]*model.Worker, 0, len(workerIDs))
		for _, workerID := range workerIDs {
			worker, err := r.Get(ctx, workerID)
			if err != nil {
				continue
			}
			workers = append(workers, worker)
		}
		return workers, nil
	}

	// Parse results from pipeline
	workers := make([]*model.Worker, 0, len(workerIDs))
	for _, cmd := range cmds {
		data, err := cmd.Result()
		if err != nil {
			// Worker expired or error, skip
			continue
		}

		var worker model.Worker
		if err := json.Unmarshal([]byte(data), &worker); err != nil {
			// Malformed data, skip
			continue
		}
		workers = append(workers, &worker)
	}

	return workers, nil
}

// UpdateHeartbeat updates heartbeat
func (r *WorkerRepository) UpdateHeartbeat(ctx context.Context, workerID, endpoint string, jobsInProgress []string) error {
	worker, err := r.Get(ctx, workerID)
	if err != nil {
		// Worker doesn't exist, create new one
		if endpoint == "" {
			endpoint = "default"
		}
		worker = &model.Worker{
			ID:             workerID,
			Endpoint:       endpoint,
			Status:         model.WorkerStatusOnline,
			Concurrency:    1,
			JobsInProgress: jobsInProgress,
			RegisteredAt:   time.Now(),
		}
	}

	// Update endpoint (allow dynamic modification)
	if endpoint != "" {
		worker.Endpoint = endpoint
	} else if worker.Endpoint == "" {
		worker.Endpoint = "default"
	}

	worker.LastHeartbeat = time.Now()
	worker.JobsInProgress = jobsInProgress
	worker.CurrentJobs = len(jobsInProgress)

	// Determine status based on current number of tasks
	// IMPORTANT: Do not override DRAINING status (set by Pod Watcher when pod is terminating)
	if worker.Status != model.WorkerStatusDraining {
		if worker.CurrentJobs >= worker.Concurrency {
			worker.Status = model.WorkerStatusBusy
		} else {
			worker.Status = model.WorkerStatusOnline
		}
	}

	return r.Save(ctx, worker)
}

// Delete deletes Worker
func (r *WorkerRepository) Delete(ctx context.Context, workerID string) error {
	key := workerKeyPrefixActive + workerID
	pipe := r.redis.Pipeline()
	pipe.Del(ctx, key)
	pipe.SRem(ctx, workerSetKeyActive, workerID)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete worker: %w", err)
	}

	return nil
}

// GetOnlineWorkerCount retrieves online Worker count
func (r *WorkerRepository) GetOnlineWorkerCount(ctx context.Context) (int, error) {
	count, err := r.redis.SCard(ctx, workerSetKeyActive).Result()
	if err != nil {
		return 0, fmt.Errorf("failed to get online worker count: %w", err)
	}
	return int(count), nil
}

// ============================================================================
// The following task-related Redis functions are deprecated (Tasks migrated to MySQL)
// These functions are retained for backward compatibility with test code
// ============================================================================

// AddToPendingQueue [DEPRECATED] Tasks are now stored in MySQL
// Retained for backward compatibility with tests
func (r *WorkerRepository) AddToPendingQueue(ctx context.Context, endpoint, taskID string) error {
	return nil // Empty implementation, tasks are now in MySQL
}

// PullFromPendingQueue [DEPRECATED] Tasks are now pulled from MySQL
// Retained for backward compatibility with tests
func (r *WorkerRepository) PullFromPendingQueue(ctx context.Context, endpoint string, count int) ([]string, error) {
	return []string{}, nil // Empty implementation, tasks are now pulled from MySQL
}

// AssignTaskToWorker [DEPRECATED] Task assignment is now managed in MySQL
// Retained for backward compatibility with tests
func (r *WorkerRepository) AssignTaskToWorker(ctx context.Context, workerID, taskID string) error {
	return nil // Empty implementation, task assignment is now in MySQL
}

// GetPendingQueueLength [DEPRECATED] Task statistics are now queried from MySQL
// Retained for backward compatibility with tests
func (r *WorkerRepository) GetPendingQueueLength(ctx context.Context, endpoint string) (int64, error) {
	return 0, nil // Empty implementation, task statistics are now from MySQL
}

// GetAssignedTasks [DEPRECATED] Task assignment is now managed in MySQL
// Retained for backward compatibility with tests
func (r *WorkerRepository) GetAssignedTasks(ctx context.Context, workerID string) ([]string, error) {
	return []string{}, nil // Empty implementation, tasks are now in MySQL
}

// RemoveTaskFromWorker [DEPRECATED] Task assignment is now managed in MySQL
// Retained for backward compatibility with tests
func (r *WorkerRepository) RemoveTaskFromWorker(ctx context.Context, workerID, taskID string) error {
	return nil // Empty implementation, tasks are now in MySQL
}

// ClearWorkerTasks [DEPRECATED] Task assignment is now managed in MySQL
// Retained for backward compatibility with tests
func (r *WorkerRepository) ClearWorkerTasks(ctx context.Context, workerID string) error {
	return nil // Empty implementation, tasks are now in MySQL
}

// GetByEndpoint retrieves all workers for specified endpoint
func (r *WorkerRepository) GetByEndpoint(ctx context.Context, endpoint string) ([]*model.Worker, error) {
	workers, err := r.GetAll(ctx)
	if err != nil {
		return nil, err
	}
	
	result := make([]*model.Worker, 0)
	for _, w := range workers {
		if w.Endpoint == endpoint {
			result = append(result, w)
		}
	}
	return result, nil
}

// CountByEndpoint counts workers for endpoint
func (r *WorkerRepository) CountByEndpoint(ctx context.Context, endpoint string) (int, error) {
	workers, err := r.GetByEndpoint(ctx, endpoint)
	return len(workers), err
}

// GetBusyWorkers retrieves busy workers
func (r *WorkerRepository) GetBusyWorkers(ctx context.Context, endpoint string) ([]*model.Worker, error) {
	workers, err := r.GetByEndpoint(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	
	result := make([]*model.Worker, 0)
	for _, w := range workers {
		if w.Status == model.WorkerStatusBusy {
			result = append(result, w)
		}
	}
	return result, nil
}

// GetOfflineWorkers retrieves offline workers (heartbeat timeout)
func (r *WorkerRepository) GetOfflineWorkers(ctx context.Context, timeout time.Duration) ([]*model.Worker, error) {
	workers, err := r.GetAll(ctx)
	if err != nil {
		return nil, err
	}
	
	now := time.Now()
	result := make([]*model.Worker, 0)
	
	for _, worker := range workers {
		if now.Sub(worker.LastHeartbeat) > timeout {
			result = append(result, worker)
		}
	}
	
	return result, nil
}
