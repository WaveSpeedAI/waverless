package mysql

import (
	"context"
	"fmt"
	"time"

	"waverless/pkg/store/mysql/model"
)

// GPUUsageRepository handles GPU usage persistence in MySQL
type GPUUsageRepository struct {
	ds *Datastore
}

// NewGPUUsageRepository creates a new GPU usage repository
func NewGPUUsageRepository(ds *Datastore) *GPUUsageRepository {
	return &GPUUsageRepository{ds: ds}
}

// RecordGPUUsage creates a new GPU usage record when a task completes
func (r *GPUUsageRepository) RecordGPUUsage(ctx context.Context, record *model.GPUUsageRecord) error {
	return r.ds.DB(ctx).Create(record).Error
}

// GetGPUUsageByTaskID retrieves GPU usage record by task ID
func (r *GPUUsageRepository) GetGPUUsageByTaskID(ctx context.Context, taskID string) (*model.GPUUsageRecord, error) {
	var record model.GPUUsageRecord
	err := r.ds.DB(ctx).Where("task_id = ?", taskID).First(&record).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get GPU usage record: %w", err)
	}
	return &record, nil
}

// ListGPUUsageRecords lists GPU usage records with filters
func (r *GPUUsageRepository) ListGPUUsageRecords(ctx context.Context, filters map[string]interface{}, limit, offset int) ([]*model.GPUUsageRecord, int64, error) {
	var records []*model.GPUUsageRecord
	var total int64

	query := r.ds.DB(ctx).Model(&model.GPUUsageRecord{})

	// Apply filters
	if endpoint, ok := filters["endpoint"].(string); ok && endpoint != "" {
		query = query.Where("endpoint = ?", endpoint)
	}
	if startTime, ok := filters["start_time"].(time.Time); ok {
		query = query.Where("started_at >= ?", startTime)
	}
	if endTime, ok := filters["end_time"].(time.Time); ok {
		query = query.Where("completed_at <= ?", endTime)
	}

	// Get total count
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to count GPU usage records: %w", err)
	}

	// Get records with pagination
	if err := query.Order("completed_at DESC").Limit(limit).Offset(offset).Find(&records).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to list GPU usage records: %w", err)
	}

	return records, total, nil
}

// GetHourlyStatistics retrieves hourly GPU usage statistics
func (r *GPUUsageRepository) GetHourlyStatistics(ctx context.Context, scopeType string, scopeValue *string, startTime, endTime time.Time) ([]*model.GPUUsageStatisticsHourly, error) {
	var stats []*model.GPUUsageStatisticsHourly

	query := r.ds.DB(ctx).Where("scope_type = ?", scopeType)
	if scopeValue != nil {
		query = query.Where("scope_value = ?", *scopeValue)
	} else {
		query = query.Where("scope_value IS NULL")
	}
	query = query.Where("time_bucket >= ? AND time_bucket < ?", startTime, endTime)

	if err := query.Order("time_bucket ASC").Find(&stats).Error; err != nil {
		return nil, fmt.Errorf("failed to get hourly statistics: %w", err)
	}

	return stats, nil
}

// GetDailyStatistics retrieves daily GPU usage statistics
func (r *GPUUsageRepository) GetDailyStatistics(ctx context.Context, scopeType string, scopeValue *string, startDate, endDate time.Time) ([]*model.GPUUsageStatisticsDaily, error) {
	var stats []*model.GPUUsageStatisticsDaily

	query := r.ds.DB(ctx).Where("scope_type = ?", scopeType)
	if scopeValue != nil {
		query = query.Where("scope_value = ?", *scopeValue)
	} else {
		query = query.Where("scope_value IS NULL")
	}
	query = query.Where("time_bucket >= ? AND time_bucket < ?", startDate, endDate)

	if err := query.Order("time_bucket ASC").Find(&stats).Error; err != nil {
		return nil, fmt.Errorf("failed to get daily statistics: %w", err)
	}

	return stats, nil
}

