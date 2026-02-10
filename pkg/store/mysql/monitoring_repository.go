package mysql

import (
	"context"
	"strconv"
	"time"

	"waverless/pkg/store/mysql/model"

	"gorm.io/gorm/clause"
)

// Helper functions for type conversion from map
func toInt(v interface{}) int {
	if v == nil {
		return 0
	}
	switch n := v.(type) {
	case int64:
		return int(n)
	case float64:
		return int(n)
	case int:
		return n
	case []byte:
		if f, err := strconv.ParseFloat(string(n), 64); err == nil {
			return int(f)
		}
		return 0
	case string:
		if f, err := strconv.ParseFloat(n, 64); err == nil {
			return int(f)
		}
		return 0
	default:
		return 0
	}
}

func toInt64(v interface{}) int64 {
	if v == nil {
		return 0
	}
	switch n := v.(type) {
	case int64:
		return n
	case float64:
		return int64(n)
	case int:
		return int64(n)
	case []byte:
		if f, err := strconv.ParseFloat(string(n), 64); err == nil {
			return int64(f)
		}
		return 0
	case string:
		if f, err := strconv.ParseFloat(n, 64); err == nil {
			return int64(f)
		}
		return 0
	default:
		return 0
	}
}

func toFloat(v interface{}) float64 {
	if v == nil {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return n
	case int64:
		return float64(n)
	case int:
		return float64(n)
	case []byte:
		if f, err := strconv.ParseFloat(string(n), 64); err == nil {
			return f
		}
		return 0
	case string:
		if f, err := strconv.ParseFloat(n, 64); err == nil {
			return f
		}
		return 0
	default:
		return 0
	}
}

// MonitoringRepository handles monitoring statistics database operations
type MonitoringRepository struct {
	ds *Datastore
}

// NewMonitoringRepository creates a new monitoring repository
func NewMonitoringRepository(ds *Datastore) *MonitoringRepository {
	return &MonitoringRepository{ds: ds}
}

// UpsertMinuteStat creates or updates minute-level statistics
func (r *MonitoringRepository) UpsertMinuteStat(ctx context.Context, stat *model.EndpointMinuteStat) error {
	return r.ds.DB(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "endpoint"}, {Name: "stat_minute"}},
		UpdateAll: true,
	}).Create(stat).Error
}

// GetMinuteStats retrieves minute-level statistics for a time range
func (r *MonitoringRepository) GetMinuteStats(ctx context.Context, endpoint string, from, to time.Time) ([]*model.EndpointMinuteStat, error) {
	var stats []*model.EndpointMinuteStat
	err := r.ds.DB(ctx).Where("endpoint = ? AND stat_minute >= ? AND stat_minute < ?", endpoint, from, to).
		Order("stat_minute ASC").Find(&stats).Error
	return stats, err
}

// UpsertHourlyStat creates or updates hourly statistics
func (r *MonitoringRepository) UpsertHourlyStat(ctx context.Context, stat *model.EndpointHourlyStat) error {
	return r.ds.DB(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "endpoint"}, {Name: "stat_hour"}},
		UpdateAll: true,
	}).Create(stat).Error
}

// GetHourlyStats retrieves hourly statistics for a time range
func (r *MonitoringRepository) GetHourlyStats(ctx context.Context, endpoint string, from, to time.Time) ([]*model.EndpointHourlyStat, error) {
	var stats []*model.EndpointHourlyStat
	err := r.ds.DB(ctx).Where("endpoint = ? AND stat_hour >= ? AND stat_hour < ?", endpoint, from, to).
		Order("stat_hour ASC").Find(&stats).Error
	return stats, err
}

// UpsertDailyStat creates or updates daily statistics
func (r *MonitoringRepository) UpsertDailyStat(ctx context.Context, stat *model.EndpointDailyStat) error {
	return r.ds.DB(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "endpoint"}, {Name: "stat_date"}},
		UpdateAll: true,
	}).Create(stat).Error
}

// GetDailyStats retrieves daily statistics for a date range
func (r *MonitoringRepository) GetDailyStats(ctx context.Context, endpoint string, from, to time.Time) ([]*model.EndpointDailyStat, error) {
	var stats []*model.EndpointDailyStat
	err := r.ds.DB(ctx).Where("endpoint = ? AND stat_date >= ? AND stat_date < ?", endpoint, from, to).
		Order("stat_date ASC").Find(&stats).Error
	return stats, err
}

