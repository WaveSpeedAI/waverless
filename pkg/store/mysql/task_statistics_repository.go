package mysql

import (
	"context"
	"fmt"
	"time"

	"waverless/pkg/store/mysql/model"
)

// TaskStatisticsRepository handles task statistics persistence in MySQL
type TaskStatisticsRepository struct {
	ds *Datastore
}

// NewTaskStatisticsRepository creates a new task statistics repository
func NewTaskStatisticsRepository(ds *Datastore) *TaskStatisticsRepository {
	return &TaskStatisticsRepository{ds: ds}
}

// GetGlobalStatistics retrieves global task statistics
func (r *TaskStatisticsRepository) GetGlobalStatistics(ctx context.Context) (*model.TaskStatistics, error) {
	var stats model.TaskStatistics
	err := r.ds.DB(ctx).
		Where("scope_type = ? AND scope_value = ?", "global", "global").
		First(&stats).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get global statistics: %w", err)
	}
	return &stats, nil
}

// GetEndpointStatistics retrieves statistics for a specific endpoint
func (r *TaskStatisticsRepository) GetEndpointStatistics(ctx context.Context, endpoint string) (*model.TaskStatistics, error) {
	var stats model.TaskStatistics
	err := r.ds.DB(ctx).
		Where("scope_type = ? AND scope_value = ?", "endpoint", endpoint).
		First(&stats).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get endpoint statistics: %w", err)
	}
	return &stats, nil
}

// ListTopEndpoints retrieves top N endpoints by task volume
func (r *TaskStatisticsRepository) ListTopEndpoints(ctx context.Context, limit int) ([]*model.TaskStatistics, error) {
	if limit <= 0 {
		limit = 10
	}

	var stats []*model.TaskStatistics
	err := r.ds.DB(ctx).
		Where("scope_type = ?", "endpoint").
		Order("total_count DESC").
		Limit(limit).
		Find(&stats).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list top endpoints: %w", err)
	}
	return stats, nil
}

// RefreshGlobalStatistics recalculates and updates global statistics from tasks table
func (r *TaskStatisticsRepository) RefreshGlobalStatistics(ctx context.Context) error {
	now := time.Now()

	// Calculate statistics from tasks table
	type TaskStats struct {
		PendingCount    int
		InProgressCount int
		CompletedCount  int
		FailedCount     int
		CancelledCount  int
		TotalCount      int
	}

	var stats TaskStats
	err := r.ds.DB(ctx).Raw(`
		SELECT
			SUM(CASE WHEN status = 'PENDING' THEN 1 ELSE 0 END) as pending_count,
			SUM(CASE WHEN status = 'IN_PROGRESS' THEN 1 ELSE 0 END) as in_progress_count,
			SUM(CASE WHEN status = 'COMPLETED' THEN 1 ELSE 0 END) as completed_count,
			SUM(CASE WHEN status = 'FAILED' THEN 1 ELSE 0 END) as failed_count,
			SUM(CASE WHEN status = 'CANCELLED' THEN 1 ELSE 0 END) as cancelled_count,
			COUNT(*) as total_count
		FROM tasks
	`).Scan(&stats).Error
	if err != nil {
		return fmt.Errorf("failed to calculate global statistics: %w", err)
	}

	// Upsert statistics
	globalValue := "global"
	globalStats := model.TaskStatistics{
		ScopeType:       "global",
		ScopeValue:      &globalValue,
		PendingCount:    stats.PendingCount,
		InProgressCount: stats.InProgressCount,
		CompletedCount:  stats.CompletedCount,
		FailedCount:     stats.FailedCount,
		CancelledCount:  stats.CancelledCount,
		TotalCount:      stats.TotalCount,
		UpdatedAt:       now,
	}

	// Use ON DUPLICATE KEY UPDATE via GORM
	err = r.ds.DB(ctx).Exec(`
		INSERT INTO task_statistics
			(scope_type, scope_value, pending_count, in_progress_count, completed_count, failed_count, cancelled_count, total_count, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			pending_count = VALUES(pending_count),
			in_progress_count = VALUES(in_progress_count),
			completed_count = VALUES(completed_count),
			failed_count = VALUES(failed_count),
			cancelled_count = VALUES(cancelled_count),
			total_count = VALUES(total_count),
			updated_at = VALUES(updated_at)
	`, globalStats.ScopeType, globalStats.ScopeValue, globalStats.PendingCount, globalStats.InProgressCount,
		globalStats.CompletedCount, globalStats.FailedCount, globalStats.CancelledCount,
		globalStats.TotalCount, globalStats.UpdatedAt).Error

	if err != nil {
		return fmt.Errorf("failed to upsert global statistics: %w", err)
	}

	return nil
}

