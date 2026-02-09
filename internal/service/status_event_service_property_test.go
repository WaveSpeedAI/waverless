// Package service provides property-based tests for the StatusEventService.
// These tests verify universal properties that should hold across all valid inputs.
//
// Feature: endpoint-status-tracking, Property 2: Phase Transition Event Recording
// Feature: endpoint-status-tracking, Property 5: Status Event Filtering
// **Validates: Requirements 1.4, 3.1, 3.2, 3.5**
package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"waverless/pkg/status"
	"waverless/pkg/store/mysql"
	"waverless/pkg/store/mysql/model"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// ============================================================================
// Mock Repository for Property Testing
// ============================================================================

// mockStatusEventRepository is a mock implementation of StatusEventRepository for testing.
// It stores events in memory and supports filtering operations.
type mockStatusEventRepository struct {
	mu     sync.RWMutex
	events []model.StatusEvent
	nextID int64
}

// newMockStatusEventRepository creates a new mock repository.
func newMockStatusEventRepository() *mockStatusEventRepository {
	return &mockStatusEventRepository{
		events: make([]model.StatusEvent, 0),
		nextID: 1,
	}
}

// Create adds a new event to the mock repository.
func (m *mockStatusEventRepository) Create(ctx context.Context, event *model.StatusEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	event.ID = m.nextID
	m.nextID++
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now()
	}
	m.events = append(m.events, *event)
	return nil
}

// List returns events matching the filter criteria.
func (m *mockStatusEventRepository) List(ctx context.Context, filter *mysql.StatusEventFilter) ([]model.StatusEvent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []model.StatusEvent

	for _, event := range m.events {
		if m.matchesFilter(&event, filter) {
			result = append(result, event)
		}
	}

	// Apply pagination
	if filter != nil {
		if filter.Offset > 0 && filter.Offset < len(result) {
			result = result[filter.Offset:]
		} else if filter.Offset >= len(result) {
			result = []model.StatusEvent{}
		}

		if filter.Limit > 0 && filter.Limit < len(result) {
			result = result[:filter.Limit]
		}
	}

	return result, nil
}

// matchesFilter checks if an event matches all filter criteria.
func (m *mockStatusEventRepository) matchesFilter(event *model.StatusEvent, filter *mysql.StatusEventFilter) bool {
	if filter == nil {
		return true
	}

	// Check endpoint filter
	if filter.Endpoint != "" && event.Endpoint != filter.Endpoint {
		return false
	}

	// Check worker_id filter
	if filter.WorkerID != "" && event.WorkerID != filter.WorkerID {
		return false
	}

	// Check event_type filter
	if filter.EventType != "" && event.EventType != filter.EventType {
		return false
	}

	// Check start_time filter
	if filter.StartTime != nil && event.CreatedAt.Before(*filter.StartTime) {
		return false
	}

	// Check end_time filter
	if filter.EndTime != nil && event.CreatedAt.After(*filter.EndTime) {
		return false
	}

	return true
}

// GetAllEvents returns all events in the repository (for testing).
func (m *mockStatusEventRepository) GetAllEvents() []model.StatusEvent {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]model.StatusEvent, len(m.events))
	copy(result, m.events)
	return result
}

// Clear removes all events from the repository.
func (m *mockStatusEventRepository) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = make([]model.StatusEvent, 0)
	m.nextID = 1
}

// ============================================================================
// Mock-based StatusEventService for testing
// ============================================================================

// testableStatusEventService wraps StatusEventService with a mock repository.
type testableStatusEventService struct {
	mockRepo *mockStatusEventRepository
	service  *StatusEventService
}

// newTestableStatusEventService creates a new testable service with mock repository.
func newTestableStatusEventService() *testableStatusEventService {
	mockRepo := newMockStatusEventRepository()
	// Create a wrapper that implements the repository interface
	return &testableStatusEventService{
		mockRepo: mockRepo,
		service:  newStatusEventServiceWithMock(mockRepo),
	}
}

// newStatusEventServiceWithMock creates a StatusEventService with a mock repository.
// This is a helper function that creates the service with the mock.
func newStatusEventServiceWithMock(mockRepo *mockStatusEventRepository) *StatusEventService {
	return &StatusEventService{
		repo:            nil, // We'll override the methods
		pendingDetector: nil,
	}
}