// CleanupOldMinuteStats removes minute stats older than retention period
func (r *MonitoringRepository) CleanupOldMinuteStats(ctx context.Context, before time.Time) (int64, error) {
	result := r.ds.DB(ctx).Where("stat_minute < ?", before).Delete(&model.EndpointMinuteStat{})
	return result.RowsAffected, result.Error
}

// CleanupOldHourlyStats removes hourly stats older than retention period
func (r *MonitoringRepository) CleanupOldHourlyStats(ctx context.Context, before time.Time) (int64, error) {
	result := r.ds.DB(ctx).Where("stat_hour < ?", before).Delete(&model.EndpointHourlyStat{})
	return result.RowsAffected, result.Error
}

// CleanupOldDailyStats removes daily stats older than retention period
func (r *MonitoringRepository) CleanupOldDailyStats(ctx context.Context, before time.Time) (int64, error) {
	result := r.ds.DB(ctx).Where("stat_date < ?", before).Delete(&model.EndpointDailyStat{})
	return result.RowsAffected, result.Error
}

// AggregateMinuteStats aggregates task_events and worker_events into minute-level statistics
func (r *MonitoringRepository) AggregateMinuteStats(ctx context.Context, endpoint string, from, to time.Time) (*model.EndpointMinuteStat, error) {
	stat := &model.EndpointMinuteStat{Endpoint: endpoint, StatMinute: from}

	// 1. Task counts from task_events
	var taskStats struct {
		TasksSubmitted int `gorm:"column:tasks_submitted"`
		TasksCompleted int `gorm:"column:tasks_completed"`
		TasksFailed    int `gorm:"column:tasks_failed"`
		TasksTimeout   int `gorm:"column:tasks_timeout"`
		TasksRetried   int `gorm:"column:tasks_retried"`
	}
	r.ds.DB(ctx).Raw(`
		SELECT
			COUNT(CASE WHEN event_type = 'TASK_CREATED' THEN 1 END) as tasks_submitted,
			COUNT(CASE WHEN event_type = 'TASK_COMPLETED' THEN 1 END) as tasks_completed,
			COUNT(CASE WHEN event_type = 'TASK_FAILED' THEN 1 END) as tasks_failed,
			COUNT(CASE WHEN event_type = 'TASK_TIMEOUT' THEN 1 END) as tasks_timeout,
			COUNT(CASE WHEN event_type = 'TASK_REQUEUED' THEN 1 END) as tasks_retried
		FROM task_events WHERE endpoint = ? AND event_time >= ? AND event_time < ?
	`, endpoint, from, to).Scan(&taskStats)
	stat.TasksSubmitted = taskStats.TasksSubmitted
	stat.TasksCompleted = taskStats.TasksCompleted
	stat.TasksFailed = taskStats.TasksFailed
	stat.TasksTimeout = taskStats.TasksTimeout
	stat.TasksRetried = taskStats.TasksRetried

	// 2. AvgQueueWaitMs from TASK_ASSIGNED events
	var queueWait float64
	r.ds.DB(ctx).Raw(`
		SELECT COALESCE(AVG(queue_wait_ms), 0)
		FROM task_events 
		WHERE endpoint = ? AND event_time >= ? AND event_time < ? 
		AND event_type = 'TASK_ASSIGNED' AND queue_wait_ms IS NOT NULL
	`, endpoint, from, to).Scan(&queueWait)
	stat.AvgQueueWaitMs = queueWait

	// 3. Execution time stats from TASK_COMPLETED events
	var execStats struct {
		AvgMs float64 `gorm:"column:avg_ms"`
		P50Ms float64 `gorm:"column:p50_ms"`
		P95Ms float64 `gorm:"column:p95_ms"`
	}
	r.ds.DB(ctx).Raw(`
		SELECT 
			COALESCE(AVG(execution_duration_ms), 0) as avg_ms,
			COALESCE(MAX(CASE WHEN pct <= 0.50 THEN execution_duration_ms END), 0) as p50_ms,
			COALESCE(MAX(CASE WHEN pct <= 0.95 THEN execution_duration_ms END), 0) as p95_ms
		FROM (
			SELECT execution_duration_ms, PERCENT_RANK() OVER (ORDER BY execution_duration_ms) as pct
			FROM task_events 
			WHERE endpoint = ? AND event_time >= ? AND event_time < ? 
			AND event_type = 'TASK_COMPLETED' AND execution_duration_ms IS NOT NULL
		) t
	`, endpoint, from, to).Scan(&execStats)
	stat.AvgExecutionMs = execStats.AvgMs
	stat.P50ExecutionMs = execStats.P50Ms
	stat.P95ExecutionMs = execStats.P95Ms

	// 4. Worker stats - count active workers by status (current snapshot)
	var workerStats struct {
		ActiveWorkers int `gorm:"column:active_workers"`
		IdleWorkers   int `gorm:"column:idle_workers"`
		TotalWorkers  int `gorm:"column:total_workers"`
	}
	r.ds.DB(ctx).Raw(`
		SELECT
			COUNT(CASE WHEN current_jobs > 0 THEN 1 END) as active_workers,
			COUNT(CASE WHEN current_jobs = 0 THEN 1 END) as idle_workers,
			COUNT(*) as total_workers
		FROM workers WHERE endpoint = ? AND status IN ('ONLINE', 'BUSY', 'DRAINING')
	`, endpoint).Scan(&workerStats)
	stat.ActiveWorkers = workerStats.ActiveWorkers
	stat.IdleWorkers = workerStats.IdleWorkers
	// Utilization will be calculated after idle time is computed

	// 5. Worker idle stats - combine WORKER_TASK_PULLED events + current idle time
	// Handle boundary crossing: only count idle time within [from, to) window

	// Part 1: From WORKER_TASK_PULLED events in [from, to) - clip to window boundary
	var eventIdleStats struct {
		SumIdleMs float64 `gorm:"column:sum_idle_ms"`
		MaxIdleMs float64 `gorm:"column:max_idle_ms"`
		Count     int     `gorm:"column:count"`
	}
	r.ds.DB(ctx).Raw(`
		SELECT 
			COALESCE(SUM(LEAST(idle_duration_ms, TIMESTAMPDIFF(MICROSECOND, ?, event_time) / 1000)), 0) as sum_idle_ms,
			COALESCE(MAX(LEAST(idle_duration_ms, TIMESTAMPDIFF(MICROSECOND, ?, event_time) / 1000)), 0) as max_idle_ms,
			COUNT(*) as count
		FROM worker_events 
		WHERE endpoint = ? AND event_time >= ? AND event_time < ? 
		AND event_type = 'WORKER_TASK_PULLED' AND idle_duration_ms IS NOT NULL
	`, from, from, endpoint, from, to).Scan(&eventIdleStats)

	// Part 2: Current idle time for workers still idle at end of window
	// Use COALESCE(last_task_time, created_at) to handle new workers that never completed a task
	var currentIdleMs int64
	r.ds.DB(ctx).Raw(`
		SELECT COALESCE(CAST(SUM(
			TIMESTAMPDIFF(MICROSECOND, 
				GREATEST(COALESCE(last_task_time, created_at), ?), 
				?
			) / 1000
		) AS SIGNED), 0)
		FROM workers
		WHERE endpoint = ? AND status IN ('ONLINE', 'BUSY', 'DRAINING') AND current_jobs = 0
		AND COALESCE(last_task_time, created_at) < ?
	`, from, to, endpoint, to).Scan(&currentIdleMs)

	// Part 3: WORKER_TASK_PULLED events after window [to, to+2min) - their idle started before window end
	// Calculate how much of their idle time falls within [from, to)
	windowMs := to.Sub(from).Milliseconds()
	var futureIdleMs int64
	r.ds.DB(ctx).Raw(`
		SELECT COALESCE(CAST(SUM(
			LEAST(
				TIMESTAMPDIFF(MICROSECOND, ?, event_time) / 1000,
				LEAST(idle_duration_ms, TIMESTAMPDIFF(MICROSECOND, ?, event_time) / 1000)
			)
		) AS SIGNED), 0)
		FROM worker_events 
		WHERE endpoint = ? AND event_time >= ? AND event_time < ?
		AND event_type = 'WORKER_TASK_PULLED' AND idle_duration_ms IS NOT NULL
		AND TIMESTAMPDIFF(MICROSECOND, event_time, ?) / 1000 + idle_duration_ms > 0
	`, from, from, endpoint, to, to.Add(2*time.Minute), to).Scan(&futureIdleMs)

	totalIdleMs := int64(eventIdleStats.SumIdleMs) + currentIdleMs + futureIdleMs
	if totalIdleMs > windowMs*int64(workerStats.TotalWorkers) && workerStats.TotalWorkers > 0 {
		totalIdleMs = windowMs * int64(workerStats.TotalWorkers) // Cap at max possible
	}

	var maxIdleMs int64 = int64(eventIdleStats.MaxIdleMs)
	if currentIdleMs > maxIdleMs {
		maxIdleMs = currentIdleMs
	}

	// Use idle workers count for average calculation (not event count to avoid double counting)
	var avgIdleMs float64
	if workerStats.IdleWorkers > 0 {
		avgIdleMs = float64(totalIdleMs) / float64(workerStats.IdleWorkers)
	}

	stat.AvgIdleDurationSec = avgIdleMs / 1000
	stat.MaxIdleDurationSec = int(maxIdleMs / 1000)
	stat.TotalIdleTimeSec = int(totalIdleMs / 1000)
	stat.IdleCount = workerStats.TotalWorkers

	// Calculate active/idle workers based on idle time ratio
	// If idle_time == window_time, worker was fully idle; if idle_time < window_time, worker was active
	if workerStats.TotalWorkers > 0 && windowMs > 0 {
		totalWorkerTimeMs := int64(workerStats.TotalWorkers) * windowMs
		activeTimeMs := totalWorkerTimeMs - totalIdleMs
		if activeTimeMs < 0 {
			activeTimeMs = 0
		}
		// Estimate: if any active time, count as active worker
		if activeTimeMs > 0 {
			stat.ActiveWorkers = workerStats.TotalWorkers
			stat.IdleWorkers = 0
		} else {
			stat.ActiveWorkers = 0
			stat.IdleWorkers = workerStats.TotalWorkers
		}
		stat.AvgWorkerUtilization = float64(activeTimeMs) / float64(totalWorkerTimeMs) * 100
	}

	// 6. Worker lifecycle from worker_events
	var lifecycleStats struct {
		Created    int `gorm:"column:created"`
		Terminated int `gorm:"column:terminated"`
	}
	r.ds.DB(ctx).Raw(`
		SELECT 
			COUNT(CASE WHEN event_type = 'WORKER_REGISTERED' THEN 1 END) as created,
			COUNT(CASE WHEN event_type = 'WORKER_OFFLINE' THEN 1 END) as `+"`terminated`"+`
		FROM worker_events 
		WHERE endpoint = ? AND event_time >= ? AND event_time < ?
	`, endpoint, from, to).Scan(&lifecycleStats)
	stat.WorkersCreated = lifecycleStats.Created
	stat.WorkersTerminated = lifecycleStats.Terminated

	// 7. Cold start stats from worker_events (WORKER_REGISTERED has cold_start_duration_ms)
	var coldStats struct {
		ColdStarts     int     `gorm:"column:cold_starts"`
		AvgColdStartMs float64 `gorm:"column:avg_cold_start_ms"`
	}
	r.ds.DB(ctx).Raw(`
		SELECT COUNT(*) as cold_starts, COALESCE(AVG(cold_start_duration_ms), 0) as avg_cold_start_ms
		FROM worker_events 
		WHERE endpoint = ? AND event_time >= ? AND event_time < ? 
		AND event_type = 'WORKER_REGISTERED' AND cold_start_duration_ms IS NOT NULL
	`, endpoint, from, to).Scan(&coldStats)
	stat.ColdStarts = coldStats.ColdStarts
	stat.AvgColdStartMs = coldStats.AvgColdStartMs

	return stat, nil
}

