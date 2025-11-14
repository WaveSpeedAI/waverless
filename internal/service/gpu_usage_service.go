package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"waverless/pkg/interfaces"
	"waverless/pkg/logger"
	"waverless/pkg/store/mysql"
	"waverless/pkg/store/mysql/model"
)

// GPUUsageService provides GPU usage statistics operations
type GPUUsageService struct {
	gpuUsageRepo *mysql.GPUUsageRepository
}

// NewGPUUsageService creates a new GPU usage service
func NewGPUUsageService(gpuUsageRepo *mysql.GPUUsageRepository) *GPUUsageService {
	return &GPUUsageService{
		gpuUsageRepo: gpuUsageRepo,
	}
}

// BackfillResult contains the result of a backfill operation
type BackfillResult struct {
	TotalTasksProcessed int       `json:"totalTasksProcessed"`
	RecordsCreated      int       `json:"recordsCreated"`
	RecordsSkipped      int       `json:"recordsSkipped"`
	Errors              []string  `json:"errors,omitempty"`
	StartTime           time.Time `json:"startTime"`
	EndTime             time.Time `json:"endTime"`
	Duration            string    `json:"duration"`
}

// RecordGPUUsage records GPU usage for a completed task
func (s *GPUUsageService) RecordGPUUsage(ctx context.Context, record *model.GPUUsageRecord) error {
	return s.gpuUsageRepo.RecordGPUUsage(ctx, record)
}

// GetMinuteStatistics retrieves minute-level GPU statistics
func (s *GPUUsageService) GetMinuteStatistics(ctx context.Context, scopeType string, scopeValue *string, startTime, endTime time.Time) ([]*model.GPUUsageStatisticsMinute, error) {
	stats, err := s.gpuUsageRepo.GetMinuteStatistics(ctx, scopeType, scopeValue, startTime, endTime)
	if err != nil {
		return nil, fmt.Errorf("failed to get minute statistics: %w", err)
	}
	return stats, nil
}

// GetHourlyStatistics retrieves hourly GPU statistics
func (s *GPUUsageService) GetHourlyStatistics(ctx context.Context, scopeType string, scopeValue *string, startTime, endTime time.Time) ([]*model.GPUUsageStatisticsHourly, error) {
	stats, err := s.gpuUsageRepo.GetHourlyStatistics(ctx, scopeType, scopeValue, startTime, endTime)
	if err != nil {
		return nil, fmt.Errorf("failed to get hourly statistics: %w", err)
	}
	return stats, nil
}

// GetDailyStatistics retrieves daily GPU statistics
func (s *GPUUsageService) GetDailyStatistics(ctx context.Context, scopeType string, scopeValue *string, startDate, endDate time.Time) ([]*model.GPUUsageStatisticsDaily, error) {
	stats, err := s.gpuUsageRepo.GetDailyStatistics(ctx, scopeType, scopeValue, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("failed to get daily statistics: %w", err)
	}
	return stats, nil
}

// AggregateStatistics aggregates GPU usage for the specified time range
func (s *GPUUsageService) AggregateStatistics(ctx context.Context, startTime, endTime time.Time, granularity string) error {
	switch granularity {
	case "minute":
		return s.aggregateMinuteRange(ctx, startTime, endTime)
	case "hourly", "hour":
		return s.aggregateHourlyRange(ctx, startTime, endTime)
	case "daily", "day":
		return s.aggregateDailyRange(ctx, startTime, endTime)
	case "all":
		// Aggregate all granularities
		if err := s.aggregateMinuteRange(ctx, startTime, endTime); err != nil {
			return err
		}
		if err := s.aggregateHourlyRange(ctx, startTime, endTime); err != nil {
			return err
		}
		return s.aggregateDailyRange(ctx, startTime, endTime)
	default:
		return fmt.Errorf("unsupported granularity: %s", granularity)
	}
}

// aggregateMinuteRange aggregates minute statistics for a time range
func (s *GPUUsageService) aggregateMinuteRange(ctx context.Context, startTime, endTime time.Time) error {
	// Get distinct minutes that have data in the time range
	minutes, err := s.gpuUsageRepo.GetDistinctMinutesWithData(ctx, startTime, endTime)
	if err != nil {
		return fmt.Errorf("failed to get distinct minutes: %w", err)
	}

	logger.InfoCtx(ctx, "Aggregating %d minutes with data (skipping empty minutes)", len(minutes))

	for _, minuteBucket := range minutes {
		if err := s.gpuUsageRepo.AggregateMinuteStatistics(ctx, minuteBucket); err != nil {
			logger.ErrorCtx(ctx, "failed to aggregate minute stats for %v: %v", minuteBucket, err)
			// Continue with other minutes even if one fails
		}
	}
	return nil
}

