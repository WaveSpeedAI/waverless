package mysql

import (
	"context"
	"encoding/json"
	"time"

	"waverless/pkg/constants"
	"waverless/pkg/logger"
	"waverless/pkg/store/mysql/model"

	"gorm.io/gorm"
)

// WorkerRepository handles worker database operations
type WorkerRepository struct {
	ds *Datastore
}

// NewWorkerRepository creates a new worker repository
func NewWorkerRepository(ds *Datastore) *WorkerRepository {
	return &WorkerRepository{ds: ds}
}

// UpdateHeartbeat updates worker heartbeat, status and jobs (sets status to ONLINE/BUSY)
func (r *WorkerRepository) UpdateHeartbeat(ctx context.Context, workerID, endpoint string, jobsInProgress []string, jobsInProgressCount int, version string) error {
	now := time.Now()
	currentJobs := len(jobsInProgress)
	if currentJobs == 0 && jobsInProgressCount > 0 {
		currentJobs = jobsInProgressCount
	}

	status := constants.WorkerStatusOnline
	if currentJobs > 0 {
		status = constants.WorkerStatusBusy
	}

	jobsJSON, _ := json.Marshal(jobsInProgress)

	updates := map[string]interface{}{
		"endpoint":         endpoint,
		"status":           gorm.Expr("CASE WHEN status = ? THEN status ELSE ? END", constants.WorkerStatusDraining, status),
		"current_jobs":     currentJobs,
		"jobs_in_progress": string(jobsJSON),
		"last_heartbeat":   now,
		"updated_at":       now,
	}
	// Only update version if provided (don't overwrite with empty)
	if version != "" {
		updates["version"] = version
	}

	// Update existing worker (preserve DRAINING status)
	result := r.ds.DB(ctx).Model(&model.Worker{}).
		Where("worker_id = ?", workerID).
		Updates(updates)

	if result.Error != nil {
		return result.Error
	}

	// If worker doesn't exist, create it
	if result.RowsAffected == 0 {
		worker := &model.Worker{
			WorkerID:       workerID,
			Endpoint:       endpoint,
			PodName:        workerID,
			Status:         status.String(),
			Concurrency:    1,
			CurrentJobs:    currentJobs,
			JobsInProgress: string(jobsJSON),
			Version:        version,
			LastHeartbeat:  now,
			LastTaskTime:   &now,
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		return r.ds.DB(ctx).Create(worker).Error
	}

	return nil
}

// UpsertFromPod creates or updates worker from pod watch events (status STARTING until heartbeat)
func (r *WorkerRepository) UpsertFromPod(ctx context.Context, podName, endpoint, phase, status, reason, message, ip, nodeName string, createdAt, startedAt *time.Time) error {
	now := time.Now()

	logger.InfoCtx(ctx, "UpsertFromPod: pod_name=%s, endpoint=%s, phase=%s, status=%s, reason=%s", podName, endpoint, phase, status, reason)

	runtimeState := map[string]interface{}{
		"phase":    phase,
		"status":   status,
		"reason":   reason,
		"message":  message,
		"ip":       ip,
		"nodeName": nodeName,
	}
	if createdAt != nil {
		runtimeState["createdAt"] = createdAt.Format(time.RFC3339)
	}
	if startedAt != nil {
		runtimeState["startedAt"] = startedAt.Format(time.RFC3339)
	}

	updates := map[string]interface{}{
		"runtime_state": JSONMap(runtimeState),
		"updated_at":    now,
	}

	// Update time fields for metrics
	if createdAt != nil {
		updates["pod_created_at"] = createdAt
	}
	if startedAt != nil {
		updates["pod_started_at"] = startedAt
	}

	// Calculate cold start duration
	if createdAt != nil && startedAt != nil {
		coldStartMs := startedAt.Sub(*createdAt).Milliseconds()
		if coldStartMs > 0 {
			updates["cold_start_duration_ms"] = coldStartMs
		}
	}

	// Update pod_ready_at when Ready
	if reason == "Ready" && status == "Running" {
		updates["pod_ready_at"] = now
	}

	// Try update first
	result := r.ds.DB(ctx).Model(&model.Worker{}).
		Where("pod_name = ?", podName).
		Updates(updates)

	if result.Error != nil {
		logger.ErrorCtx(ctx, "UpsertFromPod: update failed for pod_name=%s: %v", podName, result.Error)
		return result.Error
	}

	// If no rows updated, create new worker (STARTING until heartbeat)
	if result.RowsAffected == 0 {
		logger.InfoCtx(ctx, "UpsertFromPod: creating new worker for pod_name=%s, endpoint=%s", podName, endpoint)
		worker := &model.Worker{
			WorkerID:            podName,
			Endpoint:            endpoint,
			PodName:             podName,
			Status:              constants.WorkerStatusStarting.String(),
			Concurrency:         1,
			RuntimeState:        runtimeState,
			PodCreatedAt:        createdAt,
			PodStartedAt:        startedAt,
			ColdStartDurationMs: nil,
			LastHeartbeat:       now,
			CreatedAt:           now,
			UpdatedAt:           now,
		}
		if createdAt != nil && startedAt != nil {
			coldStartMs := startedAt.Sub(*createdAt).Milliseconds()
			if coldStartMs > 0 {
				worker.ColdStartDurationMs = &coldStartMs
			}
		}
		if err := r.ds.DB(ctx).Create(worker).Error; err != nil {
			logger.ErrorCtx(ctx, "UpsertFromPod: create failed for pod_name=%s: %v", podName, err)
			return err
		}
		logger.InfoCtx(ctx, "UpsertFromPod: successfully created worker for pod_name=%s", podName)
	} else {
		logger.InfoCtx(ctx, "UpsertFromPod: updated %d worker(s) for pod_name=%s", result.RowsAffected, podName)
	}

	return nil
}

// UpdateLastTaskTime updates last task time when worker becomes idle
func (r *WorkerRepository) UpdateLastTaskTime(ctx context.Context, workerID string) error {
	now := time.Now()
	return r.ds.DB(ctx).Model(&model.Worker{}).
		Where("worker_id = ?", workerID).
		Updates(map[string]interface{}{
			"last_task_time": now,
			"updated_at":     now,
		}).Error
}

// UpdateStatus updates worker status
func (r *WorkerRepository) UpdateStatus(ctx context.Context, workerID string, status string) error {
	return r.ds.DB(ctx).Model(&model.Worker{}).
		Where("worker_id = ?", workerID).
		Updates(map[string]interface{}{
			"status":     status,
			"updated_at": time.Now(),
		}).Error
}

// IncrementTaskStats increments task completion statistics
func (r *WorkerRepository) IncrementTaskStats(ctx context.Context, workerID string, completed bool, executionTimeMs int64) error {
	return r.IncrementTaskStatsAt(ctx, workerID, completed, executionTimeMs, time.Now())
}

// IncrementTaskStatsAt increments task completion statistics with specific time
func (r *WorkerRepository) IncrementTaskStatsAt(ctx context.Context, workerID string, completed bool, executionTimeMs int64, completedAt time.Time) error {
	updates := map[string]interface{}{
		"last_task_time":          completedAt,
		"total_execution_time_ms": gorm.Expr("total_execution_time_ms + ?", executionTimeMs),
		"updated_at":              completedAt,
	}
	if completed {
		updates["total_tasks_completed"] = gorm.Expr("total_tasks_completed + 1")
	} else {
		updates["total_tasks_failed"] = gorm.Expr("total_tasks_failed + 1")
	}
	return r.ds.DB(ctx).Model(&model.Worker{}).Where("worker_id = ?", workerID).Updates(updates).Error
}

// Get gets a worker by worker ID
func (r *WorkerRepository) Get(ctx context.Context, workerID string) (*model.Worker, error) {
	var worker model.Worker
	err := r.ds.DB(ctx).Where("worker_id = ?", workerID).First(&worker).Error
	if err != nil {
		return nil, err
	}
	return &worker, nil
}

// GetByID gets a worker by database ID (regardless of status)
func (r *WorkerRepository) GetByID(ctx context.Context, id int64) (*model.Worker, error) {
	var worker model.Worker
	err := r.ds.DB(ctx).Where("id = ?", id).First(&worker).Error
	if err != nil {
		return nil, err
	}
	return &worker, nil
}

// GetByPodName gets a worker by pod name and endpoint
func (r *WorkerRepository) GetByPodName(ctx context.Context, endpoint, podName string) (*model.Worker, error) {
	var worker model.Worker
	err := r.ds.DB(ctx).Where("endpoint = ? AND pod_name = ?", endpoint, podName).First(&worker).Error
	if err != nil {
		return nil, err
	}
	return &worker, nil
}

// GetByEndpoint lists active workers for an endpoint
func (r *WorkerRepository) GetByEndpoint(ctx context.Context, endpoint string) ([]*model.Worker, error) {
	var workers []*model.Worker
	err := r.ds.DB(ctx).Where("endpoint = ? AND status != ?", endpoint, constants.WorkerStatusOffline).Find(&workers).Error
	return workers, err
}

// GetByEndpointForSync lists workers for an endpoint including recently terminated ones
// Used by Portal for billing sync - includes OFFLINE workers terminated within the last hour
func (r *WorkerRepository) GetByEndpointForSync(ctx context.Context, endpoint string) ([]*model.Worker, error) {
	var workers []*model.Worker
	oneHourAgo := time.Now().Add(-1 * time.Hour)
	err := r.ds.DB(ctx).Where(
		"endpoint = ? AND (status != ? OR (status = ? AND terminated_at > ?))",
		endpoint, constants.WorkerStatusOffline, constants.WorkerStatusOffline, oneHourAgo,
	).Find(&workers).Error
	return workers, err
}

// GetAll lists all active workers
func (r *WorkerRepository) GetAll(ctx context.Context) ([]*model.Worker, error) {
	var workers []*model.Worker
	err := r.ds.DB(ctx).Where("status != ?", constants.WorkerStatusOffline).Find(&workers).Error
	return workers, err
}

// MarkOffline marks workers as offline if heartbeat is stale (excludes STARTING workers)
func (r *WorkerRepository) MarkOffline(ctx context.Context, heartbeatThreshold time.Duration) (int64, error) {
	threshold := time.Now().Add(-heartbeatThreshold)
	result := r.ds.DB(ctx).Model(&model.Worker{}).
		Where("last_heartbeat < ? AND status NOT IN ?", threshold, []string{constants.WorkerStatusOffline.String(), constants.WorkerStatusStarting.String()}).
		Updates(map[string]interface{}{
			"status":       constants.WorkerStatusOffline,
			"current_jobs": 0,
			"updated_at":   time.Now(),
		})
	return result.RowsAffected, result.Error
}

// GetStaleWorkers returns workers that will be marked offline
func (r *WorkerRepository) GetStaleWorkers(ctx context.Context, threshold time.Time) ([]*model.Worker, error) {
	var workers []*model.Worker
	err := r.ds.DB(ctx).Where("last_heartbeat < ? AND status NOT IN ?", threshold, []string{constants.WorkerStatusOffline.String(), constants.WorkerStatusStarting.String()}).Find(&workers).Error
	return workers, err
}

// MarkOfflineByPodName marks a specific worker as offline by pod name
func (r *WorkerRepository) MarkOfflineByPodName(ctx context.Context, podName string) error {
	now := time.Now()
	return r.ds.DB(ctx).Model(&model.Worker{}).
		Where("pod_name = ?", podName).
		Updates(map[string]interface{}{
			"status":        constants.WorkerStatusOffline,
			"current_jobs":  0,
			"terminated_at": now,
			"updated_at":    now,
		}).Error
}

// Delete deletes a worker record
func (r *WorkerRepository) Delete(ctx context.Context, workerID string) error {
	return r.ds.DB(ctx).Where("worker_id = ?", workerID).Delete(&model.Worker{}).Error
}

// UpdateWorkerFailure updates worker failure information.
// This method is called when a worker enters a failed state to record the failure details.
//
// IMPORTANT: failure_occurred_at is only set on the FIRST failure detection.
// Subsequent updates will NOT overwrite this timestamp, ensuring the Resource Releaser
// can correctly calculate the timeout duration from the first failure.
//
// Parameters:
//   - ctx: Context for database operations
//   - podName: The pod name (used to identify the worker)
//   - failureType: The type of failure (IMAGE_PULL_FAILED, CONTAINER_CRASH, etc.)
//   - failureReason: The sanitized user-friendly failure message
//   - failureDetails: JSON string with full failure details for debugging
//   - occurredAt: The timestamp when the failure occurred (only used if first failure)
//
// Returns:
//   - error if the database update fails
//
// Validates: Requirements 3.3, 3.4, 6.1, 6.2
func (r *WorkerRepository) UpdateWorkerFailure(ctx context.Context, podName, failureType, failureReason, failureDetails string, occurredAt time.Time) error {
	logger.InfoCtx(ctx, "UpdateWorkerFailure: attempting to update pod_name=%s, type=%s, reason=%s", podName, failureType, failureReason)

	// First check if worker exists and if it already has a failure_occurred_at
	var worker model.Worker
	err := r.ds.DB(ctx).Where("pod_name = ?", podName).First(&worker).Error
	if err != nil {
		logger.WarnCtx(ctx, "UpdateWorkerFailure: worker not found with pod_name=%s: %v", podName, err)
		return nil // Worker doesn't exist, nothing to update
	}

	// Build update map - always update failure info
	updates := map[string]interface{}{
		"failure_type":    failureType,
		"failure_reason":  failureReason,
		"failure_details": failureDetails,
		"updated_at":      time.Now(),
	}

	// CRITICAL: Only set failure_occurred_at if it's not already set
	// This ensures the Resource Releaser can correctly calculate timeout from FIRST failure
	if worker.FailureOccurredAt == nil {
		updates["failure_occurred_at"] = occurredAt
		logger.InfoCtx(ctx, "UpdateWorkerFailure: setting initial failure_occurred_at for pod_name=%s", podName)
	} else {
		logger.DebugCtx(ctx, "UpdateWorkerFailure: preserving existing failure_occurred_at=%v for pod_name=%s",
			worker.FailureOccurredAt, podName)
	}

	result := r.ds.DB(ctx).Model(&model.Worker{}).
		Where("pod_name = ?", podName).
		Updates(updates)

	if result.Error != nil {
		logger.ErrorCtx(ctx, "UpdateWorkerFailure: database error for pod_name=%s: %v", podName, result.Error)
		return result.Error
	}

	logger.InfoCtx(ctx, "UpdateWorkerFailure: successfully updated worker pod_name=%s, type=%s", podName, failureType)
	return nil
}

// ClearWorkerFailure clears the failure information for a worker.
// This method is called when a worker recovers from a failed state.
//
// Parameters:
//   - ctx: Context for database operations
//   - podName: The pod name (used to identify the worker)
//
// Returns:
//   - error if the database update fails
func (r *WorkerRepository) ClearWorkerFailure(ctx context.Context, podName string) error {
	return r.ds.DB(ctx).Model(&model.Worker{}).
		Where("pod_name = ?", podName).
		Updates(map[string]interface{}{
			"failure_type":        nil,
			"failure_reason":      nil,
			"failure_details":     nil,
			"failure_occurred_at": nil,
			"updated_at":          time.Now(),
		}).Error
}

// GetWorkersWithFailure returns all workers that have failure information.
// This is useful for monitoring and reporting purposes.
//
// Parameters:
//   - ctx: Context for database operations
//   - endpoint: Optional endpoint filter (empty string for all endpoints)
//
// Returns:
//   - List of workers with failure information
//   - error if the database query fails
func (r *WorkerRepository) GetWorkersWithFailure(ctx context.Context, endpoint string) ([]*model.Worker, error) {
	var workers []*model.Worker
	query := r.ds.DB(ctx).Where("failure_type IS NOT NULL AND failure_type != ''")
	if endpoint != "" {
		query = query.Where("endpoint = ?", endpoint)
	}
	err := query.Find(&workers).Error
	return workers, err
}

// GetWorkersByFailureType returns all active workers with a specific failure type.
// It excludes OFFLINE workers since they are no longer running.
//
// Parameters:
//   - ctx: Context for database operations
//   - failureType: The failure type to filter by
//
// Returns:
//   - List of active workers with the specified failure type
//   - error if the database query fails
func (r *WorkerRepository) GetWorkersByFailureType(ctx context.Context, failureType string) ([]*model.Worker, error) {
	var workers []*model.Worker
	err := r.ds.DB(ctx).Where("failure_type = ? AND status != ?", failureType, "OFFLINE").Find(&workers).Error
	return workers, err
}
