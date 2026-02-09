// Package novita provides Novita deployment provider implementation.
// This file implements the Novita Worker Status Monitor for tracking worker failures.
package novita

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"waverless/pkg/interfaces"
	"waverless/pkg/logger"
	"waverless/pkg/status"
	"waverless/pkg/store/mysql"
)

// NovitaWorkerStatusMonitor monitors Novita worker status changes and detects failures.
// It implements the WorkerStatusWatcher interface from pkg/interfaces/image_validation.go.
//
// Unlike K8s which uses informers/webhooks, Novita doesn't support webhooks,
// so this monitor uses a polling mechanism to detect status changes.
//
type NovitaWorkerStatusMonitor struct {
	client       clientInterface
	workerRepo   *mysql.WorkerRepository
	sanitizer    *status.StatusSanitizer
	pollInterval time.Duration

	// workerStates tracks the last known state of each worker
	// key: workerID, value: *monitorWorkerState
	workerStates sync.Map
}

// monitorWorkerState stores the last known state of a worker for the status monitor
type monitorWorkerState struct {
	State     string    // Novita state: "serving", "stopped", "failed", etc.
	Error     string    // Error code if any
	Message   string    // State message
	Healthy   bool      // Health status
	UpdatedAt time.Time // Last update time
}

// DefaultPollInterval is the default interval for polling Novita API
const DefaultPollInterval = 30 * time.Second

// NewNovitaWorkerStatusMonitor creates a new Novita worker status monitor.
//
// Parameters:
//   - client: The Novita API client for querying endpoint/worker status
//   - workerRepo: The worker repository for updating worker failure information
//
// Returns:
//   - A new NovitaWorkerStatusMonitor instance
func NewNovitaWorkerStatusMonitor(client clientInterface, workerRepo *mysql.WorkerRepository) *NovitaWorkerStatusMonitor {
	return &NovitaWorkerStatusMonitor{
		client:       client,
		workerRepo:   workerRepo,
		sanitizer:    status.NewStatusSanitizer(),
		pollInterval: DefaultPollInterval,
	}
}

// NewNovitaWorkerStatusMonitorWithInterval creates a new Novita worker status monitor with custom poll interval.
//
// Parameters:
//   - client: The Novita API client for querying endpoint/worker status
//   - workerRepo: The worker repository for updating worker failure information
//   - pollInterval: The interval between polling cycles
//
// Returns:
//   - A new NovitaWorkerStatusMonitor instance
func NewNovitaWorkerStatusMonitorWithInterval(client clientInterface, workerRepo *mysql.WorkerRepository, pollInterval time.Duration) *NovitaWorkerStatusMonitor {
	if pollInterval <= 0 {
		pollInterval = DefaultPollInterval
	}
	return &NovitaWorkerStatusMonitor{
		client:       client,
		workerRepo:   workerRepo,
		sanitizer:    status.NewStatusSanitizer(),
		pollInterval: pollInterval,
	}
}

// WatchWorkerStatus watches worker status changes and calls callback on failure.
// This method implements the WorkerStatusWatcher interface.
//
// It uses a polling mechanism to periodically query Novita API for endpoint/worker status.
// When a worker enters a failed state, it converts the Novita-specific status to a
// generic WorkerFailureInfo and invokes the callback.
//
// The method blocks until the context is cancelled.
//
// Parameters:
//   - ctx: Context for cancellation
//   - callback: Function to call when a worker enters a failed state
//
// Returns:
//   - error if polling fails, nil when context is cancelled
//
func (m *NovitaWorkerStatusMonitor) WatchWorkerStatus(ctx context.Context, callback interfaces.WorkerStatusCallback) error {
	if callback == nil {
		return nil
	}

	logger.InfoCtx(ctx, "Novita worker status monitor started (poll interval: %v)", m.pollInterval)

	ticker := time.NewTicker(m.pollInterval)
	defer ticker.Stop()

	// Do an initial poll immediately
	m.pollWorkerStates(ctx, callback)

	for {
		select {
		case <-ctx.Done():
			logger.InfoCtx(ctx, "Novita worker status monitor stopped")
			return ctx.Err()
		case <-ticker.C:
			m.pollWorkerStates(ctx, callback)
		}
	}
}