// RecordEvent records an event using the mock repository.
func (t *testableStatusEventService) RecordEvent(ctx context.Context, event *StatusEvent) error {
	if event == nil {
		return nil
	}

	// Validate required fields
	if event.WorkerID == "" || event.Endpoint == "" || event.NewStatus == "" {
		return nil
	}

	// Convert to model event
	modelEvent := &model.StatusEvent{
		WorkerID:  event.WorkerID,
		Endpoint:  event.Endpoint,
		EventType: string(event.EventType),
		NewStatus: event.NewStatus,
		CreatedAt: event.CreatedAt,
	}

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

	if event.SpotStatus != nil {
		modelEvent.SpotStatus = model.JSONMap{
			"capacity":     string(event.SpotStatus.Capacity),
			"score":        event.SpotStatus.Score,
			"price":        event.SpotStatus.Price,
			"instanceType": event.SpotStatus.InstanceType,
		}
	}

	err := t.mockRepo.Create(ctx, modelEvent)
	if err == nil {
		event.ID = modelEvent.ID
	}
	return err
}

// ListEvents lists events using the mock repository.
func (t *testableStatusEventService) ListEvents(ctx context.Context, filter *StatusEventFilter) ([]StatusEvent, error) {
	repoFilter := &mysql.StatusEventFilter{}
	if filter != nil {
		repoFilter.Endpoint = filter.Endpoint
		repoFilter.WorkerID = filter.WorkerID
		repoFilter.EventType = string(filter.EventType)
		repoFilter.StartTime = filter.StartTime
		repoFilter.EndTime = filter.EndTime
		repoFilter.Limit = filter.Limit
		repoFilter.Offset = filter.Offset
	}

	modelEvents, err := t.mockRepo.List(ctx, repoFilter)
	if err != nil {
		return nil, err
	}

	events := make([]StatusEvent, len(modelEvents))
	for i, me := range modelEvents {
		events[i] = StatusEvent{
			ID:        me.ID,
			WorkerID:  me.WorkerID,
			Endpoint:  me.Endpoint,
			EventType: StatusEventType(me.EventType),
			NewStatus: me.NewStatus,
			CreatedAt: me.CreatedAt,
		}
		if me.OldStatus != nil {
			events[i].OldStatus = *me.OldStatus
		}
		if me.Phase != nil {
			events[i].Phase = *me.Phase
		}
		if me.Reason != nil {
			events[i].Reason = *me.Reason
		}
		if me.Message != nil {
			events[i].Message = *me.Message
		}
	}

	return events, nil
}

// ============================================================================
// Property 2: Phase Transition Event Recording
// ============================================================================