// GetMinuteStatistics retrieves minute-level GPU usage statistics
func (r *GPUUsageRepository) GetMinuteStatistics(ctx context.Context, scopeType string, scopeValue *string, startTime, endTime time.Time) ([]*model.GPUUsageStatisticsMinute, error) {
	var stats []*model.GPUUsageStatisticsMinute

	query := r.ds.DB(ctx).Where("scope_type = ?", scopeType)
	if scopeValue != nil {
		query = query.Where("scope_value = ?", *scopeValue)
	} else {
		query = query.Where("scope_value IS NULL")
	}
	query = query.Where("time_bucket >= ? AND time_bucket < ?", startTime, endTime)

	if err := query.Order("time_bucket ASC").Find(&stats).Error; err != nil {
		return nil, fmt.Errorf("failed to get minute statistics: %w", err)
	}

	return stats, nil
}

// AggregateMinuteStatistics aggregates GPU usage records into minute statistics
// minuteBucketStr should be in format "2006-01-02 15:04:00" (without timezone)
func (r *GPUUsageRepository) AggregateMinuteStatistics(ctx context.Context, minuteBucketStr string) error {
	// Parse the minute bucket to calculate period end
	minuteBucket, err := time.Parse("2006-01-02 15:04:05", minuteBucketStr)
	if err != nil {
		return fmt.Errorf("invalid minute bucket format %s: %w", minuteBucketStr, err)
	}

	periodStartStr := minuteBucketStr
	periodEndStr := minuteBucket.Add(1 * time.Minute).Format("2006-01-02 15:04:05")
	nowStr := time.Now().Format("2006-01-02 15:04:05")

	// Aggregate global statistics
	result := r.ds.DB(ctx).Exec(`
		INSERT INTO gpu_usage_statistics_minute
		    (time_bucket, scope_type, scope_value, total_tasks, completed_tasks, failed_tasks,
		     total_gpu_seconds, total_gpu_hours, avg_gpu_count, max_gpu_count,
		     period_start, period_end, updated_at)
		SELECT
		    ? as time_bucket,
		    'global' as scope_type,
		    NULL as scope_value,
		    COUNT(*) as total_tasks,
		    SUM(CASE WHEN status = 'COMPLETED' THEN 1 ELSE 0 END) as completed_tasks,
		    SUM(CASE WHEN status = 'FAILED' THEN 1 ELSE 0 END) as failed_tasks,
		    SUM(duration_seconds * gpu_count) as total_gpu_seconds,
		    SUM(gpu_hours) as total_gpu_hours,
		    AVG(gpu_count) as avg_gpu_count,
		    MAX(gpu_count) as max_gpu_count,
		    ? as period_start,
		    ? as period_end,
		    ? as updated_at
		FROM gpu_usage_records
		WHERE completed_at >= ? AND completed_at < ?
		ON DUPLICATE KEY UPDATE
		    total_tasks = VALUES(total_tasks),
		    completed_tasks = VALUES(completed_tasks),
		    failed_tasks = VALUES(failed_tasks),
		    total_gpu_seconds = VALUES(total_gpu_seconds),
		    total_gpu_hours = VALUES(total_gpu_hours),
		    avg_gpu_count = VALUES(avg_gpu_count),
		    max_gpu_count = VALUES(max_gpu_count),
		    updated_at = VALUES(updated_at)
	`, minuteBucketStr, periodStartStr, periodEndStr, nowStr, periodStartStr, periodEndStr)

	if result.Error != nil {
		return fmt.Errorf("failed to aggregate global minute statistics: %w", result.Error)
	}

	// Aggregate per-endpoint statistics
	result = r.ds.DB(ctx).Exec(`
		INSERT INTO gpu_usage_statistics_minute
		    (time_bucket, scope_type, scope_value, total_tasks, completed_tasks, failed_tasks,
		     total_gpu_seconds, total_gpu_hours, avg_gpu_count, max_gpu_count,
		     period_start, period_end, updated_at)
		SELECT
		    ? as time_bucket,
		    'endpoint' as scope_type,
		    endpoint as scope_value,
		    COUNT(*) as total_tasks,
		    SUM(CASE WHEN status = 'COMPLETED' THEN 1 ELSE 0 END) as completed_tasks,
		    SUM(CASE WHEN status = 'FAILED' THEN 1 ELSE 0 END) as failed_tasks,
		    SUM(duration_seconds * gpu_count) as total_gpu_seconds,
		    SUM(gpu_hours) as total_gpu_hours,
		    AVG(gpu_count) as avg_gpu_count,
		    MAX(gpu_count) as max_gpu_count,
		    ? as period_start,
		    ? as period_end,
		    ? as updated_at
		FROM gpu_usage_records
		WHERE completed_at >= ? AND completed_at < ?
		GROUP BY endpoint
		ON DUPLICATE KEY UPDATE
		    total_tasks = VALUES(total_tasks),
		    completed_tasks = VALUES(completed_tasks),
		    failed_tasks = VALUES(failed_tasks),
		    total_gpu_seconds = VALUES(total_gpu_seconds),
		    total_gpu_hours = VALUES(total_gpu_hours),
		    avg_gpu_count = VALUES(avg_gpu_count),
		    max_gpu_count = VALUES(max_gpu_count),
		    updated_at = VALUES(updated_at)
	`, minuteBucketStr, periodStartStr, periodEndStr, nowStr, periodStartStr, periodEndStr)

	if result.Error != nil {
		return fmt.Errorf("failed to aggregate endpoint minute statistics: %w", result.Error)
	}

	// Aggregate per-spec statistics
	result = r.ds.DB(ctx).Exec(`
		INSERT INTO gpu_usage_statistics_minute
		    (time_bucket, scope_type, scope_value, total_tasks, completed_tasks, failed_tasks,
		     total_gpu_seconds, total_gpu_hours, avg_gpu_count, max_gpu_count,
		     period_start, period_end, updated_at)
		SELECT
		    ? as time_bucket,
		    'spec' as scope_type,
		    spec_name as scope_value,
		    COUNT(*) as total_tasks,
		    SUM(CASE WHEN status = 'COMPLETED' THEN 1 ELSE 0 END) as completed_tasks,
		    SUM(CASE WHEN status = 'FAILED' THEN 1 ELSE 0 END) as failed_tasks,
		    SUM(duration_seconds * gpu_count) as total_gpu_seconds,
		    SUM(gpu_hours) as total_gpu_hours,
		    AVG(gpu_count) as avg_gpu_count,
		    MAX(gpu_count) as max_gpu_count,
		    ? as period_start,
		    ? as period_end,
		    ? as updated_at
		FROM gpu_usage_records
		WHERE completed_at >= ? AND completed_at < ?
		    AND spec_name IS NOT NULL
		GROUP BY spec_name
		ON DUPLICATE KEY UPDATE
		    total_tasks = VALUES(total_tasks),
		    completed_tasks = VALUES(completed_tasks),
		    failed_tasks = VALUES(failed_tasks),
		    total_gpu_seconds = VALUES(total_gpu_seconds),
		    total_gpu_hours = VALUES(total_gpu_hours),
		    avg_gpu_count = VALUES(avg_gpu_count),
		    max_gpu_count = VALUES(max_gpu_count),
		    updated_at = VALUES(updated_at)
	`, minuteBucketStr, periodStartStr, periodEndStr, nowStr, periodStartStr, periodEndStr)

	if result.Error != nil {
		return fmt.Errorf("failed to aggregate spec minute statistics: %w", result.Error)
	}

	return nil
}