// AggregateHourlyStats aggregates minute stats into hourly statistics
func (r *MonitoringRepository) AggregateHourlyStats(ctx context.Context, endpoint string, statHour time.Time) (*model.EndpointHourlyStat, error) {
	stat := &model.EndpointHourlyStat{Endpoint: endpoint, StatHour: statHour}
	from, to := statHour, statHour.Add(time.Hour)

	var count int64
	r.ds.DB(ctx).Model(&model.EndpointMinuteStat{}).Where("endpoint = ? AND stat_minute >= ? AND stat_minute < ?", endpoint, from, to).Count(&count)
	if count == 0 {
		return stat, nil
	}

	var result map[string]interface{}
	r.ds.DB(ctx).Raw(`
		SELECT
			COALESCE(AVG(active_workers), 0) as active_workers,
			COALESCE(AVG(idle_workers), 0) as idle_workers,
			COALESCE(AVG(avg_worker_utilization), 0) as avg_worker_utilization,
			COALESCE(SUM(tasks_submitted), 0) as tasks_submitted,
			COALESCE(SUM(tasks_completed), 0) as tasks_completed,
			COALESCE(SUM(tasks_failed), 0) as tasks_failed,
			COALESCE(SUM(tasks_timeout), 0) as tasks_timeout,
			COALESCE(SUM(tasks_retried), 0) as tasks_retried,
			COALESCE(AVG(avg_queue_wait_ms), 0) as avg_queue_wait_ms,
			COALESCE(AVG(avg_execution_ms), 0) as avg_execution_ms,
			COALESCE(AVG(p50_execution_ms), 0) as p50_execution_ms,
			COALESCE(MAX(p95_execution_ms), 0) as p95_execution_ms,
			COALESCE(AVG(avg_idle_duration_sec), 0) as avg_idle_duration_sec,
			COALESCE(MAX(max_idle_duration_sec), 0) as max_idle_duration_sec,
			COALESCE(SUM(total_idle_time_sec), 0) as total_idle_time_sec,
			COALESCE(SUM(idle_count), 0) as idle_count,
			COALESCE(SUM(workers_created), 0) as workers_created,
			COALESCE(SUM(workers_terminated), 0) as workers_terminated,
			COALESCE(SUM(cold_starts), 0) as cold_starts,
			COALESCE(AVG(avg_cold_start_ms), 0) as avg_cold_start_ms
		FROM endpoint_minute_stats WHERE endpoint = ? AND stat_minute >= ? AND stat_minute < ?
	`, endpoint, from, to).Scan(&result)

	if result != nil {
		stat.ActiveWorkers = toInt(result["active_workers"])
		stat.IdleWorkers = toInt(result["idle_workers"])
		stat.AvgWorkerUtilization = toFloat(result["avg_worker_utilization"])
		stat.TasksSubmitted = toInt(result["tasks_submitted"])
		stat.TasksCompleted = toInt(result["tasks_completed"])
		stat.TasksFailed = toInt(result["tasks_failed"])
		stat.TasksTimeout = toInt(result["tasks_timeout"])
		stat.TasksRetried = toInt(result["tasks_retried"])
		stat.AvgQueueWaitMs = toFloat(result["avg_queue_wait_ms"])
		stat.AvgExecutionMs = toFloat(result["avg_execution_ms"])
		stat.P50ExecutionMs = toFloat(result["p50_execution_ms"])
		stat.P95ExecutionMs = toFloat(result["p95_execution_ms"])
		stat.AvgIdleDurationSec = toFloat(result["avg_idle_duration_sec"])
		stat.MaxIdleDurationSec = toInt(result["max_idle_duration_sec"])
		stat.TotalIdleTimeSec = toInt64(result["total_idle_time_sec"])
		stat.IdleCount = toInt(result["idle_count"])
		stat.WorkersCreated = toInt(result["workers_created"])
		stat.WorkersTerminated = toInt(result["workers_terminated"])
		stat.ColdStarts = toInt(result["cold_starts"])
		stat.AvgColdStartMs = toFloat(result["avg_cold_start_ms"])
	}

	return stat, nil
}

