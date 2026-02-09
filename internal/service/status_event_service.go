// Package service provides business logic services for the Waverless platform.
// This file implements the StatusEventService for managing status event recording and querying.
// It implements the status event management logic for Requirements 3.1, 3.2, 3.5.
package service

import (
	"context"
	"fmt"
	"time"

	"waverless/pkg/status"
	"waverless/pkg/store/mysql"
	"waverless/pkg/store/mysql/model"
)

// StatusEventType represents the type of status event.
// This mirrors the model.StatusEventType for service layer usage.
type StatusEventType string

const (
	// EventTypeStatusChange indicates a worker status change.
	EventTypeStatusChange StatusEventType = "STATUS_CHANGE"
	// EventTypePhaseChange indicates a pending phase change.
	EventTypePhaseChange StatusEventType = "PHASE_CHANGE"
	// EventTypeFailure indicates a worker failure.
	EventTypeFailure StatusEventType = "FAILURE"
	// EventTypeRecovery indicates a worker recovery.
	EventTypeRecovery StatusEventType = "RECOVERY"
)

// StatusEvent represents a status change event in the service layer.
// This is the domain model used by the service, separate from the database model.
type StatusEvent struct {
	ID         int64              `json:"id"`
	WorkerID   string             `json:"workerId"`
	Endpoint   string             `json:"endpoint"`
	EventType  StatusEventType    `json:"eventType"`
	OldStatus  string             `json:"oldStatus,omitempty"`
	NewStatus  string             `json:"newStatus"`
	Phase      string             `json:"phase,omitempty"`
	Reason     string             `json:"reason,omitempty"`
	Message    string             `json:"message,omitempty"`
	SpotStatus *status.SpotStatus `json:"spotStatus,omitempty"`
	CreatedAt  time.Time          `json:"createdAt"`
}

// StatusEventFilter defines query filters for status events.
// Supports filtering by endpoint, worker_id, event type, and time range.
type StatusEventFilter struct {
	// Endpoint filters events by endpoint name.
	Endpoint string
	// WorkerID filters events by worker ID.
	WorkerID string
	// EventType filters events by event type.
	EventType StatusEventType
	// StartTime filters events created at or after this time.
	StartTime *time.Time
	// EndTime filters events created at or before this time.
	EndTime *time.Time
	// Limit specifies the maximum number of events to return.
	Limit int
	// Offset specifies the number of events to skip (for pagination).
	Offset int
}

// StatusEventService manages status event recording and querying.
// It provides methods to record new events and list events with filters.
type StatusEventService struct {
	repo            *mysql.StatusEventRepository
	pendingDetector *status.PendingPhaseDetector
}

// NewStatusEventService creates a new StatusEventService.
// Parameters:
//   - repo: The status event repository for database operations.
//   - pendingDetector: Optional pending phase detector for enriching events with phase info.
//
// Returns a new StatusEventService instance.
func NewStatusEventService(repo *mysql.StatusEventRepository, pendingDetector *status.PendingPhaseDetector) *StatusEventService {
	return &StatusEventService{
		repo:            repo,
		pendingDetector: pendingDetector,
	}
}

// RecordEvent records a new status event.
// The event is persisted to the database with the current timestamp if not set.
//
// Parameters:
//   - ctx: The context for the operation.
//   - event: The status event to record.
//
// Returns an error if the event could not be recorded.
func (s *StatusEventService) RecordEvent(ctx context.Context, event *StatusEvent) error {
	if event == nil {
		return fmt.Errorf("event cannot be nil")
	}

	// Validate required fields
	if event.WorkerID == "" {
		return fmt.Errorf("worker_id is required")
	}
	if event.Endpoint == "" {
		return fmt.Errorf("endpoint is required")
	}
	if event.NewStatus == "" {
		return fmt.Errorf("new_status is required")
	}

	// Convert service event to model event
	modelEvent := s.toModelEvent(event)

	// Record the event
	if err := s.repo.Create(ctx, modelEvent); err != nil {
		return fmt.Errorf("failed to record status event: %w", err)
	}

	// Update the event ID from the created record
	event.ID = modelEvent.ID

	return nil
}

// ListEvents lists status events with filters.
// Events are returned in descending order by creation time (most recent first).
//
// Parameters:
//   - ctx: The context for the operation.
//   - filter: The filter criteria for querying events.
//
// Returns a slice of StatusEvent and an error if the query failed.
func (s *StatusEventService) ListEvents(ctx context.Context, filter *StatusEventFilter) ([]StatusEvent, error) {
	// Convert service filter to repository filter
	repoFilter := s.toRepoFilter(filter)

	// Query events from repository
	modelEvents, err := s.repo.List(ctx, repoFilter)
	if err != nil {
		return nil, fmt.Errorf("failed to list status events: %w", err)
	}

	// Convert model events to service events
	events := make([]StatusEvent, len(modelEvents))
	for i, modelEvent := range modelEvents {
		events[i] = s.fromModelEvent(&modelEvent)
	}

	return events, nil
}

