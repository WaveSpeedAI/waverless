package main

import (
	"context"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"

	"waverless/internal/jobs"
	"waverless/internal/service"
	"waverless/pkg/autoscaler"
	"waverless/pkg/deploy/k8s"
	"waverless/pkg/logger"
)

func (app *Application) initJobs() error {
	if app.workerService == nil || app.taskService == nil {
		logger.WarnCtx(app.ctx, "Service layer not fully initialized yet, skipping background task registration")
		return nil
	}

	manager := jobs.NewManager(app.ctx)

	workerInterval := time.Duration(app.config.Worker.HeartbeatTimeout) * time.Second
	if workerInterval <= 0 {
		workerInterval = time.Minute
	}

	// Create distributed locks to prevent multiple replicas from executing background cleanup tasks simultaneously
	// If Redis is unavailable, locks will automatically downgrade to single-instance mode
	var redisClient *redis.Client
	if app.redisClient != nil {
		redisClient = app.redisClient.GetClient()
	}

	workerCleanupLock := autoscaler.NewRedisDistributedLock(redisClient, "cleanup:worker-lock")
	taskTimeoutLock := autoscaler.NewRedisDistributedLock(redisClient, "cleanup:task-timeout-lock")
	orphanedTaskLock := autoscaler.NewRedisDistributedLock(redisClient, "cleanup:orphaned-task-lock")

	// Register background tasks with locks
	manager.Register(newWorkerCleanupJob(workerInterval, app.workerService, workerCleanupLock))
	manager.Register(newTaskTimeoutCleanupJob(5*time.Minute, app.taskService, taskTimeoutLock))
	manager.Register(newOrphanedTaskCleanupJob(30*time.Second, app.taskService, orphanedTaskLock))

	// Register GPU statistics aggregation tasks
	if app.gpuUsageService != nil {
		gpuMinuteLock := autoscaler.NewRedisDistributedLock(redisClient, "gpu:aggregate-minute-lock")
		gpuHourlyLock := autoscaler.NewRedisDistributedLock(redisClient, "gpu:aggregate-hourly-lock")
		gpuDailyLock := autoscaler.NewRedisDistributedLock(redisClient, "gpu:aggregate-daily-lock")
		gpuCleanupLock := autoscaler.NewRedisDistributedLock(redisClient, "gpu:cleanup-lock")

		manager.Register(newGPUMinuteAggregationJob(1*time.Minute, app.gpuUsageService, gpuMinuteLock))
		manager.Register(newGPUHourlyAggregationJob(5*time.Minute, app.gpuUsageService, gpuHourlyLock))
		manager.Register(newGPUDailyAggregationJob(1*time.Hour, app.gpuUsageService, gpuDailyLock))
		manager.Register(newGPUDataCleanupJob(24*time.Hour, app.gpuUsageService, gpuCleanupLock))
	}

	// Register task statistics refresh task
	if app.statisticsService != nil {
		statsRefreshLock := autoscaler.NewRedisDistributedLock(redisClient, "stats:refresh-lock")
		manager.Register(newStatisticsRefreshJob(10*time.Minute, app.statisticsService, statsRefreshLock))
	}

	app.jobsManager = manager
	return nil
}

// workerCleanupJob periodically removes offline workers.
type workerCleanupJob struct {
	interval        time.Duration
	workerService   *service.WorkerService
	distributedLock autoscaler.DistributedLock
}

func newWorkerCleanupJob(interval time.Duration, svc *service.WorkerService, lock autoscaler.DistributedLock) jobs.Job {
	return &workerCleanupJob{
		interval:        interval,
		workerService:   svc,
		distributedLock: lock,
	}
}

func (j *workerCleanupJob) Name() string {
	return "worker-cleanup"
}

func (j *workerCleanupJob) Interval() time.Duration {
	return j.interval
}

func (j *workerCleanupJob) Run(ctx context.Context) error {
	if j.workerService == nil {
		return fmt.Errorf("worker service not configured")
	}

	// Try to acquire distributed lock
	if j.distributedLock != nil {
		acquired, err := j.distributedLock.TryLock(ctx)
		if err != nil || !acquired {
			logger.DebugCtx(ctx, "another instance is running worker cleanup, skipping this cycle")
			return nil
		}
		defer j.distributedLock.Unlock(ctx)
	}

	logger.DebugCtx(ctx, "running worker cleanup job")
	return j.workerService.CleanupOfflineWorkers(ctx)
}

