package asynq

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"waverless/internal/model"
	"waverless/pkg/config"
	"waverless/pkg/logger"

	"github.com/hibiken/asynq"
)

const (
	TypeTaskSubmit = "task:submit"
)

// Manager queue manager
type Manager struct {
	client *asynq.Client
	server *asynq.Server
	mux    *asynq.ServeMux
}

// NewManager creates queue manager
func NewManager(cfg *config.Config) (*Manager, error) {
	redisOpt := asynq.RedisClientOpt{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	}

	client := asynq.NewClient(redisOpt)

	server := asynq.NewServer(
		redisOpt,
		asynq.Config{
			Concurrency: cfg.Queue.Concurrency,
			Queues: map[string]int{
				"default": 10,
			},
			RetryDelayFunc: func(n int, err error, task *asynq.Task) time.Duration {
				return time.Duration(n) * time.Second
			},
		},
	)

	mux := asynq.NewServeMux()

	return &Manager{
		client: client,
		server: server,
		mux:    mux,
	}, nil
}

// EnqueueTask enqueues task
func (m *Manager) EnqueueTask(ctx context.Context, task *model.Task) error {
	payload, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("failed to marshal task: %w", err)
	}

	asynqTask := asynq.NewTask(TypeTaskSubmit, payload)

	opts := []asynq.Option{
		asynq.TaskID(task.ID),
		asynq.Timeout(time.Duration(config.GlobalConfig.Queue.TaskTimeout) * time.Second),
		// No retention - task results are persisted permanently in Redis
		asynq.MaxRetry(config.GlobalConfig.Queue.MaxRetry),
	}

	info, err := m.client.EnqueueContext(ctx, asynqTask, opts...)
	if err != nil {
		return fmt.Errorf("failed to enqueue task: %w", err)
	}

	logger.InfoCtx(ctx, "task enqueued, task_id: %s, queue: %s", task.ID, info.Queue)

	return nil
}

// GetTaskInfo retrieves task information
func (m *Manager) GetTaskInfo(taskID string) (*asynq.TaskInfo, error) {
	inspector := asynq.NewInspector(asynq.RedisClientOpt{
		Addr:     config.GlobalConfig.Redis.Addr,
		Password: config.GlobalConfig.Redis.Password,
		DB:       config.GlobalConfig.Redis.DB,
	})
	defer inspector.Close()

	// Try to find task from different states
	info, err := inspector.GetTaskInfo("default", taskID)
	if err == nil {
		return info, nil
	}

	return nil, fmt.Errorf("task not found: %s", taskID)
}

// CancelTask cancels task
func (m *Manager) CancelTask(taskID string) error {
	inspector := asynq.NewInspector(asynq.RedisClientOpt{
		Addr:     config.GlobalConfig.Redis.Addr,
		Password: config.GlobalConfig.Redis.Password,
		DB:       config.GlobalConfig.Redis.DB,
	})
	defer inspector.Close()

	err := inspector.DeleteTask("default", taskID)
	if err != nil {
		return fmt.Errorf("failed to cancel task: %w", err)
	}

	logger.InfoCtx(context.Background(), "task cancelled, task_id: %s", taskID)
	return nil
}

// RegisterHandler registers task handler
func (m *Manager) RegisterHandler(pattern string, handler asynq.Handler) {
	m.mux.Handle(pattern, handler)
}

// Start starts queue processor
func (m *Manager) Start() error {
	logger.InfoCtx(context.Background(), "starting queue server")
	return m.server.Start(m.mux)
}

// Stop stops queue processor
func (m *Manager) Stop() {
	logger.InfoCtx(context.Background(), "stopping queue server")
	m.server.Stop()
	m.server.Shutdown()
}

// Close closes client
func (m *Manager) Close() error {
	return m.client.Close()
}

// GetPendingTaskCount retrieves pending task count
func (m *Manager) GetPendingTaskCount() (int, error) {
	inspector := asynq.NewInspector(asynq.RedisClientOpt{
		Addr:     config.GlobalConfig.Redis.Addr,
		Password: config.GlobalConfig.Redis.Password,
		DB:       config.GlobalConfig.Redis.DB,
	})
	defer inspector.Close()

	stats, err := inspector.GetQueueInfo("default")
	if err != nil {
		return 0, err
	}

	return stats.Pending, nil
}
