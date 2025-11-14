package mysql

import (
	"context"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TaskRepository handles task persistence in MySQL
type TaskRepository struct {
	ds *Datastore
}

// NewTaskRepository creates a new task repository
func NewTaskRepository(ds *Datastore) *TaskRepository {
	return &TaskRepository{ds: ds}
}

// Create creates a new task
func (r *TaskRepository) Create(ctx context.Context, task *Task) error {
	return r.ds.DB(ctx).Create(task).Error
}

// Get retrieves a task by ID
func (r *TaskRepository) Get(ctx context.Context, taskID string) (*Task, error) {
	var task Task
	err := r.ds.DB(ctx).Where("task_id = ?", taskID).First(&task).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get task: %w", err)
	}
	return &task, nil
}

// UpdateFields updates specific fields of a task by task_id
func (r *TaskRepository) UpdateFields(ctx context.Context, taskID string, updates map[string]interface{}) error {
	return r.ds.DB(ctx).Model(&Task{}).
		Where("task_id = ?", taskID).
		Updates(updates).Error
}

// UpdateStatus updates task status with atomic state transition (CAS - Compare And Swap)
// This prevents concurrent updates and ensures valid state transitions
// Returns error if task not found or current status doesn't match fromStatus
func (r *TaskRepository) UpdateStatus(ctx context.Context, taskID string, fromStatus, toStatus string) error {
	result := r.ds.DB(ctx).Model(&Task{}).
		Where("task_id = ? AND status = ?", taskID, fromStatus).
		Update("status", toStatus)

	if result.Error != nil {
		return fmt.Errorf("failed to update task status: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("task not found or invalid status transition: task_id=%s, from=%s, to=%s", taskID, fromStatus, toStatus)
	}

	return nil
}

// UpdateStatusUnsafe updates task status without checking current status
// Use this only when you're certain about the state (e.g., from Get())
// For concurrent-safe updates, use UpdateStatus() with fromStatus
func (r *TaskRepository) UpdateStatusUnsafe(ctx context.Context, taskID string, status string) error {
	return r.ds.DB(ctx).Model(&Task{}).
		Where("task_id = ?", taskID).
		Update("status", status).Error
}

// Delete deletes a task
func (r *TaskRepository) Delete(ctx context.Context, taskID string) error {
	return r.ds.DB(ctx).Where("task_id = ?", taskID).Delete(&Task{}).Error
}

// ListByEndpoint retrieves tasks by endpoint
func (r *TaskRepository) ListByEndpoint(ctx context.Context, endpoint string, limit int) ([]*Task, error) {
	if limit <= 0 {
		limit = 100
	}

	var tasks []*Task
	err := r.ds.DB(ctx).
		Where("endpoint = ?", endpoint).
		Order("created_at DESC").
		Limit(limit).
		Find(&tasks).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list tasks by endpoint: %w", err)
	}
	return tasks, nil
}

// ListByStatus retrieves tasks by status
func (r *TaskRepository) ListByStatus(ctx context.Context, status string, limit int) ([]*Task, error) {
	if limit <= 0 {
		limit = 100
	}

	var tasks []*Task
	err := r.ds.DB(ctx).
		Where("status = ?", status).
		Order("created_at DESC").
		Limit(limit).
		Find(&tasks).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list tasks by status: %w", err)
	}
	return tasks, nil
}

// ListByEndpointAndStatus retrieves tasks by endpoint and status
func (r *TaskRepository) ListByEndpointAndStatus(ctx context.Context, endpoint, status string, limit int) ([]*Task, error) {
	if limit <= 0 {
		limit = 100
	}

	var tasks []*Task
	err := r.ds.DB(ctx).
		Where("endpoint = ? AND status = ?", endpoint, status).
		Order("created_at DESC").
		Limit(limit).
		Find(&tasks).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list tasks by endpoint and status: %w", err)
	}
	return tasks, nil
}

// GetInProgressTasks retrieves all in-progress task IDs
// This is used for orphaned task detection
func (r *TaskRepository) GetInProgressTasks(ctx context.Context) ([]string, error) {
	var taskIDs []string
	err := r.ds.DB(ctx).Model(&Task{}).
		Where("status = ?", "IN_PROGRESS").
		Pluck("task_id", &taskIDs).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get in-progress tasks: %w", err)
	}
	return taskIDs, nil
}

// GetInProgressTasksByEndpoint retrieves in-progress task IDs for a specific endpoint
func (r *TaskRepository) GetInProgressTasksByEndpoint(ctx context.Context, endpoint string) ([]string, error) {
	var taskIDs []string
	err := r.ds.DB(ctx).Model(&Task{}).
		Where("endpoint = ? AND status = ?", endpoint, "IN_PROGRESS").
		Pluck("task_id", &taskIDs).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get in-progress tasks by endpoint: %w", err)
	}
	return taskIDs, nil
}