// taskTimeoutCleanupJob reconciles timed-out tasks as a fallback mechanism.
type taskTimeoutCleanupJob struct {
	interval        time.Duration
	taskService     *service.TaskService
	distributedLock autoscaler.DistributedLock
}

func newTaskTimeoutCleanupJob(interval time.Duration, svc *service.TaskService, lock autoscaler.DistributedLock) jobs.Job {
	return &taskTimeoutCleanupJob{
		interval:        interval,
		taskService:     svc,
		distributedLock: lock,
	}
}

func (j *taskTimeoutCleanupJob) Name() string {
	return "task-timeout-cleanup"
}

func (j *taskTimeoutCleanupJob) Interval() time.Duration {
	return j.interval
}

func (j *taskTimeoutCleanupJob) Run(ctx context.Context) error {
	if j.taskService == nil {
		return fmt.Errorf("task service not configured")
	}

	// Try to acquire distributed lock
	if j.distributedLock != nil {
		acquired, err := j.distributedLock.TryLock(ctx)
		if err != nil || !acquired {
			logger.DebugCtx(ctx, "another instance is running task timeout cleanup, skipping this cycle")
			return nil
		}
		defer j.distributedLock.Unlock(ctx)
	}

	logger.DebugCtx(ctx, "running task timeout cleanup job")
	return j.taskService.CleanupTimedOutTasks(ctx)
}

// orphanedTaskCleanupJob reclaims tasks assigned to nonexistent workers.
type orphanedTaskCleanupJob struct {
	interval        time.Duration
	taskService     *service.TaskService
	distributedLock autoscaler.DistributedLock
}

func newOrphanedTaskCleanupJob(interval time.Duration, svc *service.TaskService, lock autoscaler.DistributedLock) jobs.Job {
	return &orphanedTaskCleanupJob{
		interval:        interval,
		taskService:     svc,
		distributedLock: lock,
	}
}

func (j *orphanedTaskCleanupJob) Name() string {
	return "orphaned-task-cleanup"
}

func (j *orphanedTaskCleanupJob) Interval() time.Duration {
	return j.interval
}

func (j *orphanedTaskCleanupJob) Run(ctx context.Context) error {
	if j.taskService == nil {
		return fmt.Errorf("task service not configured")
	}

	// Try to acquire distributed lock
	if j.distributedLock != nil {
		acquired, err := j.distributedLock.TryLock(ctx)
		if err != nil || !acquired {
			logger.DebugCtx(ctx, "another instance is running orphaned task cleanup, skipping this cycle")
			return nil
		}
		defer j.distributedLock.Unlock(ctx)
	}

	logger.DebugCtx(ctx, "running orphaned task cleanup job")
	return j.taskService.CleanupOrphanedTasks(ctx)
}

// startPodCleanupJob starts Pod cleanup task (handles stuck Terminating Pods)
func (app *Application) startPodCleanupJob(k8sProvider *k8s.K8sDeploymentProvider) error {
	if k8sProvider == nil {
		logger.InfoCtx(app.ctx, "K8s provider not available, skipping pod cleanup job")
		return nil
	}

	logger.InfoCtx(app.ctx, "Starting pod cleanup job for stuck terminating pods...")

	// Start background task, check every 15 seconds
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-app.ctx.Done():
				logger.InfoCtx(app.ctx, "Pod cleanup job stopped")
				return
			case <-ticker.C:
				app.cleanupStuckTerminatingPods(k8sProvider)
			}
		}
	}()

	logger.InfoCtx(app.ctx, "✅ Pod cleanup job started")
	return nil
}

// cleanupStuckTerminatingPods cleans up stuck Terminating Pods
func (app *Application) cleanupStuckTerminatingPods(k8sProvider *k8s.K8sDeploymentProvider) {
	ctx := app.ctx

	// Get all Pods (including Terminating)
	pods, err := k8sProvider.GetPods(ctx, "")
	if err != nil {
		logger.WarnCtx(ctx, "Failed to get pods for cleanup check: %v", err)
		return
	}

	for _, pod := range pods {
		// Only process Terminating Pods
		if pod.DeletionTimestamp == "" {
			continue
		}

		// Check if Worker has running tasks (podName = workerID)
		workerID := pod.Name
		hasRunningTasks, err := app.hasRunningTasks(ctx, workerID)
		if err != nil {
			logger.WarnCtx(ctx, "Failed to check running tasks for worker %s: %v, skipping force delete", workerID, err)
			continue
		}

		if hasRunningTasks {
			logger.InfoCtx(ctx, "Worker %s has running tasks, NOT force deleting pod (waiting for tasks to complete)", workerID)
			continue
		}

		// ✅ Confirmed idle, force delete
		logger.WarnCtx(ctx, "🔥 Pod %s is stuck in Terminating with NO running tasks, force deleting...",
			pod.Name)

		if err := k8sProvider.ForceDeletePod(ctx, pod.Name); err != nil {
			logger.ErrorCtx(ctx, "Failed to force delete stuck pod %s: %v", pod.Name, err)
		} else {
			logger.InfoCtx(ctx, "✅ Successfully force deleted stuck pod %s", pod.Name)
		}
	}
}