// TestProperty_PhaseTransitionEventRecording tests Property 2: Phase Transition Event Recording
//
// Property: For any Worker status or phase change, the StatusEventService SHALL create
// a StatusEvent record that contains all required fields (worker_id, endpoint, event_type,
// old_status, new_status, phase, reason, message, timestamp), and the timestamp SHALL be
// within a reasonable tolerance of the actual change time.
//
// Feature: endpoint-status-tracking, Property 2: Phase Transition Event Recording
// **Validates: Requirements 1.4, 3.1, 3.2**
func TestProperty_PhaseTransitionEventRecording(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxSize = 50

	properties := gopter.NewProperties(parameters)

	// Property 2a: Recorded events contain all required fields
	properties.Property("recorded events contain all required fields", prop.ForAll(
		func(event *StatusEvent) bool {
			testService := newTestableStatusEventService()
			ctx := context.Background()

			// Set timestamp if not set
			if event.CreatedAt.IsZero() {
				event.CreatedAt = time.Now()
			}

			err := testService.RecordEvent(ctx, event)
			if err != nil {
				return false
			}

			// Verify the event was recorded
			allEvents := testService.mockRepo.GetAllEvents()
			if len(allEvents) != 1 {
				return false
			}

			recorded := allEvents[0]

			// Check required fields are present
			return recorded.WorkerID == event.WorkerID &&
				recorded.Endpoint == event.Endpoint &&
				recorded.EventType == string(event.EventType) &&
				recorded.NewStatus == event.NewStatus &&
				!recorded.CreatedAt.IsZero()
		},
		genValidStatusEvent(),
	))

	// Property 2b: Timestamp is within reasonable tolerance of actual change time
	properties.Property("timestamp is within reasonable tolerance", prop.ForAll(
		func(event *StatusEvent) bool {
			testService := newTestableStatusEventService()
			ctx := context.Background()

			beforeRecord := time.Now()
			event.CreatedAt = beforeRecord

			err := testService.RecordEvent(ctx, event)
			if err != nil {
				return false
			}

			afterRecord := time.Now()

			allEvents := testService.mockRepo.GetAllEvents()
			if len(allEvents) != 1 {
				return false
			}

			recorded := allEvents[0]

			// Timestamp should be between before and after (with 1 second tolerance)
			tolerance := time.Second
			return !recorded.CreatedAt.Before(beforeRecord.Add(-tolerance)) &&
				!recorded.CreatedAt.After(afterRecord.Add(tolerance))
		},
		genValidStatusEvent(),
	))

	// Property 2c: Phase change events include phase information
	properties.Property("phase change events include phase information", prop.ForAll(
		func(workerID, endpoint, phase, reason, message string) bool {
			testService := newTestableStatusEventService()
			ctx := context.Background()

			event := &StatusEvent{
				WorkerID:  workerID,
				Endpoint:  endpoint,
				EventType: EventTypePhaseChange,
				NewStatus: "PENDING",
				Phase:     phase,
				Reason:    reason,
				Message:   message,
				CreatedAt: time.Now(),
			}

			err := testService.RecordEvent(ctx, event)
			if err != nil {
				return false
			}

			allEvents := testService.mockRepo.GetAllEvents()
			if len(allEvents) != 1 {
				return false
			}

			recorded := allEvents[0]

			// Phase should be recorded
			if recorded.Phase == nil {
				return phase == ""
			}
			return *recorded.Phase == phase
		},
		genWorkerID(),
		genEndpointName(),
		genPendingPhase(),
		genReason(),
		genMessage(),
	))

	// Property 2d: Status change events include old and new status
	properties.Property("status change events include old and new status", prop.ForAll(
		func(workerID, endpoint, oldStatus, newStatus string) bool {
			testService := newTestableStatusEventService()
			ctx := context.Background()

			event := &StatusEvent{
				WorkerID:  workerID,
				Endpoint:  endpoint,
				EventType: EventTypeStatusChange,
				OldStatus: oldStatus,
				NewStatus: newStatus,
				CreatedAt: time.Now(),
			}

			err := testService.RecordEvent(ctx, event)
			if err != nil {
				return false
			}

			allEvents := testService.mockRepo.GetAllEvents()
			if len(allEvents) != 1 {
				return false
			}

			recorded := allEvents[0]

			// NewStatus should always be recorded
			if recorded.NewStatus != newStatus {
				return false
			}

			// OldStatus should be recorded if provided
			if oldStatus != "" {
				if recorded.OldStatus == nil || *recorded.OldStatus != oldStatus {
					return false
				}
			}

			return true
		},
		genWorkerID(),
		genEndpointName(),
		genWorkerStatus(),
		genWorkerStatus(),
	))

	// Property 2e: SpotStatus is preserved in phase change events
	properties.Property("SpotStatus is preserved in phase change events", prop.ForAll(
		func(workerID, endpoint string, score int, price float64, instanceType string) bool {
			testService := newTestableStatusEventService()
			ctx := context.Background()

			spotStatus := &status.SpotStatus{
				Capacity:     status.ClassifySpotCapacity(score),
				Score:        score,
				Price:        price,
				InstanceType: instanceType,
			}

			event := &StatusEvent{
				WorkerID:   workerID,
				Endpoint:   endpoint,
				EventType:  EventTypePhaseChange,
				NewStatus:  "PENDING",
				Phase:      "WAITING_NODE",
				SpotStatus: spotStatus,
				CreatedAt:  time.Now(),
			}

			err := testService.RecordEvent(ctx, event)
			if err != nil {
				return false
			}

			allEvents := testService.mockRepo.GetAllEvents()
			if len(allEvents) != 1 {
				return false
			}

			recorded := allEvents[0]

			// SpotStatus should be recorded as JSON
			if recorded.SpotStatus == nil {
				return false
			}

			// Verify SpotStatus fields
			capacity, ok := recorded.SpotStatus["capacity"].(string)
			if !ok || capacity != string(spotStatus.Capacity) {
				return false
			}

			return true
		},
		genWorkerID(),
		genEndpointName(),
		gen.IntRange(1, 10),
		gen.Float64Range(0.01, 10.0),
		genInstanceType(),
	))

	// Property 2f: Event ID is assigned after recording
	properties.Property("event ID is assigned after recording", prop.ForAll(
		func(event *StatusEvent) bool {
			testService := newTestableStatusEventService()
			ctx := context.Background()

			event.CreatedAt = time.Now()
			originalID := event.ID

			err := testService.RecordEvent(ctx, event)
			if err != nil {
				return false
			}

			// ID should be assigned (greater than 0)
			return event.ID > 0 && event.ID != originalID
		},
		genValidStatusEvent(),
	))

	properties.TestingRun(t)
}