// AggregateHourlyStatistics aggregates GPU usage records into hourly statistics
// hourBucketStr should be in format "2006-01-02 15:00:00" (UTC, without timezone)
func (r *GPUUsageRepository) AggregateHourlyStatistics(ctx context.Context, hourBucketStr string) error {
	// Parse the hour bucket to calculate period end
	hourBucket, err := time.Parse("2006-01-02 15:04:05", hourBucketStr)
	if err != nil {
		return fmt.Errorf("invalid hour bucket format %s: %w", hourBucketStr, err)
	}

	periodStartStr := hourBucketStr
	periodEndStr := hourBucket.Add(1 * time.Hour).Format("2006-01-02 15:04:05")
	nowStr := time.Now().Format("2006-01-02 15:04:05")

	// Aggregate global statistics
	err = r.ds.DB(ctx).Exec(`
		INSERT INTO gpu_usage_statistics_hourly
		    (time_bucket, scope_type, scope_value, total_tasks, completed_tasks, failed_tasks,
		     total_gpu_hours, avg_gpu_count, max_gpu_count, period_start, period_end, updated_at)
		SELECT
		    ? as time_bucket,
		    'global' as scope_type,
		    NULL as scope_value,
		    COUNT(*) as total_tasks,
		    SUM(CASE WHEN status = 'COMPLETED' THEN 1 ELSE 0 END) as completed_tasks,
		    SUM(CASE WHEN status = 'FAILED' THEN 1 ELSE 0 END) as failed_tasks,
		    SUM(gpu_hours) as total_gpu_hours,
		    AVG(gpu_count) as avg_gpu_count,
		    MAX(gpu_count) as max_gpu_count,
		    ? as period_start,
		    ? as period_end,
		    ? as updated_at
		FROM gpu_usage_records
		WHERE completed_at >= ? AND completed_at < ?
		ON DUPLICATE KEY UPDATE
		    total_tasks = VALUES(total_tasks),
		    completed_tasks = VALUES(completed_tasks),
		    failed_tasks = VALUES(failed_tasks),
		    total_gpu_hours = VALUES(total_gpu_hours),
		    avg_gpu_count = VALUES(avg_gpu_count),
		    max_gpu_count = VALUES(max_gpu_count),
		    updated_at = VALUES(updated_at)
	`, hourBucketStr, periodStartStr, periodEndStr, nowStr, periodStartStr, periodEndStr).Error

	if err != nil {
		return fmt.Errorf("failed to aggregate global hourly statistics: %w", err)
	}

	// Aggregate per-endpoint statistics
	err = r.ds.DB(ctx).Exec(`
		INSERT INTO gpu_usage_statistics_hourly
		    (time_bucket, scope_type, scope_value, total_tasks, completed_tasks, failed_tasks,
		     total_gpu_hours, avg_gpu_count, max_gpu_count, period_start, period_end, updated_at)
		SELECT
		    ? as time_bucket,
		    'endpoint' as scope_type,
		    endpoint as scope_value,
		    COUNT(*) as total_tasks,
		    SUM(CASE WHEN status = 'COMPLETED' THEN 1 ELSE 0 END) as completed_tasks,
		    SUM(CASE WHEN status = 'FAILED' THEN 1 ELSE 0 END) as failed_tasks,
		    SUM(gpu_hours) as total_gpu_hours,
		    AVG(gpu_count) as avg_gpu_count,
		    MAX(gpu_count) as max_gpu_count,
		    ? as period_start,
		    ? as period_end,
		    ? as updated_at
		FROM gpu_usage_records
		WHERE completed_at >= ? AND completed_at < ?
		GROUP BY endpoint
		ON DUPLICATE KEY UPDATE
		    total_tasks = VALUES(total_tasks),
		    completed_tasks = VALUES(completed_tasks),
		    failed_tasks = VALUES(failed_tasks),
		    total_gpu_hours = VALUES(total_gpu_hours),
		    avg_gpu_count = VALUES(avg_gpu_count),
		    max_gpu_count = VALUES(max_gpu_count),
		    updated_at = VALUES(updated_at)
	`, hourBucketStr, periodStartStr, periodEndStr, nowStr, periodStartStr, periodEndStr).Error

	if err != nil {
		return fmt.Errorf("failed to aggregate endpoint hourly statistics: %w", err)
	}

	return nil
}