// hasRunningTasks checks if worker has running tasks (via database)
func (app *Application) hasRunningTasks(ctx context.Context, workerID string) (bool, error) {
	// Query database directly via task repository
	tasks, err := app.mysqlRepo.Task.GetTasksByWorker(ctx, workerID)
	if err != nil {
		return false, fmt.Errorf("failed to get tasks for worker %s: %w", workerID, err)
	}

	// GetTasksByWorker 只返回 IN_PROGRESS 状态的任务
	return len(tasks) > 0, nil
}

// gpuMinuteAggregationJob aggregates GPU usage statistics by minute
type gpuMinuteAggregationJob struct {
	interval        time.Duration
	gpuUsageService *service.GPUUsageService
	distributedLock autoscaler.DistributedLock
}

func newGPUMinuteAggregationJob(interval time.Duration, svc *service.GPUUsageService, lock autoscaler.DistributedLock) jobs.Job {
	return &gpuMinuteAggregationJob{
		interval:        interval,
		gpuUsageService: svc,
		distributedLock: lock,
	}
}

func (j *gpuMinuteAggregationJob) Name() string {
	return "gpu-minute-aggregation"
}

func (j *gpuMinuteAggregationJob) Interval() time.Duration {
	return j.interval
}

func (j *gpuMinuteAggregationJob) Run(ctx context.Context) error {
	if j.gpuUsageService == nil {
		return fmt.Errorf("gpu usage service not configured")
	}

	// Try to acquire distributed lock
	if j.distributedLock != nil {
		acquired, err := j.distributedLock.TryLock(ctx)
		if err != nil || !acquired {
			logger.DebugCtx(ctx, "another instance is running GPU minute aggregation, skipping this cycle")
			return nil
		}
		defer j.distributedLock.Unlock(ctx)
	}

	logger.DebugCtx(ctx, "running GPU minute aggregation job")
	// Aggregate the previous minute (to ensure all records are captured)
	now := time.Now()
	endTime := now.Add(-1 * time.Minute).Truncate(time.Minute)
	startTime := endTime.Add(-1 * time.Minute)

	return j.gpuUsageService.AggregateStatistics(ctx, startTime, endTime, "minute")
}

// gpuHourlyAggregationJob aggregates GPU usage statistics by hour
type gpuHourlyAggregationJob struct {
	interval        time.Duration
	gpuUsageService *service.GPUUsageService
	distributedLock autoscaler.DistributedLock
}

func newGPUHourlyAggregationJob(interval time.Duration, svc *service.GPUUsageService, lock autoscaler.DistributedLock) jobs.Job {
	return &gpuHourlyAggregationJob{
		interval:        interval,
		gpuUsageService: svc,
		distributedLock: lock,
	}
}

func (j *gpuHourlyAggregationJob) Name() string {
	return "gpu-hourly-aggregation"
}

func (j *gpuHourlyAggregationJob) Interval() time.Duration {
	return j.interval
}

func (j *gpuHourlyAggregationJob) Run(ctx context.Context) error {
	if j.gpuUsageService == nil {
		return fmt.Errorf("gpu usage service not configured")
	}

	// Try to acquire distributed lock
	if j.distributedLock != nil {
		acquired, err := j.distributedLock.TryLock(ctx)
		if err != nil || !acquired {
			logger.DebugCtx(ctx, "another instance is running GPU hourly aggregation, skipping this cycle")
			return nil
		}
		defer j.distributedLock.Unlock(ctx)
	}

	logger.DebugCtx(ctx, "running GPU hourly aggregation job")
	// Aggregate the previous hour
	now := time.Now()
	endTime := now.Add(-1 * time.Hour).Truncate(time.Hour)
	startTime := endTime.Add(-1 * time.Hour)

	return j.gpuUsageService.AggregateStatistics(ctx, startTime, endTime, "hourly")
}