// AggregateDailyStats aggregates hourly stats into daily statistics
func (r *MonitoringRepository) AggregateDailyStats(ctx context.Context, endpoint string, statDate time.Time) (*model.EndpointDailyStat, error) {
	stat := &model.EndpointDailyStat{Endpoint: endpoint, StatDate: statDate}
	from, to := statDate, statDate.AddDate(0, 0, 1)

	var count int64
	r.ds.DB(ctx).Model(&model.EndpointHourlyStat{}).Where("endpoint = ? AND stat_hour >= ? AND stat_hour < ?", endpoint, from, to).Count(&count)
	if count == 0 {
		return stat, nil
	}

	var result map[string]interface{}
	r.ds.DB(ctx).Raw(`
		SELECT
			COALESCE(AVG(active_workers), 0) as active_workers,
			COALESCE(AVG(idle_workers), 0) as idle_workers,
			COALESCE(AVG(avg_worker_utilization), 0) as avg_worker_utilization,
			COALESCE(SUM(tasks_submitted), 0) as tasks_submitted,
			COALESCE(SUM(tasks_completed), 0) as tasks_completed,
			COALESCE(SUM(tasks_failed), 0) as tasks_failed,
			COALESCE(SUM(tasks_timeout), 0) as tasks_timeout,
			COALESCE(SUM(tasks_retried), 0) as tasks_retried,
			COALESCE(AVG(avg_queue_wait_ms), 0) as avg_queue_wait_ms,
			COALESCE(AVG(avg_execution_ms), 0) as avg_execution_ms,
			COALESCE(AVG(p50_execution_ms), 0) as p50_execution_ms,
			COALESCE(MAX(p95_execution_ms), 0) as p95_execution_ms,
			COALESCE(AVG(avg_idle_duration_sec), 0) as avg_idle_duration_sec,
			COALESCE(MAX(max_idle_duration_sec), 0) as max_idle_duration_sec,
			COALESCE(SUM(total_idle_time_sec), 0) as total_idle_time_sec,
			COALESCE(SUM(idle_count), 0) as idle_count,
			COALESCE(SUM(workers_created), 0) as workers_created,
			COALESCE(SUM(workers_terminated), 0) as workers_terminated,
			COALESCE(SUM(cold_starts), 0) as cold_starts,
			COALESCE(AVG(avg_cold_start_ms), 0) as avg_cold_start_ms
		FROM endpoint_hourly_stats WHERE endpoint = ? AND stat_hour >= ? AND stat_hour < ?
	`, endpoint, from, to).Scan(&result)

	if result != nil {
		stat.ActiveWorkers = toInt(result["active_workers"])
		stat.IdleWorkers = toInt(result["idle_workers"])
		stat.AvgWorkerUtilization = toFloat(result["avg_worker_utilization"])
		stat.TasksSubmitted = toInt(result["tasks_submitted"])
		stat.TasksCompleted = toInt(result["tasks_completed"])
		stat.TasksFailed = toInt(result["tasks_failed"])
		stat.TasksTimeout = toInt(result["tasks_timeout"])
		stat.TasksRetried = toInt(result["tasks_retried"])
		stat.AvgQueueWaitMs = toFloat(result["avg_queue_wait_ms"])
		stat.AvgExecutionMs = toFloat(result["avg_execution_ms"])
		stat.P50ExecutionMs = toFloat(result["p50_execution_ms"])
		stat.P95ExecutionMs = toFloat(result["p95_execution_ms"])
		stat.AvgIdleDurationSec = toFloat(result["avg_idle_duration_sec"])
		stat.MaxIdleDurationSec = toInt(result["max_idle_duration_sec"])
		stat.TotalIdleTimeSec = toInt64(result["total_idle_time_sec"])
		stat.IdleCount = toInt(result["idle_count"])
		stat.WorkersCreated = toInt(result["workers_created"])
		stat.WorkersTerminated = toInt(result["workers_terminated"])
		stat.ColdStarts = toInt(result["cold_starts"])
		stat.AvgColdStartMs = toFloat(result["avg_cold_start_ms"])
	}

	return stat, nil
}

