package mysql

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// TaskEventRepository handles task event persistence in MySQL
type TaskEventRepository struct {
	ds *Datastore
}

// NewTaskEventRepository creates a new task event repository
func NewTaskEventRepository(ds *Datastore) *TaskEventRepository {
	return &TaskEventRepository{ds: ds}
}

// RecordEvent creates a new task event
func (r *TaskEventRepository) RecordEvent(ctx context.Context, event *TaskEvent) error {
	if event.EventID == "" {
		event.EventID = generateEventID()
	}
	if event.EventTime.IsZero() {
		event.EventTime = time.Now()
	}
	return r.ds.DB(ctx).Create(event).Error
}

// GetTaskEvents retrieves all events for a task (ordered by time)
func (r *TaskEventRepository) GetTaskEvents(ctx context.Context, taskID string) ([]*TaskEvent, error) {
	var events []*TaskEvent
	err := r.ds.DB(ctx).
		Where("task_id = ?", taskID).
		Order("event_time ASC").
		Find(&events).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get task events: %w", err)
	}
	return events, nil
}

// GetTaskTimeline retrieves task timeline (simplified event list)
func (r *TaskEventRepository) GetTaskTimeline(ctx context.Context, taskID string) ([]*TaskEvent, error) {
	var events []*TaskEvent
	err := r.ds.DB(ctx).
		Where("task_id = ?", taskID).
		Select("event_type, event_time, worker_id, error_message, from_status, to_status").
		Order("event_time ASC").
		Find(&events).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get task timeline: %w", err)
	}
	return events, nil
}

// GetWorkerTaskHistory retrieves tasks processed by a worker
func (r *TaskEventRepository) GetWorkerTaskHistory(ctx context.Context, workerID string, limit int) ([]*TaskEvent, error) {
	if limit <= 0 {
		limit = 100
	}

	var events []*TaskEvent
	err := r.ds.DB(ctx).
		Where("worker_id = ?", workerID).
		Order("event_time DESC").
		Limit(limit).
		Find(&events).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get worker task history: %w", err)
	}
	return events, nil
}

// GetTaskRetryCount returns the retry count for a task
func (r *TaskEventRepository) GetTaskRetryCount(ctx context.Context, taskID string) (int, error) {
	var count int64
	err := r.ds.DB(ctx).
		Model(&TaskEvent{}).
		Where("task_id = ? AND event_type IN (?, ?)",
			taskID,
			"TASK_REQUEUED",
			"TASK_FAILED").
		Count(&count).Error
	if err != nil {
		return 0, fmt.Errorf("failed to get task retry count: %w", err)
	}
	return int(count), nil
}

// GetEndpointEventStats retrieves event statistics for an endpoint
func (r *TaskEventRepository) GetEndpointEventStats(ctx context.Context, endpoint string, startTime, endTime time.Time) (map[string]int64, error) {
	type EventStat struct {
		EventType string
		Count     int64
	}

	var stats []EventStat
	err := r.ds.DB(ctx).
		Model(&TaskEvent{}).
		Select("event_type, COUNT(*) as count").
		Where("endpoint = ? AND event_time BETWEEN ? AND ?", endpoint, startTime, endTime).
		Group("event_type").
		Scan(&stats).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get endpoint event stats: %w", err)
	}

	result := make(map[string]int64)
	for _, s := range stats {
		result[s.EventType] = s.Count
	}
	return result, nil
}

// GetRecentEvents retrieves recent task events
func (r *TaskEventRepository) GetRecentEvents(ctx context.Context, limit int) ([]*TaskEvent, error) {
	if limit <= 0 {
		limit = 100
	}

	var events []*TaskEvent
	err := r.ds.DB(ctx).
		Order("event_time DESC").
		Limit(limit).
		Find(&events).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get recent events: %w", err)
	}
	return events, nil
}

// GetEventsByType retrieves events by type
func (r *TaskEventRepository) GetEventsByType(ctx context.Context, eventType string, limit int) ([]*TaskEvent, error) {
	if limit <= 0 {
		limit = 100
	}

	var events []*TaskEvent
	err := r.ds.DB(ctx).
		Where("event_type = ?", eventType).
		Order("event_time DESC").
		Limit(limit).
		Find(&events).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get events by type: %w", err)
	}
	return events, nil
}

// GetEventsByEndpoint retrieves events for an endpoint
func (r *TaskEventRepository) GetEventsByEndpoint(ctx context.Context, endpoint string, limit int) ([]*TaskEvent, error) {
	if limit <= 0 {
		limit = 100
	}

	var events []*TaskEvent
	err := r.ds.DB(ctx).
		Where("endpoint = ?", endpoint).
		Order("event_time DESC").
		Limit(limit).
		Find(&events).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get events by endpoint: %w", err)
	}
	return events, nil
}

// DeleteOldEvents deletes events older than the specified duration
func (r *TaskEventRepository) DeleteOldEvents(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoffTime := time.Now().Add(-olderThan)

	result := r.ds.DB(ctx).
		Where("event_time < ?", cutoffTime).
		Delete(&TaskEvent{})

	if result.Error != nil {
		return 0, fmt.Errorf("failed to delete old events: %w", result.Error)
	}

	return result.RowsAffected, nil
}

// generateEventID generates a unique event ID
func generateEventID() string {
	return uuid.New().String()
}