// ListEventsByEndpoint lists status events for a specific endpoint.
// This is a convenience method that wraps ListEvents with an endpoint filter.
//
// Parameters:
//   - ctx: The context for the operation.
//   - endpoint: The endpoint name to filter by.
//   - limit: Maximum number of events to return.
//   - offset: Number of events to skip.
//
// Returns a slice of StatusEvent and an error if the query failed.
func (s *StatusEventService) ListEventsByEndpoint(ctx context.Context, endpoint string, limit, offset int) ([]StatusEvent, error) {
	filter := &StatusEventFilter{
		Endpoint: endpoint,
		Limit:    limit,
		Offset:   offset,
	}
	return s.ListEvents(ctx, filter)
}

// ListEventsByWorker lists status events for a specific worker.
// This is a convenience method that wraps ListEvents with a worker filter.
//
// Parameters:
//   - ctx: The context for the operation.
//   - workerID: The worker ID to filter by.
//   - limit: Maximum number of events to return.
//   - offset: Number of events to skip.
//
// Returns a slice of StatusEvent and an error if the query failed.
func (s *StatusEventService) ListEventsByWorker(ctx context.Context, workerID string, limit, offset int) ([]StatusEvent, error) {
	filter := &StatusEventFilter{
		WorkerID: workerID,
		Limit:    limit,
		Offset:   offset,
	}
	return s.ListEvents(ctx, filter)
}

// RecordStatusChange records a status change event.
// This is a convenience method for recording STATUS_CHANGE events.
//
// Parameters:
//   - ctx: The context for the operation.
//   - workerID: The worker ID.
//   - endpoint: The endpoint name.
//   - oldStatus: The previous status (can be empty for initial status).
//   - newStatus: The new status.
//   - reason: Optional reason for the status change.
//   - message: Optional message describing the status change.
//
// Returns an error if the event could not be recorded.
func (s *StatusEventService) RecordStatusChange(ctx context.Context, workerID, endpoint, oldStatus, newStatus, reason, message string) error {
	event := &StatusEvent{
		WorkerID:  workerID,
		Endpoint:  endpoint,
		EventType: EventTypeStatusChange,
		OldStatus: oldStatus,
		NewStatus: newStatus,
		Reason:    reason,
		Message:   message,
		CreatedAt: time.Now(),
	}
	return s.RecordEvent(ctx, event)
}

// RecordPhaseChange records a pending phase change event.
// This is a convenience method for recording PHASE_CHANGE events.
//
// Parameters:
//   - ctx: The context for the operation.
//   - workerID: The worker ID.
//   - endpoint: The endpoint name.
//   - phase: The new pending phase.
//   - reason: Optional reason for the phase change.
//   - message: Optional message describing the phase change.
//   - spotStatus: Optional Spot status (for WAITING_NODE phase).
//
// Returns an error if the event could not be recorded.
func (s *StatusEventService) RecordPhaseChange(ctx context.Context, workerID, endpoint, phase, reason, message string, spotStatus *status.SpotStatus) error {
	event := &StatusEvent{
		WorkerID:   workerID,
		Endpoint:   endpoint,
		EventType:  EventTypePhaseChange,
		NewStatus:  "PENDING",
		Phase:      phase,
		Reason:     reason,
		Message:    message,
		SpotStatus: spotStatus,
		CreatedAt:  time.Now(),
	}
	return s.RecordEvent(ctx, event)
}

// RecordFailure records a failure event.
// This is a convenience method for recording FAILURE events.
//
// Parameters:
//   - ctx: The context for the operation.
//   - workerID: The worker ID.
//   - endpoint: The endpoint name.
//   - reason: The failure reason.
//   - message: The failure message.
//
// Returns an error if the event could not be recorded.
func (s *StatusEventService) RecordFailure(ctx context.Context, workerID, endpoint, reason, message string) error {
	event := &StatusEvent{
		WorkerID:  workerID,
		Endpoint:  endpoint,
		EventType: EventTypeFailure,
		NewStatus: "FAILED",
		Reason:    reason,
		Message:   message,
		CreatedAt: time.Now(),
	}
	return s.RecordEvent(ctx, event)
}