// aggregateHourlyRange aggregates hourly statistics for a time range
func (s *GPUUsageService) aggregateHourlyRange(ctx context.Context, startTime, endTime time.Time) error {
	// Get distinct hours that have data in the time range
	hours, err := s.gpuUsageRepo.GetDistinctHoursWithData(ctx, startTime, endTime)
	if err != nil {
		return fmt.Errorf("failed to get distinct hours: %w", err)
	}

	logger.InfoCtx(ctx, "Aggregating %d hours with data (skipping empty hours)", len(hours))

	for _, hourBucket := range hours {
		if err := s.gpuUsageRepo.AggregateHourlyStatistics(ctx, hourBucket); err != nil {
			logger.ErrorCtx(ctx, "failed to aggregate hourly stats for %v: %v", hourBucket, err)
		}
	}
	return nil
}

// aggregateDailyRange aggregates daily statistics for a time range
func (s *GPUUsageService) aggregateDailyRange(ctx context.Context, startTime, endTime time.Time) error {
	// Get distinct days that have data in the time range
	days, err := s.gpuUsageRepo.GetDistinctDaysWithData(ctx, startTime, endTime)
	if err != nil {
		return fmt.Errorf("failed to get distinct days: %w", err)
	}

	logger.InfoCtx(ctx, "Aggregating %d days with data (skipping empty days)", len(days))

	for _, dayBucket := range days {
		if err := s.gpuUsageRepo.AggregateDailyStatistics(ctx, dayBucket); err != nil {
			logger.ErrorCtx(ctx, "failed to aggregate daily stats for %v: %v", dayBucket, err)
		}
	}
	return nil
}

// AggregateLastMinute aggregates GPU usage for the last completed minute
func (s *GPUUsageService) AggregateLastMinute(ctx context.Context) error {
	now := time.Now()
	lastMinute := now.Add(-1 * time.Minute).Truncate(time.Minute)
	lastMinuteStr := lastMinute.UTC().Format("2006-01-02 15:04:05")

	logger.DebugCtx(ctx, "aggregating GPU statistics for minute: %s (UTC)", lastMinuteStr)

	if err := s.gpuUsageRepo.AggregateMinuteStatistics(ctx, lastMinuteStr); err != nil {
		return fmt.Errorf("failed to aggregate last minute: %w", err)
	}

	return nil
}

// AggregateLastHour aggregates GPU usage for the last completed hour
func (s *GPUUsageService) AggregateLastHour(ctx context.Context) error {
	now := time.Now()
	lastHour := now.Add(-1 * time.Hour).Truncate(time.Hour)
	lastHourStr := lastHour.UTC().Format("2006-01-02 15:04:05")

	logger.DebugCtx(ctx, "aggregating GPU statistics for hour: %s (UTC)", lastHourStr)

	if err := s.gpuUsageRepo.AggregateHourlyStatistics(ctx, lastHourStr); err != nil {
		return fmt.Errorf("failed to aggregate last hour: %w", err)
	}

	return nil
}

// AggregateLastDay aggregates GPU usage for the last completed day
func (s *GPUUsageService) AggregateLastDay(ctx context.Context) error {
	now := time.Now()
	yesterday := time.Date(now.Year(), now.Month(), now.Day()-1, 0, 0, 0, 0, time.UTC)
	yesterdayStr := yesterday.Format("2006-01-02 15:04:05")

	logger.DebugCtx(ctx, "aggregating GPU statistics for day: %s (UTC)", yesterdayStr)

	if err := s.gpuUsageRepo.AggregateDailyStatistics(ctx, yesterdayStr); err != nil {
		return fmt.Errorf("failed to aggregate last day: %w", err)
	}

	return nil
}

