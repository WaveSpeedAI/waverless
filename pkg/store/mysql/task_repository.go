package mysql

import (
	"context"
	"fmt"
	"time"

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

// UpdateFieldsWithStatus updates specific fields of a task with CAS (Compare-And-Swap) on status
// This prevents concurrent updates by ensuring the task status matches expectedStatus before updating
// Returns error if task not found or status doesn't match expectedStatus
func (r *TaskRepository) UpdateFieldsWithStatus(ctx context.Context, taskID string, expectedStatus string, updates map[string]interface{}) error {
	result := r.ds.DB(ctx).Model(&Task{}).
		Where("task_id = ? AND status = ?", taskID, expectedStatus).
		Updates(updates)

	if result.Error != nil {
		return fmt.Errorf("failed to update task: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("task not found or status changed (expected: %s): task_id=%s", expectedStatus, taskID)
	}

	return nil
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

// CountByStatus counts tasks by status globally
func (r *TaskRepository) CountByStatus(ctx context.Context, status string) (int64, error) {
	var count int64
	err := r.ds.DB(ctx).Model(&Task{}).Where("status = ?", status).Count(&count).Error
	return count, err
}

// CountInProgressByEndpoint counts in-progress tasks for an endpoint
func (r *TaskRepository) CountInProgressByEndpoint(ctx context.Context, endpoint string) (int64, error) {
	return r.CountByEndpointAndStatus(ctx, endpoint, "IN_PROGRESS")
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
		Order("id DESC").
		Limit(limit).
		Offset(offset).
		Find(&tasks).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list tasks: %w", err)
	}
	return tasks, nil
}

// ListWithTaskIDExcludeInput retrieves tasks excluding the input field (performance optimization)
// This avoids fetching potentially large input data when not needed (e.g., in list views)
func (r *TaskRepository) ListWithTaskIDExcludeInput(ctx context.Context, filters map[string]interface{}, taskID string, limit, offset int) ([]*Task, error) {
	if limit <= 0 {
		limit = 100
	}

	// Exclude both input and output fields to avoid sort memory issues
	// These fields can contain large JSON data (e.g., base64 images)
	query := r.ds.DB(ctx).Model(&Task{}).
		Select("id", "task_id", "endpoint", "status", "error", "worker_id", "webhook_url", "created_at", "updated_at", "started_at", "completed_at", "extend")

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
		Order("id DESC").
		Limit(limit).
		Offset(offset).
		Find(&tasks).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list tasks: %w", err)
	}
	return tasks, nil
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

// SelectAndAssignTasks atomically selects PENDING tasks and assigns them to worker in one transaction
// This prevents race condition where multiple workers grab the same task
func (r *TaskRepository) SelectAndAssignTasks(ctx context.Context, endpoint string, limit int, workerID string) ([]*Task, error) {
	var assignedTasks []*Task

	err := r.ds.ExecTx(ctx, func(txCtx context.Context) error {
		// 1. SELECT FOR UPDATE to lock PENDING tasks
		var tasks []*Task
		err := r.ds.DB(txCtx).
			Where("endpoint = ? AND status = ?", endpoint, "PENDING").
			Order("id ASC").
			Limit(limit).
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Find(&tasks).Error
		if err != nil {
			return fmt.Errorf("failed to select pending tasks: %w", err)
		}

		if len(tasks) == 0 {
			return nil
		}

		now := r.ds.GetDB().NowFunc()

		// 2. Update each task in the same transaction
		for _, task := range tasks {
			task.Status = "IN_PROGRESS"
			task.WorkerID = workerID
			task.StartedAt = &now
			task.AddExecutionRecord(workerID, now)

			err := r.ds.DB(txCtx).Save(task).Error
			if err != nil {
				return fmt.Errorf("failed to update task %s: %w", task.TaskID, err)
			}
			assignedTasks = append(assignedTasks, task)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return assignedTasks, nil
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

// ExecTx executes a function within a transaction
// This allows multiple repository operations to be executed atomically
func (r *TaskRepository) ExecTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return r.ds.ExecTx(ctx, fn)
}

// CleanupOldTasks removes completed/failed tasks older than the given time in batches
func (r *TaskRepository) CleanupOldTasks(ctx context.Context, before time.Time) (int64, error) {
	const batchSize = 5000
	var total int64
	for {
		result := r.ds.DB(ctx).Where("status IN (?, ?, ?) AND updated_at < ?", "COMPLETED", "FAILED", "TIMEOUT", before).Limit(batchSize).Delete(&Task{})
		if result.Error != nil {
			return total, result.Error
		}
		total += result.RowsAffected
		if result.RowsAffected < batchSize {
			break
		}
		time.Sleep(100 * time.Millisecond) // avoid overwhelming DB
	}
	return total, nil
}