// RecordRecovery records a recovery event.
// This is a convenience method for recording RECOVERY events.
//
// Parameters:
//   - ctx: The context for the operation.
//   - workerID: The worker ID.
//   - endpoint: The endpoint name.
//   - newStatus: The new status after recovery.
//   - reason: Optional reason for the recovery.
//   - message: Optional message describing the recovery.
//
// Returns an error if the event could not be recorded.
func (s *StatusEventService) RecordRecovery(ctx context.Context, workerID, endpoint, newStatus, reason, message string) error {
	event := &StatusEvent{
		WorkerID:  workerID,
		Endpoint:  endpoint,
		EventType: EventTypeRecovery,
		OldStatus: "FAILED",
		NewStatus: newStatus,
		Reason:    reason,
		Message:   message,
		CreatedAt: time.Now(),
	}
	return s.RecordEvent(ctx, event)
}

// toModelEvent converts a service StatusEvent to a model StatusEvent.
func (s *StatusEventService) toModelEvent(event *StatusEvent) *model.StatusEvent {
	modelEvent := &model.StatusEvent{
		WorkerID:  event.WorkerID,
		Endpoint:  event.Endpoint,
		EventType: string(event.EventType),
		NewStatus: event.NewStatus,
		CreatedAt: event.CreatedAt,
	}

	// Set optional fields
	if event.OldStatus != "" {
		modelEvent.OldStatus = &event.OldStatus
	}
	if event.Phase != "" {
		modelEvent.Phase = &event.Phase
	}
	if event.Reason != "" {
		modelEvent.Reason = &event.Reason
	}
	if event.Message != "" {
		modelEvent.Message = &event.Message
	}

	// Convert SpotStatus to JSON map
	if event.SpotStatus != nil {
		modelEvent.SpotStatus = model.JSONMap{
			"capacity":     string(event.SpotStatus.Capacity),
			"score":        event.SpotStatus.Score,
			"price":        event.SpotStatus.Price,
			"instanceType": event.SpotStatus.InstanceType,
		}
	}

	return modelEvent
}

// fromModelEvent converts a model StatusEvent to a service StatusEvent.
func (s *StatusEventService) fromModelEvent(modelEvent *model.StatusEvent) StatusEvent {
	event := StatusEvent{
		ID:        modelEvent.ID,
		WorkerID:  modelEvent.WorkerID,
		Endpoint:  modelEvent.Endpoint,
		EventType: StatusEventType(modelEvent.EventType),
		NewStatus: modelEvent.NewStatus,
		CreatedAt: modelEvent.CreatedAt,
	}

	// Set optional fields
	if modelEvent.OldStatus != nil {
		event.OldStatus = *modelEvent.OldStatus
	}
	if modelEvent.Phase != nil {
		event.Phase = *modelEvent.Phase
	}
	if modelEvent.Reason != nil {
		event.Reason = *modelEvent.Reason
	}
	if modelEvent.Message != nil {
		event.Message = *modelEvent.Message
	}

	// Convert JSON map to SpotStatus
	if len(modelEvent.SpotStatus) > 0 {
		event.SpotStatus = s.parseSpotStatus(modelEvent.SpotStatus)
	}

	return event
}

// parseSpotStatus parses a JSON map into a SpotStatus struct.
func (s *StatusEventService) parseSpotStatus(jsonMap model.JSONMap) *status.SpotStatus {
	spotStatus := &status.SpotStatus{}

	if capacity, ok := jsonMap["capacity"].(string); ok {
		spotStatus.Capacity = status.SpotCapacity(capacity)
	}
	if score, ok := jsonMap["score"].(float64); ok {
		spotStatus.Score = int(score)
	} else if score, ok := jsonMap["score"].(int); ok {
		spotStatus.Score = score
	}
	if price, ok := jsonMap["price"].(float64); ok {
		spotStatus.Price = price
	}
	if instanceType, ok := jsonMap["instanceType"].(string); ok {
		spotStatus.InstanceType = instanceType
	}

	return spotStatus
}

// toRepoFilter converts a service StatusEventFilter to a repository StatusEventFilter.
func (s *StatusEventService) toRepoFilter(filter *StatusEventFilter) *mysql.StatusEventFilter {
	if filter == nil {
		return nil
	}

	return &mysql.StatusEventFilter{
		Endpoint:  filter.Endpoint,
		WorkerID:  filter.WorkerID,
		EventType: string(filter.EventType),
		StartTime: filter.StartTime,
		EndTime:   filter.EndTime,
		Limit:     filter.Limit,
		Offset:    filter.Offset,
	}
}