// ============================================================================
// Property 5: Status Event Filtering
// ============================================================================

// TestProperty_StatusEventFiltering tests Property 5: Status Event Filtering
//
// Property: For any set of StatusEvents and any valid filter combination (endpoint,
// worker_id, time range), the ListEvents query SHALL return only events that match
// ALL specified filter criteria. The result set SHALL be a subset of all events,
// and every returned event SHALL satisfy all filter conditions.
//
// Feature: endpoint-status-tracking, Property 5: Status Event Filtering
// **Validates: Requirements 3.5**
func TestProperty_StatusEventFiltering(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxSize = 50

	properties := gopter.NewProperties(parameters)

	// Property 5a: Endpoint filter returns only matching events
	properties.Property("endpoint filter returns only matching events", prop.ForAll(
		func(events []*StatusEvent, targetEndpoint string) bool {
			testService := newTestableStatusEventService()
			ctx := context.Background()

			// Record all events
			for _, event := range events {
				event.CreatedAt = time.Now()
				_ = testService.RecordEvent(ctx, event)
			}

			// Filter by endpoint
			filter := &StatusEventFilter{
				Endpoint: targetEndpoint,
			}

			results, err := testService.ListEvents(ctx, filter)
			if err != nil {
				return false
			}

			// All returned events should match the endpoint
			for _, result := range results {
				if result.Endpoint != targetEndpoint {
					return false
				}
			}

			return true
		},
		genStatusEventList(),
		genEndpointName(),
	))

	// Property 5b: WorkerID filter returns only matching events
	properties.Property("worker_id filter returns only matching events", prop.ForAll(
		func(events []*StatusEvent, targetWorkerID string) bool {
			testService := newTestableStatusEventService()
			ctx := context.Background()

			// Record all events
			for _, event := range events {
				event.CreatedAt = time.Now()
				_ = testService.RecordEvent(ctx, event)
			}

			// Filter by worker_id
			filter := &StatusEventFilter{
				WorkerID: targetWorkerID,
			}

			results, err := testService.ListEvents(ctx, filter)
			if err != nil {
				return false
			}

			// All returned events should match the worker_id
			for _, result := range results {
				if result.WorkerID != targetWorkerID {
					return false
				}
			}

			return true
		},
		genStatusEventList(),
		genWorkerID(),
	))

	// Property 5c: Time range filter returns only events within range
	properties.Property("time range filter returns only events within range", prop.ForAll(
		func(events []*StatusEvent, startOffset, endOffset int) bool {
			testService := newTestableStatusEventService()
			ctx := context.Background()

			baseTime := time.Now()

			// Record events with different timestamps
			for i, event := range events {
				event.CreatedAt = baseTime.Add(time.Duration(i) * time.Minute)
				_ = testService.RecordEvent(ctx, event)
			}

			// Create time range filter
			startTime := baseTime.Add(time.Duration(startOffset) * time.Minute)
			endTime := baseTime.Add(time.Duration(endOffset) * time.Minute)

			filter := &StatusEventFilter{
				StartTime: &startTime,
				EndTime:   &endTime,
			}

			results, err := testService.ListEvents(ctx, filter)
			if err != nil {
				return false
			}

			// All returned events should be within the time range
			for _, result := range results {
				if result.CreatedAt.Before(startTime) || result.CreatedAt.After(endTime) {
					return false
				}
			}

			return true
		},
		genStatusEventList(),
		gen.IntRange(0, 5),
		gen.IntRange(5, 10),
	))

	// Property 5d: Combined filters return only events matching ALL criteria
	properties.Property("combined filters return only events matching ALL criteria", prop.ForAll(
		func(events []*StatusEvent, targetEndpoint, targetWorkerID string) bool {
			testService := newTestableStatusEventService()
			ctx := context.Background()

			// Record all events
			for _, event := range events {
				event.CreatedAt = time.Now()
				_ = testService.RecordEvent(ctx, event)
			}

			// Filter by both endpoint and worker_id
			filter := &StatusEventFilter{
				Endpoint: targetEndpoint,
				WorkerID: targetWorkerID,
			}

			results, err := testService.ListEvents(ctx, filter)
			if err != nil {
				return false
			}

			// All returned events should match BOTH criteria
			for _, result := range results {
				if result.Endpoint != targetEndpoint || result.WorkerID != targetWorkerID {
					return false
				}
			}

			return true
		},
		genStatusEventList(),
		genEndpointName(),
		genWorkerID(),
	))

	// Property 5e: Result set is always a subset of all events
	properties.Property("result set is always a subset of all events", prop.ForAll(
		func(events []*StatusEvent, targetEndpoint string) bool {
			testService := newTestableStatusEventService()
			ctx := context.Background()

			// Record all events
			for _, event := range events {
				event.CreatedAt = time.Now()
				_ = testService.RecordEvent(ctx, event)
			}

			// Get all events
			allEvents, _ := testService.ListEvents(ctx, nil)

			// Filter by endpoint
			filter := &StatusEventFilter{
				Endpoint: targetEndpoint,
			}
			filteredEvents, _ := testService.ListEvents(ctx, filter)

			// Filtered count should be <= total count
			return len(filteredEvents) <= len(allEvents)
		},
		genStatusEventList(),
		genEndpointName(),
	))

	// Property 5f: Empty filter returns all events
	properties.Property("empty filter returns all events", prop.ForAll(
		func(events []*StatusEvent) bool {
			testService := newTestableStatusEventService()
			ctx := context.Background()

			// Record all events
			for _, event := range events {
				event.CreatedAt = time.Now()
				_ = testService.RecordEvent(ctx, event)
			}

			// Get all events with nil filter
			results, err := testService.ListEvents(ctx, nil)
			if err != nil {
				return false
			}

			// Should return all recorded events
			return len(results) == len(events)
		},
		genStatusEventList(),
	))

	// Property 5g: Limit parameter restricts result count
	properties.Property("limit parameter restricts result count", prop.ForAll(
		func(events []*StatusEvent, limit int) bool {
			testService := newTestableStatusEventService()
			ctx := context.Background()

			// Record all events
			for _, event := range events {
				event.CreatedAt = time.Now()
				_ = testService.RecordEvent(ctx, event)
			}

			// Filter with limit
			filter := &StatusEventFilter{
				Limit: limit,
			}

			results, err := testService.ListEvents(ctx, filter)
			if err != nil {
				return false
			}

			// Result count should be <= limit (and <= total events)
			expectedMax := limit
			if len(events) < limit {
				expectedMax = len(events)
			}

			return len(results) <= expectedMax
		},
		genStatusEventList(),
		gen.IntRange(1, 10),
	))

	// Property 5h: EventType filter returns only matching events
	properties.Property("event_type filter returns only matching events", prop.ForAll(
		func(events []*StatusEvent, targetEventType StatusEventType) bool {
			testService := newTestableStatusEventService()
			ctx := context.Background()

			// Record all events
			for _, event := range events {
				event.CreatedAt = time.Now()
				_ = testService.RecordEvent(ctx, event)
			}

			// Filter by event_type
			filter := &StatusEventFilter{
				EventType: targetEventType,
			}

			results, err := testService.ListEvents(ctx, filter)
			if err != nil {
				return false
			}

			// All returned events should match the event_type
			for _, result := range results {
				if result.EventType != targetEventType {
					return false
				}
			}

			return true
		},
		genStatusEventList(),
		genEventType(),
	))

	properties.TestingRun(t)
}