// GetTasksByWorker retrieves tasks assigned to a worker
func (r *TaskRepository) GetTasksByWorker(ctx context.Context, workerID string) ([]*Task, error) {
	var tasks []*Task
	err := r.ds.DB(ctx).
		Where("worker_id = ? AND status = ?", workerID, "IN_PROGRESS").
		Find(&tasks).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get tasks by worker: %w", err)
	}
	return tasks, nil
}

// CountByEndpointAndStatus counts tasks by endpoint and status
func (r *TaskRepository) CountByEndpointAndStatus(ctx context.Context, endpoint, status string) (int64, error) {
	var count int64
	err := r.ds.DB(ctx).Model(&Task{}).
		Where("endpoint = ? AND status = ?", endpoint, status).
		Count(&count).Error
	if err != nil {
		return 0, fmt.Errorf("failed to count tasks: %w", err)
	}
	return count, nil
}

// CountInProgressByEndpoint counts in-progress tasks for an endpoint
func (r *TaskRepository) CountInProgressByEndpoint(ctx context.Context, endpoint string) (int64, error) {
	return r.CountByEndpointAndStatus(ctx, endpoint, "IN_PROGRESS")
}

// GetOldestInProgressTasks retrieves the oldest N in-progress tasks for timeout detection
func (r *TaskRepository) GetOldestInProgressTasks(ctx context.Context, limit int) ([]*Task, error) {
	var tasks []*Task
	err := r.ds.DB(ctx).
		Where("status = ?", "IN_PROGRESS").
		Order("started_at ASC").
		Limit(limit).
		Find(&tasks).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get oldest in-progress tasks: %w", err)
	}
	return tasks, nil
}

// BatchUpdateStatus updates status for multiple tasks in a transaction
func (r *TaskRepository) BatchUpdateStatus(ctx context.Context, taskIDs []string, status string) error {
	if len(taskIDs) == 0 {
		return nil
	}

	return r.ds.ExecTx(ctx, func(txCtx context.Context) error {
		return r.ds.DB(txCtx).Model(&Task{}).
			Where("task_id IN ?", taskIDs).
			Update("status", status).Error
	})
}

// List retrieves tasks with optional filters
func (r *TaskRepository) List(ctx context.Context, filters map[string]interface{}, limit, offset int) ([]*Task, error) {
	if limit <= 0 {
		limit = 100
	}

	query := r.ds.DB(ctx).Model(&Task{})

	// Apply filters
	for key, value := range filters {
		query = query.Where(key+" = ?", value)
	}

	var tasks []*Task
	err := query.
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&tasks).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list tasks: %w", err)
	}
	return tasks, nil
}

// ListWithTaskID retrieves tasks with optional filters and task_id exact match
func (r *TaskRepository) ListWithTaskID(ctx context.Context, filters map[string]interface{}, taskID string, limit, offset int) ([]*Task, error) {
	if limit <= 0 {
		limit = 100
	}

	query := r.ds.DB(ctx).Model(&Task{})

	// Apply filters
	for key, value := range filters {
		query = query.Where(key+" = ?", value)
	}

	// Apply task_id exact match if provided (uses index)
	if taskID != "" {
		query = query.Where("task_id = ?", taskID)
	}

	var tasks []*Task
	err := query.
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&tasks).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list tasks: %w", err)
	}
	return tasks, nil
}

// Count counts tasks with optional filters
func (r *TaskRepository) Count(ctx context.Context, filters map[string]interface{}) (int64, error) {
	query := r.ds.DB(ctx).Model(&Task{})

	// Apply filters
	for key, value := range filters {
		query = query.Where(key+" = ?", value)
	}

	var count int64
	err := query.Count(&count).Error
	if err != nil {
		return 0, fmt.Errorf("failed to count tasks: %w", err)
	}
	return count, nil
}

// CountWithTaskID counts tasks with optional filters and task_id exact match
func (r *TaskRepository) CountWithTaskID(ctx context.Context, filters map[string]interface{}, taskID string) (int64, error) {
	query := r.ds.DB(ctx).Model(&Task{})

	// Apply filters
	for key, value := range filters {
		query = query.Where(key+" = ?", value)
	}

	// Apply task_id exact match if provided (uses index)
	if taskID != "" {
		query = query.Where("task_id = ?", taskID)
	}

	var count int64
	err := query.Count(&count).Error
	if err != nil {
		return 0, fmt.Errorf("failed to count tasks: %w", err)
	}
	return count, nil
}

