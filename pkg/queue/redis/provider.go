package redis

import (
	"context"
	"fmt"

	"waverless/internal/model"
	"waverless/pkg/config"
	"waverless/pkg/interfaces"
	"waverless/pkg/queue/asynq"
	redisstore "waverless/pkg/store/redis"
)

// RedisQueueProvider Redis queue provider implementation (deprecated, retained for backward compatibility)
// Note: Tasks are now stored in MySQL, Redis is no longer used for task storage
type RedisQueueProvider struct {
	queueManager  *asynq.Manager
	workerRepo    *redisstore.WorkerRepository
	redisClient   *redisstore.RedisClient
}

// NewRedisQueueProvider creates Redis queue provider
func NewRedisQueueProvider(cfg *config.Config) (interfaces.QueueProvider, error) {
	// Create Redis client
	redisClient, err := redisstore.NewRedisClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create redis client: %w", err)
	}

	// creates queue manager
	queueManager, err := asynq.NewManager(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create queue manager: %w", err)
	}

	// Create worker repository (tasks are now in MySQL)
	workerRepo := redisstore.NewWorkerRepository(redisClient)

	return &RedisQueueProvider{
		queueManager:  queueManager,
		workerRepo:    workerRepo,
		redisClient:   redisClient,
	}, nil
}

// EnqueueTask enqueues task (deprecated - tasks are now stored in MySQL)
func (p *RedisQueueProvider) EnqueueTask(ctx context.Context, task *model.Task) error {
	return fmt.Errorf("RedisQueueProvider is deprecated: tasks are now stored in MySQL")
}

// DequeueTask dequeues task (deprecated - tasks are now stored in MySQL)
func (p *RedisQueueProvider) DequeueTask(ctx context.Context, endpoint string, count int) ([]*model.Task, error) {
	return nil, fmt.Errorf("RedisQueueProvider is deprecated: tasks are now stored in MySQL")
}

// GetTaskInfo retrieves task information (deprecated - tasks are now stored in MySQL)
func (p *RedisQueueProvider) GetTaskInfo(ctx context.Context, taskID string) (*interfaces.TaskInfo, error) {
	return nil, fmt.Errorf("RedisQueueProvider is deprecated: tasks are now stored in MySQL")
}

// CancelTask cancels task (deprecated - tasks are now stored in MySQL)
func (p *RedisQueueProvider) CancelTask(ctx context.Context, taskID string) error {
	return fmt.Errorf("RedisQueueProvider is deprecated: tasks are now stored in MySQL")
}

// GetPendingTaskCount retrieves pending task count (deprecated - tasks are now stored in MySQL)
func (p *RedisQueueProvider) GetPendingTaskCount(ctx context.Context, endpoint string) (int, error) {
	return 0, fmt.Errorf("RedisQueueProvider is deprecated: tasks are now stored in MySQL")
}

// UpdateTaskStatus updates task status (deprecated - tasks are now stored in MySQL)
func (p *RedisQueueProvider) UpdateTaskStatus(ctx context.Context, taskID string, status model.TaskStatus) error {
	return fmt.Errorf("RedisQueueProvider is deprecated: tasks are now stored in MySQL")
}

// GetQueueStats retrieves queue statistics (deprecated - tasks are now stored in MySQL)
func (p *RedisQueueProvider) GetQueueStats(ctx context.Context, endpoint string) (*interfaces.QueueStats, error) {
	return &interfaces.QueueStats{
		Endpoint:       endpoint,
		PendingCount:   0, // Deprecated, tasks are now in MySQL
		ActiveCount:    0,
		CompletedCount: 0,
		FailedCount:    0,
		RetryCount:     0,
	}, nil
}

// Close closes queue connection
func (p *RedisQueueProvider) Close() error {
	if err := p.queueManager.Close(); err != nil {
		return err
	}
	return p.redisClient.Close()
}