// pollWorkerStates polls all endpoints and their workers for status changes.
func (m *NovitaWorkerStatusMonitor) pollWorkerStates(ctx context.Context, callback interfaces.WorkerStatusCallback) {
	// List all endpoints
	resp, err := m.client.ListEndpoints(ctx)
	if err != nil {
		logger.ErrorCtx(ctx, "Failed to list Novita endpoints for status monitoring: %v", err)
		return
	}

	// Process each endpoint and its workers
	for _, endpoint := range resp.Endpoints {
		endpointName := endpoint.Name

		// Check endpoint-level failure first
		if m.isEndpointFailed(&endpoint.State) {
			m.handleEndpointFailure(ctx, endpointName, &endpoint.State, callback)
		}

		// Check each worker
		for _, worker := range endpoint.Workers {
			m.checkWorkerState(ctx, worker.ID, endpointName, &worker, callback)
		}
	}
}

// isEndpointFailed checks if the endpoint state indicates a failure.
func (m *NovitaWorkerStatusMonitor) isEndpointFailed(state *StateInfo) bool {
	if state == nil {
		return false
	}

	// Check for failed state
	stateLower := strings.ToLower(state.State)
	return stateLower == "failed" || stateLower == "error" ||
		state.Error != "" || strings.Contains(stateLower, "fail")
}

// handleEndpointFailure handles endpoint-level failures.
func (m *NovitaWorkerStatusMonitor) handleEndpointFailure(ctx context.Context, endpointName string, state *StateInfo, callback interfaces.WorkerStatusCallback) {
	// Create a synthetic worker ID for endpoint-level failures
	workerID := "endpoint-" + endpointName

	// Check if we've already reported this failure
	if prevState, ok := m.workerStates.Load(workerID); ok {
		prev := prevState.(*monitorWorkerState)
		if prev.State == state.State && prev.Error == state.Error && prev.Message == state.Message {
			return // No change, skip
		}
	}

	// Classify the failure
	failureType := m.ClassifyNovitaFailure(state.State, state.Error, state.Message)

	// Create failure info
	failureInfo := m.createFailureInfo(failureType, state.State, state.Error, state.Message)

	logger.InfoCtx(ctx, "🚨 Endpoint failure detected: endpoint=%s, type=%s, state=%s, error=%s",
		endpointName, failureInfo.Type, state.State, state.Error)

	// Update state cache
	m.workerStates.Store(workerID, &monitorWorkerState{
		State:     state.State,
		Error:     state.Error,
		Message:   state.Message,
		UpdatedAt: time.Now(),
	})

	// Invoke callback
	callback(workerID, endpointName, failureInfo)
}

// checkWorkerState checks a single worker's state and triggers callback if failed.
func (m *NovitaWorkerStatusMonitor) checkWorkerState(ctx context.Context, workerID, endpointName string, worker *WorkerInfo, callback interfaces.WorkerStatusCallback) {
	if worker == nil {
		return
	}

	// Get previous state
	prevStateInterface, hasPrevState := m.workerStates.Load(workerID)

	// Check if this is a failure state
	if !m.isWorkerFailed(worker) {
		// Worker is healthy, clear any previous failure state
		if hasPrevState {
			prev := prevStateInterface.(*monitorWorkerState)
			if prev.State == "failed" || prev.Error != "" {
				// Worker recovered, clear failure in database
				if m.workerRepo != nil {
					if err := m.workerRepo.ClearWorkerFailure(ctx, workerID); err != nil {
						logger.WarnCtx(ctx, "Failed to clear worker failure: worker=%s, error=%v", workerID, err)
					}
				}
			}
		}

		// Update state cache
		m.workerStates.Store(workerID, &monitorWorkerState{
			State:     worker.State.State,
			Error:     worker.State.Error,
			Message:   worker.State.Message,
			Healthy:   worker.Healthy,
			UpdatedAt: time.Now(),
		})
		return
	}

	// Worker is in failed state
	// Check if state has changed
	if hasPrevState {
		prev := prevStateInterface.(*monitorWorkerState)
		if prev.State == worker.State.State &&
			prev.Error == worker.State.Error &&
			prev.Message == worker.State.Message {
			return // No change, skip
		}
	}

	// Classify the failure
	failureType := m.ClassifyNovitaFailure(worker.State.State, worker.State.Error, worker.State.Message)

	// Create failure info
	failureInfo := m.createFailureInfo(failureType, worker.State.State, worker.State.Error, worker.State.Message)

	logger.InfoCtx(ctx, "🚨 Worker failure detected: worker=%s, endpoint=%s, type=%s, state=%s, error=%s",
		workerID, endpointName, failureInfo.Type, worker.State.State, worker.State.Error)

	// Update worker record in database
	if m.workerRepo != nil {
		if err := m.updateWorkerFailure(ctx, workerID, endpointName, failureInfo); err != nil {
			logger.ErrorCtx(ctx, "Failed to update worker failure: worker=%s, error=%v", workerID, err)
		}
	}

	// Update state cache
	m.workerStates.Store(workerID, &monitorWorkerState{
		State:     worker.State.State,
		Error:     worker.State.Error,
		Message:   worker.State.Message,
		Healthy:   worker.Healthy,
		UpdatedAt: time.Now(),
	})

	// Invoke callback
	callback(workerID, endpointName, failureInfo)
}