// RefreshEndpointStatistics recalculates and updates per-endpoint statistics
func (r *TaskStatisticsRepository) RefreshEndpointStatistics(ctx context.Context, endpoint string) error {
	now := time.Now()

	// Calculate statistics for specific endpoint
	type TaskStats struct {
		Endpoint        string
		PendingCount    int
		InProgressCount int
		CompletedCount  int
		FailedCount     int
		CancelledCount  int
		TotalCount      int
	}

	var stats TaskStats
	err := r.ds.DB(ctx).Raw(`
		SELECT
			endpoint,
			SUM(CASE WHEN status = 'PENDING' THEN 1 ELSE 0 END) as pending_count,
			SUM(CASE WHEN status = 'IN_PROGRESS' THEN 1 ELSE 0 END) as in_progress_count,
			SUM(CASE WHEN status = 'COMPLETED' THEN 1 ELSE 0 END) as completed_count,
			SUM(CASE WHEN status = 'FAILED' THEN 1 ELSE 0 END) as failed_count,
			SUM(CASE WHEN status = 'CANCELLED' THEN 1 ELSE 0 END) as cancelled_count,
			COUNT(*) as total_count
		FROM tasks
		WHERE endpoint = ?
		GROUP BY endpoint
	`, endpoint).Scan(&stats).Error
	if err != nil {
		return fmt.Errorf("failed to calculate endpoint statistics: %w", err)
	}

	// Upsert statistics
	endpointStats := model.TaskStatistics{
		ScopeType:       "endpoint",
		ScopeValue:      &endpoint,
		PendingCount:    stats.PendingCount,
		InProgressCount: stats.InProgressCount,
		CompletedCount:  stats.CompletedCount,
		FailedCount:     stats.FailedCount,
		CancelledCount:  stats.CancelledCount,
		TotalCount:      stats.TotalCount,
		UpdatedAt:       now,
	}

	err = r.ds.DB(ctx).Exec(`
		INSERT INTO task_statistics
			(scope_type, scope_value, pending_count, in_progress_count, completed_count, failed_count, cancelled_count, total_count, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			pending_count = VALUES(pending_count),
			in_progress_count = VALUES(in_progress_count),
			completed_count = VALUES(completed_count),
			failed_count = VALUES(failed_count),
			cancelled_count = VALUES(cancelled_count),
			total_count = VALUES(total_count),
			updated_at = VALUES(updated_at)
	`, endpointStats.ScopeType, endpointStats.ScopeValue, endpointStats.PendingCount,
		endpointStats.InProgressCount, endpointStats.CompletedCount, endpointStats.FailedCount,
		endpointStats.CancelledCount, endpointStats.TotalCount, endpointStats.UpdatedAt).Error

	if err != nil {
		return fmt.Errorf("failed to upsert endpoint statistics: %w", err)
	}

	return nil
}

// RefreshAllEndpointStatistics recalculates and updates all endpoint statistics
// Uses pagination to avoid locking large tables
func (r *TaskStatisticsRepository) RefreshAllEndpointStatistics(ctx context.Context) error {
	// First, get all distinct endpoints with pagination
	var allEndpoints []string
	batchSize := 100
	offset := 0

	for {
		var endpoints []string
		err := r.ds.DB(ctx).
			Table("tasks").
			Select("DISTINCT endpoint").
			Where("endpoint IS NOT NULL AND endpoint != ''").
			Order("endpoint").
			Limit(batchSize).
			Offset(offset).
			Pluck("endpoint", &endpoints).Error

		if err != nil {
			return fmt.Errorf("failed to fetch endpoints (offset %d): %w", offset, err)
		}

		if len(endpoints) == 0 {
			break
		}

		allEndpoints = append(allEndpoints, endpoints...)

		if len(endpoints) < batchSize {
			break
		}

		offset += batchSize
	}

	// Refresh statistics for each endpoint
	for _, endpoint := range allEndpoints {
		if err := r.RefreshEndpointStatistics(ctx, endpoint); err != nil {
			// Log error but continue processing other endpoints
			return fmt.Errorf("failed to refresh statistics for endpoint %s: %w", endpoint, err)
		}
	}

	return nil
}