// BackfillHistoricalData backfills GPU usage records for historical tasks
// Parameters:
//   - taskRepo: repository to query historical tasks
//   - endpointRepo: repository to get endpoint spec information
//   - deploymentProvider: provider to get spec details
//   - batchSize: number of tasks to process per batch (0 = all at once)
//   - maxTasks: maximum number of tasks to process (0 = no limit)
func (s *GPUUsageService) BackfillHistoricalData(
	ctx context.Context,
	taskRepo *mysql.TaskRepository,
	endpointRepo *mysql.EndpointRepository,
	deploymentProvider interfaces.DeploymentProvider,
	batchSize int,
	maxTasks int,
) (*BackfillResult, error) {
	startTime := time.Now()
	result := &BackfillResult{
		StartTime: startTime,
		Errors:    make([]string, 0),
	}

	// Default batch size
	if batchSize <= 0 {
		batchSize = 1000
	}

	logger.InfoCtx(ctx, "Starting GPU usage backfill (batch_size=%d, max_tasks=%d)", batchSize, maxTasks)

	offset := 0
	for {
		// Check if we've reached max tasks limit
		if maxTasks > 0 && result.TotalTasksProcessed >= maxTasks {
			logger.InfoCtx(ctx, "Reached max tasks limit: %d", maxTasks)
			break
		}

		// Adjust batch size if needed to not exceed maxTasks
		currentBatchSize := batchSize
		if maxTasks > 0 && result.TotalTasksProcessed+batchSize > maxTasks {
			currentBatchSize = maxTasks - result.TotalTasksProcessed
		}

		// Query tasks without GPU usage records
		tasks, err := s.gpuUsageRepo.GetTasksWithoutGPURecords(ctx, currentBatchSize, offset)
		if err != nil {
			errMsg := fmt.Sprintf("failed to query tasks: %v", err)
			result.Errors = append(result.Errors, errMsg)
			logger.ErrorCtx(ctx, errMsg)
			break
		}

		if len(tasks) == 0 {
			logger.InfoCtx(ctx, "No more tasks to process")
			break
		}

		logger.InfoCtx(ctx, "Processing batch of %d tasks (offset: %d)", len(tasks), offset)

		// Process each task
		for _, task := range tasks {
			result.TotalTasksProcessed++

			// Get endpoint info
			endpoint, err := endpointRepo.Get(ctx, task.Endpoint)
			if err != nil {
				errMsg := fmt.Sprintf("task %s: endpoint %s not found", task.TaskID, task.Endpoint)
				result.Errors = append(result.Errors, errMsg)
				result.RecordsSkipped++
				logger.WarnCtx(ctx, errMsg)
				continue
			}

			// Validate task times
			if task.StartedAt == nil || task.CompletedAt == nil {
				errMsg := fmt.Sprintf("task %s: missing start/completion time", task.TaskID)
				result.Errors = append(result.Errors, errMsg)
				result.RecordsSkipped++
				logger.WarnCtx(ctx, errMsg)
				continue
			}

			// Calculate duration
			duration := task.CompletedAt.Sub(*task.StartedAt)
			durationSeconds := int(duration.Seconds())
			if durationSeconds < 0 {
				durationSeconds = 0
			}

			// Parse GPU info from spec
			specName := endpoint.SpecName
			gpuCount := 1 // Default to 1 GPU
			var gpuType *string
			var gpuMemoryGB *int

			if deploymentProvider != nil {
				specInfo, err := deploymentProvider.GetSpec(ctx, specName)
				if err != nil {
					logger.WarnCtx(ctx, "task %s: failed to get spec %s: %v, using default gpu_count=1",
						task.TaskID, specName, err)
				} else {
					// Parse GPU count
					if specInfo.Resources.GPU != "" {
						if count, err := strconv.Atoi(strings.TrimSpace(specInfo.Resources.GPU)); err == nil && count > 0 {
							gpuCount = count
						}
					}

					// Parse GPU type
					if specInfo.Resources.GPUType != "" {
						gpuTypeStr := strings.TrimSpace(specInfo.Resources.GPUType)
						gpuType = &gpuTypeStr
					}

					// Try to parse GPU memory from GPUType
					if gpuType != nil {
						parts := strings.Split(*gpuType, "-")
						for _, part := range parts {
							if strings.HasSuffix(strings.ToUpper(part), "GB") {
								memStr := strings.TrimSuffix(strings.ToUpper(part), "GB")
								if mem, err := strconv.Atoi(memStr); err == nil && mem > 0 {
									gpuMemoryGB = &mem
									break
								}
							}
						}
					}
				}
			}

			// Calculate GPU hours
			gpuHours := float64(gpuCount) * (float64(durationSeconds) / 3600.0)

			// Create GPU usage record
			record := &model.GPUUsageRecord{
				TaskID:          task.TaskID,
				Endpoint:        task.Endpoint,
				WorkerID:        &task.WorkerID,
				SpecName:        &specName,
				GPUCount:        gpuCount,
				GPUType:         gpuType,
				GPUMemoryGB:     gpuMemoryGB,
				StartedAt:       *task.StartedAt,
				CompletedAt:     *task.CompletedAt,
				DurationSeconds: durationSeconds,
				GPUHours:        gpuHours,
				Status:          task.Status,
				CreatedAt:       time.Now(),
			}

			if err := s.gpuUsageRepo.RecordGPUUsage(ctx, record); err != nil {
				errMsg := fmt.Sprintf("task %s: failed to create GPU usage record: %v", task.TaskID, err)
				result.Errors = append(result.Errors, errMsg)
				result.RecordsSkipped++
				logger.ErrorCtx(ctx, errMsg)
				continue
			}

			result.RecordsCreated++
			if result.RecordsCreated%100 == 0 {
				logger.InfoCtx(ctx, "Progress: created %d GPU usage records", result.RecordsCreated)
			}
		}

		offset += batchSize

		// Small delay to avoid overwhelming the database
		time.Sleep(50 * time.Millisecond)
	}

	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime).String()

	logger.InfoCtx(ctx, "Backfill completed: processed=%d, created=%d, skipped=%d, errors=%d, duration=%s",
		result.TotalTasksProcessed, result.RecordsCreated, result.RecordsSkipped, len(result.Errors), result.Duration)

	return result, nil
}