// AggregateDailyStatistics aggregates hourly statistics into daily statistics
// dayBucketStr should be in format "2006-01-02 00:00:00" (UTC, without timezone)
func (r *GPUUsageRepository) AggregateDailyStatistics(ctx context.Context, dayBucketStr string) error {
	// Parse the day bucket to calculate period end
	dayBucket, err := time.Parse("2006-01-02 15:04:05", dayBucketStr)
	if err != nil {
		return fmt.Errorf("invalid day bucket format %s: %w", dayBucketStr, err)
	}

	periodStartStr := dayBucketStr
	periodEndStr := dayBucket.Add(24 * time.Hour).Format("2006-01-02 15:04:05")
	nowStr := time.Now().Format("2006-01-02 15:04:05")
	dayOnlyStr := dayBucket.Format("2006-01-02") // DATE format

	// Aggregate global daily statistics from hourly stats
	err = r.ds.DB(ctx).Exec(`
		INSERT INTO gpu_usage_statistics_daily
		    (time_bucket, scope_type, scope_value, total_tasks, completed_tasks, failed_tasks,
		     total_gpu_hours, avg_gpu_count, max_gpu_count, peak_hour, peak_gpu_hours,
		     period_start, period_end, updated_at)
		SELECT
		    ? as time_bucket,
		    'global' as scope_type,
		    NULL as scope_value,
		    SUM(total_tasks) as total_tasks,
		    SUM(completed_tasks) as completed_tasks,
		    SUM(failed_tasks) as failed_tasks,
		    SUM(total_gpu_hours) as total_gpu_hours,
		    AVG(avg_gpu_count) as avg_gpu_count,
		    MAX(max_gpu_count) as max_gpu_count,
		    (SELECT time_bucket FROM gpu_usage_statistics_hourly
		     WHERE scope_type = 'global' AND scope_value IS NULL
		     AND time_bucket >= ? AND time_bucket < ?
		     ORDER BY total_gpu_hours DESC LIMIT 1) as peak_hour,
		    MAX(total_gpu_hours) as peak_gpu_hours,
		    ? as period_start,
		    ? as period_end,
		    ? as updated_at
		FROM gpu_usage_statistics_hourly
		WHERE scope_type = 'global' AND scope_value IS NULL
		    AND time_bucket >= ? AND time_bucket < ?
		ON DUPLICATE KEY UPDATE
		    total_tasks = VALUES(total_tasks),
		    completed_tasks = VALUES(completed_tasks),
		    failed_tasks = VALUES(failed_tasks),
		    total_gpu_hours = VALUES(total_gpu_hours),
		    avg_gpu_count = VALUES(avg_gpu_count),
		    max_gpu_count = VALUES(max_gpu_count),
		    peak_hour = VALUES(peak_hour),
		    peak_gpu_hours = VALUES(peak_gpu_hours),
		    updated_at = VALUES(updated_at)
	`, dayOnlyStr, periodStartStr, periodEndStr, periodStartStr, periodEndStr, nowStr, periodStartStr, periodEndStr).Error

	if err != nil {
		return fmt.Errorf("failed to aggregate global daily statistics: %w", err)
	}

	// Aggregate per-endpoint daily statistics
	err = r.ds.DB(ctx).Exec(`
		INSERT INTO gpu_usage_statistics_daily
		    (time_bucket, scope_type, scope_value, total_tasks, completed_tasks, failed_tasks,
		     total_gpu_hours, avg_gpu_count, max_gpu_count, period_start, period_end, updated_at)
		SELECT
		    ? as time_bucket,
		    'endpoint' as scope_type,
		    scope_value,
		    SUM(total_tasks) as total_tasks,
		    SUM(completed_tasks) as completed_tasks,
		    SUM(failed_tasks) as failed_tasks,
		    SUM(total_gpu_hours) as total_gpu_hours,
		    AVG(avg_gpu_count) as avg_gpu_count,
		    MAX(max_gpu_count) as max_gpu_count,
		    ? as period_start,
		    ? as period_end,
		    ? as updated_at
		FROM gpu_usage_statistics_hourly
		WHERE scope_type = 'endpoint' AND scope_value IS NOT NULL
		    AND time_bucket >= ? AND time_bucket < ?
		GROUP BY scope_value
		ON DUPLICATE KEY UPDATE
		    total_tasks = VALUES(total_tasks),
		    completed_tasks = VALUES(completed_tasks),
		    failed_tasks = VALUES(failed_tasks),
		    total_gpu_hours = VALUES(total_gpu_hours),
		    avg_gpu_count = VALUES(avg_gpu_count),
		    max_gpu_count = VALUES(max_gpu_count),
		    updated_at = VALUES(updated_at)
	`, dayOnlyStr, periodStartStr, periodEndStr, nowStr, periodStartStr, periodEndStr).Error

	if err != nil {
		return fmt.Errorf("failed to aggregate endpoint daily statistics: %w", err)
	}

	return nil
}