// isWorkerFailed checks if the worker state indicates a failure.
func (m *NovitaWorkerStatusMonitor) isWorkerFailed(worker *WorkerInfo) bool {
	if worker == nil {
		return false
	}

	// Check state
	stateLower := strings.ToLower(worker.State.State)
	if stateLower == "failed" || stateLower == "error" || strings.Contains(stateLower, "fail") {
		return true
	}

	// Check error field
	if worker.State.Error != "" {
		return true
	}

	// Check health status (unhealthy worker with error message)
	if !worker.Healthy && worker.State.Message != "" && strings.Contains(strings.ToLower(worker.State.Message), "error") {
		return true
	}

	return false
}

// createFailureInfo creates a WorkerFailureInfo from Novita state information.
func (m *NovitaWorkerStatusMonitor) createFailureInfo(failureType interfaces.FailureType, state, errorCode, message string) *interfaces.WorkerFailureInfo {
	// Build reason from state and error code
	reason := state
	if errorCode != "" {
		reason = errorCode
	}

	// Sanitize the message
	sanitizedMsg := ""
	if m.sanitizer != nil {
		sanitized := m.sanitizer.Sanitize(failureType, reason, message)
		if sanitized != nil {
			sanitizedMsg = sanitized.UserMessage
			if sanitized.Suggestion != "" {
				sanitizedMsg += ". " + sanitized.Suggestion
			}
		}
	}

	return &interfaces.WorkerFailureInfo{
		Type:         failureType,
		Reason:       reason,
		Message:      message,
		SanitizedMsg: sanitizedMsg,
		OccurredAt:   time.Now(), // Use local time - GORM will convert to UTC for storage
	}
}

// ClassifyNovitaFailure converts Novita status to generic FailureType.
// This method maps Novita-specific error states to the generic failure types
// defined in pkg/interfaces/image_validation.go.
//
// Parameters:
//   - state: The Novita state string (e.g., "failed", "serving")
//   - errorCode: The Novita error code if any
//   - message: The Novita message string (used for additional context)
//
// Returns:
//   - The corresponding FailureType
//
func (m *NovitaWorkerStatusMonitor) ClassifyNovitaFailure(state, errorCode, message string) interfaces.FailureType {
	// Normalize for comparison
	stateLower := strings.ToLower(state)
	errorLower := strings.ToLower(errorCode)
	messageLower := strings.ToLower(message)

	// Check for image-related failures
	if containsAny(errorLower, "image", "pull", "registry", "manifest", "repository") ||
		containsAny(messageLower, "image", "pull", "registry", "manifest", "repository", "not found") {
		return interfaces.FailureTypeImagePull
	}

	// Check for container crash failures
	if containsAny(errorLower, "crash", "exit", "oom", "killed", "container") ||
		containsAny(messageLower, "crash", "exit", "oom", "killed", "container error") {
		return interfaces.FailureTypeContainerCrash
	}

	// Check for resource limit failures
	if containsAny(errorLower, "resource", "memory", "cpu", "gpu", "quota", "limit", "insufficient") ||
		containsAny(messageLower, "resource", "memory", "cpu", "gpu", "quota", "limit", "insufficient", "unavailable") {
		return interfaces.FailureTypeResourceLimit
	}

	// Check for timeout failures
	if containsAny(errorLower, "timeout", "deadline") ||
		containsAny(messageLower, "timeout", "deadline", "timed out") {
		return interfaces.FailureTypeTimeout
	}

	// Check state for generic failure indicators
	if stateLower == "failed" || stateLower == "error" {
		// Try to infer from message
		if containsAny(messageLower, "image", "pull") {
			return interfaces.FailureTypeImagePull
		}
		if containsAny(messageLower, "crash", "exit") {
			return interfaces.FailureTypeContainerCrash
		}
		if containsAny(messageLower, "resource", "memory", "gpu") {
			return interfaces.FailureTypeResourceLimit
		}
	}

	return interfaces.FailureTypeUnknown
}