// GetPendingTasksByEndpoint retrieves pending tasks for endpoint (sorted by priority)
func (r *TaskRepository) GetPendingTasksByEndpoint(ctx context.Context, endpoint string, limit int) ([]*Task, error) {
	var tasks []*Task
	query := r.ds.DB(ctx).
		Where("endpoint = ? AND status = ?", endpoint, "PENDING").
		Order("priority DESC, created_at ASC")

	if limit > 0 {
		query = query.Limit(limit)
	}

	err := query.Find(&tasks).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get pending tasks: %w", err)
	}
	return tasks, nil
}

// GetRunningTasksCount retrieves count of running tasks
func (r *TaskRepository) GetRunningTasksCount(ctx context.Context, endpoint string) (int64, error) {
	var count int64
	err := r.ds.DB(ctx).Model(&Task{}).
		Where("endpoint = ? AND status = ?", endpoint, "IN_PROGRESS").
		Count(&count).Error
	if err != nil {
		return 0, fmt.Errorf("failed to count running tasks: %w", err)
	}
	return count, nil
}

// GetOldestPendingTask retrieves the oldest pending task (for starvation detection)
func (r *TaskRepository) GetOldestPendingTask(ctx context.Context, endpoint string) (*Task, error) {
	var task Task
	err := r.ds.DB(ctx).
		Where("endpoint = ? AND status = ?", endpoint, "PENDING").
		Order("created_at ASC").
		First(&task).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get oldest pending task: %w", err)
	}
	return &task, nil
}

// SelectPendingTasksForUpdate queries and locks PENDING tasks (without updating status)
// Uses SELECT FOR UPDATE row lock to ensure same task won't be pulled by multiple workers simultaneously
// This function only handles query and locking, not status update, to avoid rollback needs
func (r *TaskRepository) SelectPendingTasksForUpdate(ctx context.Context, endpoint string, limit int) ([]*Task, error) {
	var tasks []*Task

	// Use transaction + SELECT FOR UPDATE to query and lock tasks
	err := r.ds.ExecTx(ctx, func(txCtx context.Context) error {
		// Query PENDING tasks and add row lock (SELECT FOR UPDATE)
		err := r.ds.DB(txCtx).
			Where("endpoint = ? AND status = ?", endpoint, "PENDING").
			Order("created_at ASC"). // Earlier creation time has priority (FIFO)
			Limit(limit).
			Clauses(clause.Locking{Strength: "UPDATE"}). // SELECT FOR UPDATE
			Find(&tasks).Error
		if err != nil {
			return fmt.Errorf("failed to select pending tasks: %w", err)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return tasks, nil
}

// AssignTasksToWorker atomically assigns tasks to worker (CAS update)
// Uses status as CAS condition to ensure only PENDING tasks will be updated
// This function completes all updates in one transaction: status, worker_id, started_at, extend
func (r *TaskRepository) AssignTasksToWorker(ctx context.Context, taskIDs []string, workerID string) ([]*Task, error) {
	if len(taskIDs) == 0 {
		return []*Task{}, nil
	}

	var updatedTasks []*Task

	// Use transaction to atomically update multiple tasks
	err := r.ds.ExecTx(ctx, func(txCtx context.Context) error {
		now := r.ds.GetDB().NowFunc()

		// Update each task individually (need to update extend field, can't batch update JSON)
		for _, taskID := range taskIDs {
			// 1. First query task (ensure status is PENDING)
			var task Task
			err := r.ds.DB(txCtx).
				Where("task_id = ? AND status = ?", taskID, "PENDING").
				First(&task).Error
			if err != nil {
				if err == gorm.ErrRecordNotFound {
					// Task doesn't exist or status changed, skip
					continue
				}
				return fmt.Errorf("failed to get task %s: %w", taskID, err)
			}

			// 2. Update status, worker_id, started_at
			task.Status = "IN_PROGRESS"
			task.WorkerID = workerID
			task.StartedAt = &now

			// 3. Add execution record to extend field
			task.AddExecutionRecord(workerID, now)

			// 4. CAS update (only update when status=PENDING)
			result := r.ds.DB(txCtx).Model(&Task{}).
				Where("task_id = ? AND status = ?", taskID, "PENDING").
				Updates(map[string]interface{}{
					"status":     "IN_PROGRESS",
					"worker_id":  workerID,
					"started_at": now,
					"extend":     task.Extend,
				})

			if result.Error != nil {
				return fmt.Errorf("failed to update task %s: %w", taskID, result.Error)
			}

			// Only add successfully updated tasks to return list
			if result.RowsAffected > 0 {
				updatedTasks = append(updatedTasks, &task)
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return updatedTasks, nil
}