// CleanupOldStatistics removes old GPU usage data according to retention policies
// Retention policy:
//   - gpu_usage_records: 90 days
//   - gpu_usage_statistics_minute: 3 days
//   - gpu_usage_statistics_hourly: 30 days
//   - gpu_usage_statistics_daily: kept indefinitely (not cleaned)
func (s *GPUUsageService) CleanupOldStatistics(ctx context.Context) error {
	now := time.Now()

	// Cleanup minute-level statistics (older than 3 days)
	minuteCutoff := now.AddDate(0, 0, -3)
	deletedMinutes, err := s.gpuUsageRepo.DeleteOldMinuteStatistics(ctx, minuteCutoff)
	if err != nil {
		logger.ErrorCtx(ctx, "Failed to cleanup old minute statistics: %v", err)
		return fmt.Errorf("failed to cleanup minute statistics: %w", err)
	}
	if deletedMinutes > 0 {
		logger.InfoCtx(ctx, "Cleaned up %d minute-level statistics records (older than %s)",
			deletedMinutes, minuteCutoff.Format("2006-01-02"))
	}

	// Cleanup hourly statistics (older than 30 days)
	hourlyCutoff := now.AddDate(0, 0, -30)
	deletedHourly, err := s.gpuUsageRepo.DeleteOldHourlyStatistics(ctx, hourlyCutoff)
	if err != nil {
		logger.ErrorCtx(ctx, "Failed to cleanup old hourly statistics: %v", err)
		return fmt.Errorf("failed to cleanup hourly statistics: %w", err)
	}
	if deletedHourly > 0 {
		logger.InfoCtx(ctx, "Cleaned up %d hourly statistics records (older than %s)",
			deletedHourly, hourlyCutoff.Format("2006-01-02"))
	}

	// Cleanup GPU usage records (older than 90 days)
	recordsCutoff := now.AddDate(0, 0, -90)
	deletedRecords, err := s.gpuUsageRepo.DeleteOldRecords(ctx, recordsCutoff)
	if err != nil {
		logger.ErrorCtx(ctx, "Failed to cleanup old GPU usage records: %v", err)
		return fmt.Errorf("failed to cleanup GPU usage records: %w", err)
	}
	if deletedRecords > 0 {
		logger.InfoCtx(ctx, "Cleaned up %d GPU usage records (older than %s)",
			deletedRecords, recordsCutoff.Format("2006-01-02"))
	}

	// Daily statistics are kept indefinitely, no cleanup

	logger.InfoCtx(ctx, "GPU statistics cleanup completed: minutes=%d, hourly=%d, records=%d",
		deletedMinutes, deletedHourly, deletedRecords)

	return nil
}
