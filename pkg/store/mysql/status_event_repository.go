package mysql

import (
	"context"
	"fmt"
	"time"

	"waverless/pkg/store/mysql/model"

	"gorm.io/gorm"
)

// StatusEventFilter defines query filters for status events
type StatusEventFilter struct {
	Endpoint  string
	WorkerID  string
	EventType string
	StartTime *time.Time
	EndTime   *time.Time
	Limit     int
	Offset    int
}

// StatusEventRepository handles status event persistence in MySQL
type StatusEventRepository struct {
	ds *Datastore
}

// NewStatusEventRepository creates a new status event repository
func NewStatusEventRepository(ds *Datastore) *StatusEventRepository {
	return &StatusEventRepository{ds: ds}
}

// Create creates a new status event
func (r *StatusEventRepository) Create(ctx context.Context, event *model.StatusEvent) error {
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now()
	}
	return r.ds.DB(ctx).Create(event).Error
}

// List lists status events with filters
func (r *StatusEventRepository) List(ctx context.Context, filter *StatusEventFilter) ([]model.StatusEvent, error) {
	var events []model.StatusEvent

	query := r.ds.DB(ctx).Model(&model.StatusEvent{})

	// Apply filters
	query = r.applyFilters(query, filter)

	// Apply pagination
	if filter != nil {
		if filter.Limit > 0 {
			query = query.Limit(filter.Limit)
		}
		if filter.Offset > 0 {
			query = query.Offset(filter.Offset)
		}
	}

	// Order by created_at descending (most recent first)
	query = query.Order("created_at DESC")

	err := query.Find(&events).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list status events: %w", err)
	}

	return events, nil
}

// ListByEndpoint lists status events for a specific endpoint
func (r *StatusEventRepository) ListByEndpoint(ctx context.Context, endpoint string, limit, offset int) ([]model.StatusEvent, error) {
	filter := &StatusEventFilter{
		Endpoint: endpoint,
		Limit:    limit,
		Offset:   offset,
	}
	return r.List(ctx, filter)
}

// ListByWorker lists status events for a specific worker
func (r *StatusEventRepository) ListByWorker(ctx context.Context, workerID string, limit, offset int) ([]model.StatusEvent, error) {
	filter := &StatusEventFilter{
		WorkerID: workerID,
		Limit:    limit,
		Offset:   offset,
	}
	return r.List(ctx, filter)
}

// DeleteOldEvents deletes events older than the specified duration
// Returns the number of deleted events
func (r *StatusEventRepository) DeleteOldEvents(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoffTime := time.Now().Add(-olderThan)

	// Delete in batches to avoid long-running transactions
	const batchSize = 5000
	var total int64

	for {
		result := r.ds.DB(ctx).
			Where("created_at < ?", cutoffTime).
			Limit(batchSize).
			Delete(&model.StatusEvent{})

		if result.Error != nil {
			return total, fmt.Errorf("failed to delete old status events: %w", result.Error)
		}

		total += result.RowsAffected

		// If we deleted fewer than batch size, we're done
		if result.RowsAffected < batchSize {
			break
		}

		// Small delay to avoid overwhelming the database
		time.Sleep(100 * time.Millisecond)
	}

	return total, nil
}

// applyFilters applies the filter conditions to the query
func (r *StatusEventRepository) applyFilters(query *gorm.DB, filter *StatusEventFilter) *gorm.DB {
	if filter == nil {
		return query
	}

	if filter.Endpoint != "" {
		query = query.Where("endpoint = ?", filter.Endpoint)
	}

	if filter.WorkerID != "" {
		query = query.Where("worker_id = ?", filter.WorkerID)
	}

	if filter.EventType != "" {
		query = query.Where("event_type = ?", filter.EventType)
	}

	if filter.StartTime != nil {
		query = query.Where("created_at >= ?", *filter.StartTime)
	}

	if filter.EndTime != nil {
		query = query.Where("created_at <= ?", *filter.EndTime)
	}

	return query
}
