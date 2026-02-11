package mysql

import (
	"context"
	"time"

	"waverless/pkg/store/mysql/model"
)

// TaskRepositoryInterface is the task data access interface
type TaskRepositoryInterface interface {
	// Basic CRUD
	Create(ctx context.Context, task *Task) error
	Get(ctx context.Context, taskID string) (*Task, error)
	Delete(ctx context.Context, taskID string) error

	// Update operations
	UpdateFields(ctx context.Context, taskID string, updates map[string]interface{}) error
	UpdateFieldsWithStatus(ctx context.Context, taskID string, expectedStatus string, updates map[string]interface{}) error
	UpdateStatus(ctx context.Context, taskID string, fromStatus, toStatus string) error
	UpdateStatusUnsafe(ctx context.Context, taskID string, status string) error
	BatchUpdateStatus(ctx context.Context, taskIDs []string, status string) error

	// Query operations
	GetInProgressTasks(ctx context.Context) ([]string, error)
	GetInProgressTasksByEndpoint(ctx context.Context, endpoint string) ([]string, error)
	GetTasksByWorker(ctx context.Context, workerID string) ([]*Task, error)
	GetPendingTasksByEndpoint(ctx context.Context, endpoint string, limit int) ([]*Task, error)
	ListWithTaskID(ctx context.Context, filters map[string]interface{}, taskID string, limit, offset int) ([]*Task, error)
	ListWithTaskIDExcludeInput(ctx context.Context, filters map[string]interface{}, taskID string, limit, offset int) ([]*Task, error)

	// Statistics operations
	CountByEndpointAndStatus(ctx context.Context, endpoint, status string) (int64, error)
	CountByStatus(ctx context.Context, status string) (int64, error)
	CountInProgressByEndpoint(ctx context.Context, endpoint string) (int64, error)
	CountWithTaskID(ctx context.Context, filters map[string]interface{}, taskID string) (int64, error)

	// Task assignment
	SelectAndAssignTasks(ctx context.Context, endpoint string, limit int, workerID string) ([]*Task, error)
	AssignTasksToWorker(ctx context.Context, taskIDs []string, workerID string) ([]*Task, error)

	// Transaction support
	ExecTx(ctx context.Context, fn func(ctx context.Context) error) error

	// Cleanup operations
	CleanupOldTasks(ctx context.Context, before time.Time) (int64, error)
}

// WorkerRepositoryInterface is the Worker data access interface
type WorkerRepositoryInterface interface {
	// Basic operations
	Get(ctx context.Context, workerID string) (*model.Worker, error)
	GetByID(ctx context.Context, id int64) (*model.Worker, error)
	GetByPodName(ctx context.Context, endpoint, podName string) (*model.Worker, error)
	Delete(ctx context.Context, workerID string) error

	// List queries
	GetByEndpoint(ctx context.Context, endpoint string) ([]*model.Worker, error)
	GetByEndpointForSync(ctx context.Context, endpoint string) ([]*model.Worker, error)
	GetAll(ctx context.Context) ([]*model.Worker, error)
	GetStaleWorkers(ctx context.Context, threshold time.Time) ([]*model.Worker, error)

	// Status updates
	UpdateStatus(ctx context.Context, workerID string, status string) error
	UpdateHeartbeat(ctx context.Context, workerID, endpoint string, jobsInProgress []string, jobsInProgressCount int, version string) error
	UpdateLastTaskTime(ctx context.Context, workerID string) error
	UpsertFromPod(ctx context.Context, podName, endpoint, phase, status, reason, message, ip, nodeName string, createdAt, startedAt, readyAt *time.Time) error

	// Statistics updates
	IncrementTaskStats(ctx context.Context, workerID string, completed bool, executionTimeMs int64) error
	IncrementTaskStatsAt(ctx context.Context, workerID string, completed bool, executionTimeMs int64, completedAt time.Time) error

	// Offline marking
	MarkOffline(ctx context.Context, heartbeatThreshold time.Duration) (int64, error)
	MarkOfflineByPodName(ctx context.Context, podName string, terminatedAt ...time.Time) error

	// Failure handling
	UpdateWorkerFailure(ctx context.Context, podName, failureType, failureReason, failureDetails string, occurredAt time.Time) error
	ClearWorkerFailure(ctx context.Context, podName string) error
	GetWorkersWithFailure(ctx context.Context, endpoint string) ([]*model.Worker, error)
	GetWorkersByFailureType(ctx context.Context, failureType string) ([]*model.Worker, error)
}