// containsAny checks if the string contains any of the given substrings.
func containsAny(s string, substrs ...string) bool {
	for _, substr := range substrs {
		if strings.Contains(s, substr) {
			return true
		}
	}
	return false
}

// updateWorkerFailure updates the worker record with failure information.
// This method persists the failure details to the database for later retrieval.
//
// Parameters:
//   - ctx: Context for database operations
//   - workerID: The Novita worker ID
//   - endpoint: The endpoint name
//   - info: The failure information to store
//
// Returns:
//   - error if the database update fails
//
func (m *NovitaWorkerStatusMonitor) updateWorkerFailure(ctx context.Context, workerID, endpoint string, info *interfaces.WorkerFailureInfo) error {
	if m.workerRepo == nil || info == nil {
		return nil
	}

	// Build failure details JSON
	details := map[string]any{
		"type":         string(info.Type),
		"reason":       info.Reason,
		"message":      info.Message,
		"sanitizedMsg": info.SanitizedMsg,
		"occurredAt":   info.OccurredAt.Format(time.RFC3339),
		"provider":     "novita",
	}
	detailsJSON, err := json.Marshal(details)
	if err != nil {
		logger.WarnCtx(ctx, "Failed to marshal failure details: %v", err)
		detailsJSON = []byte("{}")
	}

	// Update worker record using the repository
	return m.workerRepo.UpdateWorkerFailure(ctx, workerID, string(info.Type), info.SanitizedMsg, string(detailsJSON), info.OccurredAt)
}

// GetSanitizer returns the status sanitizer.
// This is useful for sanitizing error messages externally.
func (m *NovitaWorkerStatusMonitor) GetSanitizer() *status.StatusSanitizer {
	return m.sanitizer
}

// SetPollInterval sets the polling interval.
// This is useful for testing with shorter intervals.
func (m *NovitaWorkerStatusMonitor) SetPollInterval(interval time.Duration) {
	if interval > 0 {
		m.pollInterval = interval
	}
}

// GetPollInterval returns the current polling interval.
func (m *NovitaWorkerStatusMonitor) GetPollInterval() time.Duration {
	return m.pollInterval
}

// DetectFailure detects if a worker is in a failed state from PodInfo.
// This method is used by the lifecycle manager to detect failures from status change events.
// Returns nil if the worker is not in a failed state.
func (m *NovitaWorkerStatusMonitor) DetectFailure(info *interfaces.PodInfo) *interfaces.WorkerFailureInfo {
	if info == nil {
		return nil
	}

	// Check if this is a failure state based on Phase/Status/Reason
	stateLower := strings.ToLower(info.Phase)
	reasonLower := strings.ToLower(info.Reason)
	messageLower := strings.ToLower(info.Message)

	// Check for failure indicators
	isFailed := stateLower == "failed" || stateLower == "error" ||
		strings.Contains(stateLower, "fail") ||
		reasonLower != "" && reasonLower != "ready" ||
		strings.Contains(messageLower, "error") ||
		strings.Contains(messageLower, "fail")

	if !isFailed {
		return nil
	}

	// Classify the failure
	failureType := m.ClassifyNovitaFailure(info.Phase, info.Reason, info.Message)

	// Create failure info
	return m.createFailureInfo(failureType, info.Phase, info.Reason, info.Message)
}

// ClearWorkerStates clears all cached worker states.
// This is useful for testing.
func (m *NovitaWorkerStatusMonitor) ClearWorkerStates() {
	m.workerStates.Range(func(key, value any) bool {
		m.workerStates.Delete(key)
		return true
	})
}
