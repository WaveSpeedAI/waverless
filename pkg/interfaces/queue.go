package interfaces

import (
	"context"
	"time"

	"waverless/internal/model"
)

// QueueProvider queue provider interface
// Supports multiple implementations like Redis/Asynq, database, message queue, etc.
type QueueProvider interface {
	// EnqueueTask enqueues task
	EnqueueTask(ctx context.Context, task *model.Task) error

	// DequeueTask dequeues task (Worker pull)
	// endpoint: which endpoint to pull tasks from
	// count: number to pull
	DequeueTask(ctx context.Context, endpoint string, count int) ([]*model.Task, error)

	// GetTaskInfo retrieves task information
	GetTaskInfo(ctx context.Context, taskID string) (*TaskInfo, error)

	// CancelTask cancels task
	CancelTask(ctx context.Context, taskID string) error

	// GetPendingTaskCount retrieves pending task count
	// endpoint: specified endpoint, empty string means all endpoints
	GetPendingTaskCount(ctx context.Context, endpoint string) (int, error)

	// UpdateTaskStatus updates task status
	UpdateTaskStatus(ctx context.Context, taskID string, status model.TaskStatus) error

	// GetQueueStats retrieves queue statistics
	GetQueueStats(ctx context.Context, endpoint string) (*QueueStats, error)

	// Close closes queue connection
	Close() error
}

// TaskInfo task information (queue level)
type TaskInfo struct {
	ID          string           `json:"id"`
	Type        string           `json:"type"`
	State       TaskState        `json:"state"`
	Endpoint    string           `json:"endpoint"`
	MaxRetry    int              `json:"maxRetry"`
	Retried     int              `json:"retried"`
	LastErr     string           `json:"lastErr,omitempty"`
	Timeout     time.Duration    `json:"timeout"`
	Deadline    time.Time        `json:"deadline"`
	CompletedAt *time.Time       `json:"completedAt,omitempty"`
}

// TaskState task state (queue level)
type TaskState string

const (
	TaskStatePending   TaskState = "pending"    // Pending
	TaskStateActive    TaskState = "active"     // Active
	TaskStateCompleted TaskState = "completed"  // Completed
	TaskStateFailed    TaskState = "failed"     // Failed
	TaskStateRetry     TaskState = "retry"      // Retry
	TaskStateArchived  TaskState = "archived"   // Archived
)

// QueueStats queue statistics
type QueueStats struct {
	Endpoint       string `json:"endpoint"`
	PendingCount   int    `json:"pendingCount"`   // Pending
	ActiveCount    int    `json:"activeCount"`    // Active
	CompletedCount int    `json:"completedCount"` // Completed (last 1 hour)
	FailedCount    int    `json:"failedCount"`    // Failed (last 1 hour)
	RetryCount     int    `json:"retryCount"`     // Retry
}

// WorkerInfo Worker information (queue level)
type WorkerInfo struct {
	ID             string    `json:"id"`
	Endpoint       string    `json:"endpoint"`
	Status         string    `json:"status"`
	Concurrency    int       `json:"concurrency"`
	CurrentJobs    int       `json:"currentJobs"`
	JobsInProgress []string  `json:"jobsInProgress"`
	LastHeartbeat  time.Time `json:"lastHeartbeat"`
}