// EndpointRepositoryInterface is the Endpoint data access interface
type EndpointRepositoryInterface interface {
	// Basic CRUD
	Create(ctx context.Context, endpoint *model.Endpoint) error
	Get(ctx context.Context, name string) (*model.Endpoint, error)
	Update(ctx context.Context, endpoint *model.Endpoint) error
	Delete(ctx context.Context, name string) error
	List(ctx context.Context) ([]*model.Endpoint, error)

	// Status updates
	UpdateRuntimeState(ctx context.Context, name string, status string, runtimeState map[string]interface{}) error
	UpdateHealthStatus(ctx context.Context, name string, healthStatus string, healthMessage string) error
}

// SpecRepositoryInterface is the Spec data access interface
type SpecRepositoryInterface interface {
	Create(ctx context.Context, spec *model.Spec) error
	Get(ctx context.Context, name string) (*model.Spec, error)
	Update(ctx context.Context, spec *model.Spec) error
	Delete(ctx context.Context, name string) error
	List(ctx context.Context) ([]*model.Spec, error)
}

// MonitoringRepositoryInterface is the monitoring data access interface
type MonitoringRepositoryInterface interface {
	// Worker events
	SaveWorkerEvent(ctx context.Context, event *model.WorkerEvent) error
	CountWorkerEvents(ctx context.Context, workerID, eventType string, count *int64)
	CleanupOldWorkerEvents(ctx context.Context, before time.Time) (int64, error)

	// Statistics aggregation
	UpsertMinuteStat(ctx context.Context, stat *model.EndpointMinuteStat) error
	GetMinuteStats(ctx context.Context, endpoint string, from, to time.Time) ([]*model.EndpointMinuteStat, error)
	UpsertHourlyStat(ctx context.Context, stat *model.EndpointHourlyStat) error
	GetHourlyStats(ctx context.Context, endpoint string, from, to time.Time) ([]*model.EndpointHourlyStat, error)
	UpsertDailyStat(ctx context.Context, stat *model.EndpointDailyStat) error
	GetDailyStats(ctx context.Context, endpoint string, from, to time.Time) ([]*model.EndpointDailyStat, error)

	// Real-time metrics
	GetRealtimeMetrics(ctx context.Context, endpoint string) (*RealtimeMetrics, error)

	// Aggregation calculations
	AggregateMinuteStats(ctx context.Context, endpoint string, from, to time.Time) (*model.EndpointMinuteStat, error)
	AggregateHourlyStats(ctx context.Context, endpoint string, statHour time.Time) (*model.EndpointHourlyStat, error)
	AggregateDailyStats(ctx context.Context, endpoint string, statDate time.Time) (*model.EndpointDailyStat, error)

	// Cleanup
	CleanupOldMinuteStats(ctx context.Context, before time.Time) (int64, error)
	CleanupOldHourlyStats(ctx context.Context, before time.Time) (int64, error)
	CleanupOldDailyStats(ctx context.Context, before time.Time) (int64, error)

	// Helpers
	GetDistinctEndpoints(ctx context.Context, from, to time.Time) ([]string, error)
	GetAllEndpoints(ctx context.Context) ([]string, error)
}

// Ensure implementations conform to interfaces
var (
	_ TaskRepositoryInterface       = (*TaskRepository)(nil)
	_ WorkerRepositoryInterface     = (*WorkerRepository)(nil)
	_ EndpointRepositoryInterface   = (*EndpointRepository)(nil)
	_ SpecRepositoryInterface       = (*SpecRepository)(nil)
	_ MonitoringRepositoryInterface = (*MonitoringRepository)(nil)
)