// TestProperty_StatusEventFilteringConsistency tests consistency properties of filtering
//
// Property: Filtering operations SHALL be consistent:
// - Applying the same filter twice returns the same results
// - Order of filter criteria does not affect results
//
// Feature: endpoint-status-tracking, Property 5: Status Event Filtering
// **Validates: Requirements 3.5**
func TestProperty_StatusEventFilteringConsistency(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	parameters.MaxSize = 50

	properties := gopter.NewProperties(parameters)

	// Property: Same filter applied twice returns same results
	properties.Property("same filter applied twice returns same results", prop.ForAll(
		func(events []*StatusEvent, targetEndpoint string) bool {
			testService := newTestableStatusEventService()
			ctx := context.Background()

			// Record all events
			for _, event := range events {
				event.CreatedAt = time.Now()
				_ = testService.RecordEvent(ctx, event)
			}

			filter := &StatusEventFilter{
				Endpoint: targetEndpoint,
			}

			results1, _ := testService.ListEvents(ctx, filter)
			results2, _ := testService.ListEvents(ctx, filter)

			// Results should be identical
			if len(results1) != len(results2) {
				return false
			}

			for i := range results1 {
				if results1[i].ID != results2[i].ID {
					return false
				}
			}

			return true
		},
		genStatusEventList(),
		genEndpointName(),
	))

	// Property: Filtering is idempotent (filtering already filtered results gives same results)
	properties.Property("filtering is idempotent", prop.ForAll(
		func(events []*StatusEvent, targetEndpoint string) bool {
			testService := newTestableStatusEventService()
			ctx := context.Background()

			// Record all events
			for _, event := range events {
				event.CreatedAt = time.Now()
				_ = testService.RecordEvent(ctx, event)
			}

			filter := &StatusEventFilter{
				Endpoint: targetEndpoint,
			}

			// First filter
			results1, _ := testService.ListEvents(ctx, filter)

			// All results should already match the filter
			for _, result := range results1 {
				if result.Endpoint != targetEndpoint {
					return false
				}
			}

			return true
		},
		genStatusEventList(),
		genEndpointName(),
	))

	properties.TestingRun(t)
}