// GetDistinctEndpoints returns all distinct endpoints from task_events
func (r *MonitoringRepository) GetDistinctEndpoints(ctx context.Context, from, to time.Time) ([]string, error) {
	var endpoints []string
	err := r.ds.DB(ctx).Model(&model.TaskEvent{}).
		Where("event_time >= ? AND event_time < ?", from, to).
		Distinct().Pluck("endpoint", &endpoints).Error
	return endpoints, err
}

// GetAllEndpoints returns all non-deleted endpoints from endpoints table
func (r *MonitoringRepository) GetAllEndpoints(ctx context.Context) ([]string, error) {
	var endpoints []string
	err := r.ds.DB(ctx).Model(&model.Endpoint{}).Where("status != ?", "deleted").Pluck("endpoint", &endpoints).Error
	return endpoints, err
}

// GetRealtimeMetrics returns real-time metrics for an endpoint
func (r *MonitoringRepository) GetRealtimeMetrics(ctx context.Context, endpoint string) (*RealtimeMetrics, error) {
	metrics := &RealtimeMetrics{Endpoint: endpoint}

	r.ds.DB(ctx).Raw(`
		SELECT COUNT(*) as total,
			COUNT(CASE WHEN current_jobs > 0 THEN 1 END) as active,
			COUNT(CASE WHEN current_jobs = 0 THEN 1 END) as idle
		FROM workers WHERE endpoint = ? AND status IN ('ONLINE', 'BUSY', 'DRAINING')
	`, endpoint).Scan(&metrics.Workers)

	lastMinute := time.Now().Add(-time.Minute)
	r.ds.DB(ctx).Raw(`
		SELECT
			(SELECT COUNT(*) FROM tasks WHERE endpoint = ? AND status = 'PENDING') as in_queue,
			(SELECT COUNT(*) FROM tasks WHERE endpoint = ? AND status = 'IN_PROGRESS') as running,
			(SELECT COUNT(*) FROM task_events WHERE endpoint = ? AND event_type = 'TASK_COMPLETED' AND event_time >= ?) as completed_last_minute
	`, endpoint, endpoint, endpoint, lastMinute).Scan(&metrics.Tasks)

	last5Min := time.Now().Add(-5 * time.Minute)
	r.ds.DB(ctx).Raw(`
		SELECT COALESCE(AVG(queue_wait_ms), 0) as avg_queue_wait_ms,
			COALESCE(AVG(execution_duration_ms), 0) as avg_execution_ms,
			COALESCE(AVG(total_duration_ms), 0) as avg_total_duration_ms
		FROM task_events WHERE endpoint = ? AND event_type = 'TASK_COMPLETED' AND event_time >= ?
	`, endpoint, last5Min).Scan(&metrics.Performance)

	return metrics, nil
}