// GetTasksWithoutGPURecords retrieves tasks that don't have GPU usage records yet
// Used for backfilling historical data
func (r *GPUUsageRepository) GetTasksWithoutGPURecords(ctx context.Context, limit, offset int) ([]*model.Task, error) {
	var tasks []*model.Task

	// Query completed/failed tasks that don't have GPU usage records
	// Use LEFT JOIN instead of NOT IN to avoid collation conflicts
	err := r.ds.DB(ctx).
		Table("tasks").
		Select("tasks.*").
		Joins("LEFT JOIN gpu_usage_records ON tasks.task_id = gpu_usage_records.task_id").
		Where("(tasks.status = ? OR tasks.status = ?)", "COMPLETED", "FAILED").
		Where("tasks.started_at IS NOT NULL").
		Where("tasks.completed_at IS NOT NULL").
		Where("gpu_usage_records.task_id IS NULL").
		Order("tasks.completed_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&tasks).Error

	if err != nil {
		return nil, fmt.Errorf("failed to query tasks without GPU records: %w", err)
	}

	return tasks, nil
}

// GetDistinctMinutesWithData returns distinct minute buckets that have GPU usage records in the time range
// Returns time strings in UTC format (e.g., "2025-11-12 06:04:00")
func (r *GPUUsageRepository) GetDistinctMinutesWithData(ctx context.Context, startTime, endTime time.Time) ([]string, error) {
	var results []struct {
		MinuteBucket string
	}

	// Convert to UTC and format as strings to ensure consistent timezone handling
	// Database stores UTC times, so we query with UTC strings
	startStr := startTime.UTC().Format("2006-01-02 15:04:05")
	endStr := endTime.UTC().Format("2006-01-02 15:04:05")

	err := r.ds.DB(ctx).Raw(`
		SELECT DISTINCT DATE_FORMAT(completed_at, '%Y-%m-%d %H:%i:00') as minute_bucket
		FROM gpu_usage_records
		WHERE completed_at >= ? AND completed_at < ?
		ORDER BY minute_bucket
	`, startStr, endStr).Scan(&results).Error

	if err != nil {
		return nil, fmt.Errorf("failed to get distinct minutes: %w", err)
	}

	minutes := make([]string, 0, len(results))
	for _, r := range results {
		minutes = append(minutes, r.MinuteBucket)
	}
	return minutes, nil
}

// GetDistinctHoursWithData returns distinct hour buckets that have GPU usage records in the time range
// Returns time strings in UTC format (e.g., "2025-11-12 06:00:00")
func (r *GPUUsageRepository) GetDistinctHoursWithData(ctx context.Context, startTime, endTime time.Time) ([]string, error) {
	var results []struct {
		HourBucket string
	}

	// Convert to UTC and format as strings
	startStr := startTime.UTC().Format("2006-01-02 15:04:05")
	endStr := endTime.UTC().Format("2006-01-02 15:04:05")

	err := r.ds.DB(ctx).Raw(`
		SELECT DISTINCT DATE_FORMAT(completed_at, '%Y-%m-%d %H:00:00') as hour_bucket
		FROM gpu_usage_records
		WHERE completed_at >= ? AND completed_at < ?
		ORDER BY hour_bucket
	`, startStr, endStr).Scan(&results).Error

	if err != nil {
		return nil, fmt.Errorf("failed to get distinct hours: %w", err)
	}

	hours := make([]string, 0, len(results))
	for _, r := range results {
		hours = append(hours, r.HourBucket)
	}
	return hours, nil
}

// GetDistinctDaysWithData returns distinct day buckets that have GPU usage records in the time range
// Returns time strings in UTC format (e.g., "2025-11-12 00:00:00")
func (r *GPUUsageRepository) GetDistinctDaysWithData(ctx context.Context, startTime, endTime time.Time) ([]string, error) {
	var results []struct {
		DayBucket string
	}

	// Convert to UTC and format as strings
	startStr := startTime.UTC().Format("2006-01-02 15:04:05")
	endStr := endTime.UTC().Format("2006-01-02 15:04:05")

	err := r.ds.DB(ctx).Raw(`
		SELECT DISTINCT DATE_FORMAT(DATE(completed_at), '%Y-%m-%d 00:00:00') as day_bucket
		FROM gpu_usage_records
		WHERE completed_at >= ? AND completed_at < ?
		ORDER BY day_bucket
	`, startStr, endStr).Scan(&results).Error

	if err != nil {
		return nil, fmt.Errorf("failed to get distinct days: %w", err)
	}

	days := make([]string, 0, len(results))
	for _, r := range results {
		days = append(days, r.DayBucket)
	}
	return days, nil
}

// DeleteOldRecords deletes GPU usage records older than the specified time
func (r *GPUUsageRepository) DeleteOldRecords(ctx context.Context, beforeTime time.Time) (int64, error) {
	result := r.ds.DB(ctx).
		Where("completed_at < ?", beforeTime).
		Delete(&model.GPUUsageRecord{})

	if result.Error != nil {
		return 0, fmt.Errorf("failed to delete old GPU usage records: %w", result.Error)
	}

	return result.RowsAffected, nil
}

// DeleteOldMinuteStatistics deletes minute-level statistics older than the specified time
func (r *GPUUsageRepository) DeleteOldMinuteStatistics(ctx context.Context, beforeTime time.Time) (int64, error) {
	result := r.ds.DB(ctx).
		Where("time_bucket < ?", beforeTime).
		Delete(&model.GPUUsageStatisticsMinute{})

	if result.Error != nil {
		return 0, fmt.Errorf("failed to delete old minute statistics: %w", result.Error)
	}

	return result.RowsAffected, nil
}

// DeleteOldHourlyStatistics deletes hourly statistics older than the specified time
func (r *GPUUsageRepository) DeleteOldHourlyStatistics(ctx context.Context, beforeTime time.Time) (int64, error) {
	result := r.ds.DB(ctx).
		Where("time_bucket < ?", beforeTime).
		Delete(&model.GPUUsageStatisticsHourly{})

	if result.Error != nil {
		return 0, fmt.Errorf("failed to delete old hourly statistics: %w", result.Error)
	}

	return result.RowsAffected, nil
}