// IncrementStatistics atomically increments statistics for a specific status change
// This is called when a task transitions from one status to another
func (r *TaskStatisticsRepository) IncrementStatistics(ctx context.Context, endpoint string, fromStatus, toStatus string) error {
	now := time.Now()

	// Update global statistics
	if err := r.updateStatisticsForStatusChange(ctx, "global", nil, fromStatus, toStatus, now); err != nil {
		return fmt.Errorf("failed to update global statistics: %w", err)
	}

	// Update endpoint statistics (if endpoint is not empty)
	if endpoint != "" {
		if err := r.updateStatisticsForStatusChange(ctx, "endpoint", &endpoint, fromStatus, toStatus, now); err != nil {
			return fmt.Errorf("failed to update endpoint statistics: %w", err)
		}
	}

	return nil
}

// updateStatisticsForStatusChange updates statistics for a status transition
func (r *TaskStatisticsRepository) updateStatisticsForStatusChange(ctx context.Context, scopeType string, scopeValue *string, fromStatus, toStatus string, now time.Time) error {
	// For global scope, always use "global" string instead of NULL
	actualScopeValue := scopeValue
	if scopeValue == nil {
		globalValue := "global"
		actualScopeValue = &globalValue
	}

	// Build SQL with GREATEST() to prevent negative counts
	sql := `
		INSERT INTO task_statistics (scope_type, scope_value, pending_count, in_progress_count, completed_count, failed_count, cancelled_count, total_count, updated_at)
		VALUES (?, ?,
			CASE WHEN ? = 'PENDING' THEN 1 ELSE 0 END,
			CASE WHEN ? = 'IN_PROGRESS' THEN 1 ELSE 0 END,
			CASE WHEN ? = 'COMPLETED' THEN 1 ELSE 0 END,
			CASE WHEN ? = 'FAILED' THEN 1 ELSE 0 END,
			CASE WHEN ? = 'CANCELLED' THEN 1 ELSE 0 END,
			1, ?)
		ON DUPLICATE KEY UPDATE
			pending_count = GREATEST(0, pending_count - CASE WHEN ? = 'PENDING' THEN 1 ELSE 0 END + CASE WHEN ? = 'PENDING' THEN 1 ELSE 0 END),
			in_progress_count = GREATEST(0, in_progress_count - CASE WHEN ? = 'IN_PROGRESS' THEN 1 ELSE 0 END + CASE WHEN ? = 'IN_PROGRESS' THEN 1 ELSE 0 END),
			completed_count = GREATEST(0, completed_count - CASE WHEN ? = 'COMPLETED' THEN 1 ELSE 0 END + CASE WHEN ? = 'COMPLETED' THEN 1 ELSE 0 END),
			failed_count = GREATEST(0, failed_count - CASE WHEN ? = 'FAILED' THEN 1 ELSE 0 END + CASE WHEN ? = 'FAILED' THEN 1 ELSE 0 END),
			cancelled_count = GREATEST(0, cancelled_count - CASE WHEN ? = 'CANCELLED' THEN 1 ELSE 0 END + CASE WHEN ? = 'CANCELLED' THEN 1 ELSE 0 END),
			total_count = GREATEST(0, total_count + CASE WHEN ? = '' THEN 1 ELSE 0 END),
			updated_at = ?
	`
	args := []interface{}{
		scopeType, *actualScopeValue,
		toStatus, toStatus, toStatus, toStatus, toStatus, // INSERT VALUES
		now,
		fromStatus, toStatus, // pending_count
		fromStatus, toStatus, // in_progress_count
		fromStatus, toStatus, // completed_count
		fromStatus, toStatus, // failed_count
		fromStatus, toStatus, // cancelled_count
		fromStatus, // total_count (only increment if fromStatus is empty, meaning new task)
		now,
	}

	return r.ds.DB(ctx).Exec(sql, args...).Error
}