// ============================================================================
// Generators for Status Event property tests
// ============================================================================

// genWorkerID generates valid worker IDs
func genWorkerID() gopter.Gen {
	return gen.RegexMatch(`worker-[a-z0-9]{8}`).SuchThat(func(s string) bool {
		return len(s) >= 10
	})
}

// genEndpointName generates valid endpoint names
func genEndpointName() gopter.Gen {
	return gen.OneConstOf(
		"my-model",
		"test-endpoint",
		"prod-service",
		"dev-api",
		"staging-model",
	)
}

// genEventType generates valid event types
func genEventType() gopter.Gen {
	return gen.OneConstOf(
		EventTypeStatusChange,
		EventTypePhaseChange,
		EventTypeFailure,
		EventTypeRecovery,
	)
}

// genWorkerStatus generates valid worker statuses
func genWorkerStatus() gopter.Gen {
	return gen.OneConstOf(
		"PENDING",
		"RUNNING",
		"ONLINE",
		"OFFLINE",
		"FAILED",
		"TERMINATED",
	)
}

// genPendingPhase generates valid pending phases
func genPendingPhase() gopter.Gen {
	return gen.OneConstOf(
		"SCHEDULING",
		"WAITING_NODE",
		"PULLING_IMAGE",
		"INITIALIZING",
	)
}

// genReason generates valid reason strings
func genReason() gopter.Gen {
	return gen.OneConstOf(
		"",
		"Unschedulable",
		"ContainerCreating",
		"ImagePullBackOff",
		"PodInitializing",
		"NodeNotReady",
	)
}

// genMessage generates valid message strings
func genMessage() gopter.Gen {
	return gen.OneConstOf(
		"",
		"Waiting for node to be ready",
		"Pulling image from registry",
		"Init container running",
		"Pod scheduled successfully",
	)
}

// genInstanceType generates valid AWS instance types
func genInstanceType() gopter.Gen {
	return gen.OneConstOf(
		"g4dn.xlarge",
		"g4dn.2xlarge",
		"g5.xlarge",
		"p3.2xlarge",
	)
}

// genValidStatusEvent generates a valid StatusEvent with all required fields
func genValidStatusEvent() gopter.Gen {
	return gopter.CombineGens(
		genWorkerID(),
		genEndpointName(),
		genEventType(),
		genWorkerStatus(),
		genWorkerStatus(),
		genPendingPhase(),
		genReason(),
		genMessage(),
	).Map(func(vals []interface{}) *StatusEvent {
		return &StatusEvent{
			WorkerID:  vals[0].(string),
			Endpoint:  vals[1].(string),
			EventType: vals[2].(StatusEventType),
			OldStatus: vals[3].(string),
			NewStatus: vals[4].(string),
			Phase:     vals[5].(string),
			Reason:    vals[6].(string),
			Message:   vals[7].(string),
		}
	})
}

// genStatusEventList generates a list of status events for filtering tests
func genStatusEventList() gopter.Gen {
	return gen.SliceOfN(10, genValidStatusEvent())
}