// RealtimeMetrics represents real-time metrics for an endpoint
type RealtimeMetrics struct {
	Endpoint string `json:"endpoint"`
	Workers  struct {
		Total  int `json:"total"`
		Active int `json:"active"`
		Idle   int `json:"idle"`
	} `json:"workers"`
	Tasks struct {
		InQueue             int64 `json:"in_queue"`
		Running             int64 `json:"running"`
		CompletedLastMinute int   `json:"completed_last_minute"`
	} `json:"tasks"`
	Performance struct {
		AvgQueueWaitMs     float64 `json:"avg_queue_wait_ms"`
		AvgExecutionMs     float64 `json:"avg_execution_ms"`
		AvgTotalDurationMs float64 `json:"avg_total_duration_ms"`
	} `json:"performance"`
}

// SaveWorkerEvent saves a worker event
func (r *MonitoringRepository) SaveWorkerEvent(ctx context.Context, event *model.WorkerEvent) error {
	return r.ds.DB(ctx).Create(event).Error
}

// CountWorkerEvents counts events for a worker by type
func (r *MonitoringRepository) CountWorkerEvents(ctx context.Context, workerID, eventType string, count *int64) {
	r.ds.DB(ctx).Model(&model.WorkerEvent{}).Where("worker_id = ? AND event_type = ?", workerID, eventType).Count(count)
}

// CleanupOldWorkerEvents removes worker events older than retention period in batches
func (r *MonitoringRepository) CleanupOldWorkerEvents(ctx context.Context, before time.Time) (int64, error) {
	const batchSize = 5000
	var total int64
	for {
		result := r.ds.DB(ctx).Where("event_time < ?", before).Limit(batchSize).Delete(&model.WorkerEvent{})
		if result.Error != nil {
			return total, result.Error
		}
		total += result.RowsAffected
		if result.RowsAffected < batchSize {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	return total, nil
}