// gpuDailyAggregationJob aggregates GPU usage statistics by day
type gpuDailyAggregationJob struct {
	interval        time.Duration
	gpuUsageService *service.GPUUsageService
	distributedLock autoscaler.DistributedLock
}

func newGPUDailyAggregationJob(interval time.Duration, svc *service.GPUUsageService, lock autoscaler.DistributedLock) jobs.Job {
	return &gpuDailyAggregationJob{
		interval:        interval,
		gpuUsageService: svc,
		distributedLock: lock,
	}
}

func (j *gpuDailyAggregationJob) Name() string {
	return "gpu-daily-aggregation"
}

func (j *gpuDailyAggregationJob) Interval() time.Duration {
	return j.interval
}

func (j *gpuDailyAggregationJob) Run(ctx context.Context) error {
	if j.gpuUsageService == nil {
		return fmt.Errorf("gpu usage service not configured")
	}

	// Try to acquire distributed lock
	if j.distributedLock != nil {
		acquired, err := j.distributedLock.TryLock(ctx)
		if err != nil || !acquired {
			logger.DebugCtx(ctx, "another instance is running GPU daily aggregation, skipping this cycle")
			return nil
		}
		defer j.distributedLock.Unlock(ctx)
	}

	logger.DebugCtx(ctx, "running GPU daily aggregation job")
	// Aggregate the previous day
	now := time.Now()
	yesterday := now.AddDate(0, 0, -1)
	startTime := time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 0, 0, 0, 0, yesterday.Location())
	endTime := startTime.AddDate(0, 0, 1)

	return j.gpuUsageService.AggregateStatistics(ctx, startTime, endTime, "daily")
}

// gpuDataCleanupJob cleans up old GPU usage statistics data
type gpuDataCleanupJob struct {
	interval        time.Duration
	gpuUsageService *service.GPUUsageService
	distributedLock autoscaler.DistributedLock
}

func newGPUDataCleanupJob(interval time.Duration, svc *service.GPUUsageService, lock autoscaler.DistributedLock) jobs.Job {
	return &gpuDataCleanupJob{
		interval:        interval,
		gpuUsageService: svc,
		distributedLock: lock,
	}
}

func (j *gpuDataCleanupJob) Name() string {
	return "gpu-data-cleanup"
}

func (j *gpuDataCleanupJob) Interval() time.Duration {
	return j.interval
}

func (j *gpuDataCleanupJob) Run(ctx context.Context) error {
	if j.gpuUsageService == nil {
		return fmt.Errorf("gpu usage service not configured")
	}

	// Try to acquire distributed lock
	if j.distributedLock != nil {
		acquired, err := j.distributedLock.TryLock(ctx)
		if err != nil || !acquired {
			logger.DebugCtx(ctx, "another instance is running GPU data cleanup, skipping this cycle")
			return nil
		}
		defer j.distributedLock.Unlock(ctx)
	}

	logger.InfoCtx(ctx, "running GPU data cleanup job")
	return j.gpuUsageService.CleanupOldStatistics(ctx)
}

// statisticsRefreshJob periodically refreshes task statistics from the tasks table
type statisticsRefreshJob struct {
	interval          time.Duration
	statisticsService *service.StatisticsService
	distributedLock   autoscaler.DistributedLock
}

func newStatisticsRefreshJob(interval time.Duration, svc *service.StatisticsService, lock autoscaler.DistributedLock) jobs.Job {
	return &statisticsRefreshJob{
		interval:          interval,
		statisticsService: svc,
		distributedLock:   lock,
	}
}

func (j *statisticsRefreshJob) Name() string {
	return "statistics-refresh"
}

func (j *statisticsRefreshJob) Interval() time.Duration {
	return j.interval
}

func (j *statisticsRefreshJob) Run(ctx context.Context) error {
	if j.statisticsService == nil {
		return fmt.Errorf("statistics service not configured")
	}

	// Try to acquire distributed lock
	if j.distributedLock != nil {
		acquired, err := j.distributedLock.TryLock(ctx)
		if err != nil || !acquired {
			logger.DebugCtx(ctx, "another instance is running statistics refresh, skipping this cycle")
			return nil
		}
		defer j.distributedLock.Unlock(ctx)
	}

	logger.InfoCtx(ctx, "running statistics refresh job")
	return j.statisticsService.RefreshAllStatistics(ctx)
}
