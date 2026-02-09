// Package k8s provides Kubernetes deployment provider implementation.
// This file implements the K8s Worker Status Monitor for tracking worker failures.
package k8s

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

// StatusEventRecorder interface for recording status events.
// This allows for dependency injection and easier testing.
type StatusEventRecorder interface {
	// RecordStatusChange records a worker status change event.
	RecordStatusChange(ctx context.Context, workerID, endpoint, oldStatus, newStatus, reason, message string) error
	// RecordPhaseChange records a pending phase change event.
	RecordPhaseChange(ctx context.Context, workerID, endpoint, phase, reason, message string, spotStatus *status.SpotStatus) error
	// RecordFailure records a worker failure event.
	RecordFailure(ctx context.Context, workerID, endpoint, reason, message string) error
}

// K8sWorkerStatusMonitor monitors K8s worker (pod) status changes and detects failures.
// It provides methods for detecting failures from pod info and updating worker records.
//
// Usage: Use DetectFailure() and UpdateWorkerFailure() within an existing pod watcher
// (setupPodStatusWatcher in initializers.go) to avoid duplicate callbacks.
//
// Validates: Requirements 1.4, 3.1, 3.2, 3.3
type K8sWorkerStatusMonitor struct {
	manager             *Manager
	workerRepo          *mysql.WorkerRepository
	sanitizer           *status.StatusSanitizer
	statusEventRecorder StatusEventRecorder
	pendingDetector     *status.PendingPhaseDetector

	// workerStatusCache tracks the last known status for each worker to detect changes.
	// Key: workerID (podName), Value: last known status
	workerStatusCache map[string]string
	// workerPhaseCache tracks the last known pending phase for each worker.
	// Key: workerID (podName), Value: last known pending phase
	workerPhaseCache map[string]status.PendingPhase
	cacheMu          sync.RWMutex
}

// NewK8sWorkerStatusMonitor creates a new K8s worker status monitor.
//
// Parameters:
//   - manager: The K8s manager that provides pod watching capabilities
//   - workerRepo: The worker repository for updating worker failure information
//
// Returns:
//   - A new K8sWorkerStatusMonitor instance
func NewK8sWorkerStatusMonitor(manager *Manager, workerRepo *mysql.WorkerRepository) *K8sWorkerStatusMonitor {
	return &K8sWorkerStatusMonitor{
		manager:           manager,
		workerRepo:        workerRepo,
		sanitizer:         status.NewStatusSanitizer(),
		workerStatusCache: make(map[string]string),
		workerPhaseCache:  make(map[string]status.PendingPhase),
	}
}

// WithStatusEventRecorder sets the status event recorder for recording status events.
// This enables the monitor to record status change, phase change, and failure events.
// Validates: Requirements 1.4, 3.1
func (m *K8sWorkerStatusMonitor) WithStatusEventRecorder(recorder StatusEventRecorder) *K8sWorkerStatusMonitor {
	m.statusEventRecorder = recorder
	return m
}

// WithPendingPhaseDetector sets the pending phase detector for detecting pending phases.
// This enables the monitor to detect and record pending phase changes.
// Validates: Requirements 1.1, 1.2, 1.3
func (m *K8sWorkerStatusMonitor) WithPendingPhaseDetector(detector *status.PendingPhaseDetector) *K8sWorkerStatusMonitor {
	m.pendingDetector = detector
	return m
}

// DetectFailure checks if the pod info indicates a failure state.
// Returns WorkerFailureInfo if a failure is detected, nil otherwise.
//
// This method should be called from the pod status watcher callback
// (setupPodStatusWatcher in initializers.go).
//
// It examines the pod's Reason and Status fields to identify failure conditions:
// - ImagePullBackOff, ErrImagePull, InvalidImageName -> IMAGE_PULL_FAILED
// - CrashLoopBackOff, Error -> CONTAINER_CRASH
// - OutOfMemory, OutOfCpu -> RESOURCE_LIMIT
//
// IMPORTANT: Pods with DeletionTimestamp set are being gracefully terminated
// (e.g., scale-down, manual deletion) and should NOT be marked as failures.
// The "Error" reason during termination is expected K8s behavior, not a crash.
//
// Validates: Requirements 3.2, 3.3
func (m *K8sWorkerStatusMonitor) DetectFailure(info *interfaces.PodInfo) *interfaces.WorkerFailureInfo {
	if info == nil {
		return nil
	}

	// CRITICAL: Skip failure detection for pods being gracefully terminated.
	// When K8s deletes a pod (scale-down, manual deletion), the pod's reason
	// becomes "Error" which would be misclassified as CONTAINER_CRASH.
	// DeletionTimestamp being set indicates intentional termination, not a crash.
	if info.DeletionTimestamp != "" {
		return nil
	}

	// Check if the reason indicates a failure
	failureType := m.ClassifyK8sFailure(info.Reason, info.Message)
	if failureType == interfaces.FailureTypeUnknown {
		// Also check the status field for failure indicators
		failureType = m.ClassifyK8sFailure(info.Status, info.Message)
	}

	// Only return failure info for actual failures
	if !isFailureState(info.Reason, info.Status) {
		return nil
	}

	// Sanitize the message to remove sensitive information
	sanitizedMsg := ""
	if m.sanitizer != nil {
		sanitized := m.sanitizer.Sanitize(failureType, info.Reason, info.Message)
		if sanitized != nil {
			sanitizedMsg = sanitized.UserMessage
			if sanitized.Suggestion != "" {
				sanitizedMsg += ". " + sanitized.Suggestion
			}
		}
	}

	return &interfaces.WorkerFailureInfo{
		Type:         failureType,
		Reason:       info.Reason,
		Message:      info.Message,
		SanitizedMsg: sanitizedMsg,
		OccurredAt:   time.Now().UTC(), // Use local time - GORM will convert to UTC for storage
	}
}

// isFailureState checks if the given reason or status indicates a failure state.
// This is used to filter out normal states like "Running", "Ready", "ContainerCreating".
func isFailureState(reason, status string) bool {
	// Known failure reasons
	failureReasons := map[string]bool{
		"ImagePullBackOff":     true,
		"ErrImagePull":         true,
		"InvalidImageName":     true,
		"ImageInspectError":    true,
		"CrashLoopBackOff":     true,
		"Error":                true,
		"OOMKilled":            true,
		"ContainerCannotRun":   true,
		"OutOfMemory":          true,
		"OutOfCpu":             true,
		"Unschedulable":        true,
		"FailedScheduling":     true,
		"FailedMount":          true,
		"FailedAttachVolume":   true,
		"CreateContainerError": true,
	}

	if failureReasons[reason] {
		return true
	}

	// Check status as well
	if failureReasons[status] {
		return true
	}

	// Check for "Failed" phase
	if strings.EqualFold(status, "Failed") || strings.EqualFold(reason, "Failed") {
		return true
	}

	return false
}

// ClassifyK8sFailure converts K8s reason to generic FailureType.
// This method maps Kubernetes-specific error reasons to the generic failure types
// defined in pkg/interfaces/image_validation.go.
//
// Parameters:
//   - reason: The K8s reason string (e.g., "ImagePullBackOff", "CrashLoopBackOff")
//   - message: The K8s message string (used for additional context)
//
// Returns:
//   - The corresponding FailureType
//
// Validates: Requirements 3.2, 6.2
func (m *K8sWorkerStatusMonitor) ClassifyK8sFailure(reason, message string) interfaces.FailureType {
	// Normalize reason for comparison
	reasonLower := strings.ToLower(reason)

	// Image pull failures
	switch reason {
	case "ImagePullBackOff", "ErrImagePull", "InvalidImageName", "ImageInspectError":
		return interfaces.FailureTypeImagePull
	}

	// Check for image-related keywords in reason
	if strings.Contains(reasonLower, "image") &&
		(strings.Contains(reasonLower, "pull") ||
			strings.Contains(reasonLower, "error") ||
			strings.Contains(reasonLower, "invalid")) {
		return interfaces.FailureTypeImagePull
	}

	// Container crash failures
	switch reason {
	case "CrashLoopBackOff", "Error", "OOMKilled", "ContainerCannotRun", "CreateContainerError":
		return interfaces.FailureTypeContainerCrash
	}

	// Check for crash-related keywords
	if strings.Contains(reasonLower, "crash") ||
		strings.Contains(reasonLower, "oom") ||
		strings.Contains(reasonLower, "killed") {
		return interfaces.FailureTypeContainerCrash
	}

	// Resource limit failures
	switch reason {
	case "OutOfMemory", "OutOfCpu", "Unschedulable", "FailedScheduling":
		return interfaces.FailureTypeResourceLimit
	}

	// Check for resource-related keywords
	if strings.Contains(reasonLower, "memory") ||
		strings.Contains(reasonLower, "cpu") ||
		strings.Contains(reasonLower, "resource") ||
		strings.Contains(reasonLower, "unschedulable") {
		return interfaces.FailureTypeResourceLimit
	}

	// Check message for additional context
	messageLower := strings.ToLower(message)

	// Image-related messages
	if strings.Contains(messageLower, "image") &&
		(strings.Contains(messageLower, "pull") ||
			strings.Contains(messageLower, "not found") ||
			strings.Contains(messageLower, "unauthorized")) {
		return interfaces.FailureTypeImagePull
	}

	// Resource-related messages
	if strings.Contains(messageLower, "insufficient") ||
		strings.Contains(messageLower, "no nodes available") {
		return interfaces.FailureTypeResourceLimit
	}

	// Timeout-related
	if strings.Contains(reasonLower, "timeout") || strings.Contains(messageLower, "timeout") {
		return interfaces.FailureTypeTimeout
	}

	return interfaces.FailureTypeUnknown
}

// UpdateWorkerFailure updates the worker record with failure information.
// This method persists the failure details to the database for later retrieval.
//
// This method should be called from the pod status watcher callback
// (setupPodStatusWatcher in initializers.go) after DetectFailure() returns non-nil.
//
// Parameters:
//   - ctx: Context for database operations
//   - podName: The name of the pod (used as worker ID)
//   - endpoint: The endpoint name
//   - info: The failure information to store
//
// Returns:
//   - error if the database update fails
//
// Validates: Requirements 3.3, 3.4
func (m *K8sWorkerStatusMonitor) UpdateWorkerFailure(ctx context.Context, podName, endpoint string, info *interfaces.WorkerFailureInfo) error {
	if m.workerRepo == nil || info == nil {
		return nil
	}

	// Build failure details JSON
	details := map[string]interface{}{
		"type":         string(info.Type),
		"reason":       info.Reason,
		"message":      info.Message,
		"sanitizedMsg": info.SanitizedMsg,
		"occurredAt":   info.OccurredAt.Format(time.RFC3339),
	}
	detailsJSON, err := json.Marshal(details)
	if err != nil {
		logger.WarnCtx(ctx, "Failed to marshal failure details: %v", err)
		detailsJSON = []byte("{}")
	}

	// Update worker record using the repository
	return m.workerRepo.UpdateWorkerFailure(ctx, podName, string(info.Type), info.SanitizedMsg, string(detailsJSON), info.OccurredAt)
}

// GetManager returns the underlying K8s manager.
// This is useful for accessing other K8s operations.
func (m *K8sWorkerStatusMonitor) GetManager() *Manager {
	return m.manager
}

// GetSanitizer returns the status sanitizer.
// This is useful for sanitizing error messages externally.
func (m *K8sWorkerStatusMonitor) GetSanitizer() *status.StatusSanitizer {
	return m.sanitizer
}

// HandleStatusChange handles a worker status change and records the event.
// This method should be called when a pod status changes.
// It detects status changes, phase changes, and records appropriate events.
//
// Parameters:
//   - ctx: Context for database operations
//   - podName: The name of the pod (used as worker ID)
//   - endpoint: The endpoint name
//   - info: The current pod information
//   - podConditions: The pod conditions (optional, for pending phase detection)
//
// Validates: Requirements 1.4, 3.1
func (m *K8sWorkerStatusMonitor) HandleStatusChange(ctx context.Context, podName, endpoint string, info *interfaces.PodInfo, podConditions []interfaces.PodCondition) {
	if info == nil {
		return
	}

	// Get the current status from pod info
	currentStatus := m.normalizeStatus(info.Phase, info.Status)

	// Check if status has changed
	m.cacheMu.RLock()
	oldStatus, hasOldStatus := m.workerStatusCache[podName]
	m.cacheMu.RUnlock()

	statusChanged := !hasOldStatus || oldStatus != currentStatus

	// Record status change event if status changed
	if statusChanged && m.statusEventRecorder != nil {
		oldStatusStr := ""
		if hasOldStatus {
			oldStatusStr = oldStatus
		}

		if err := m.statusEventRecorder.RecordStatusChange(ctx, podName, endpoint, oldStatusStr, currentStatus, info.Reason, info.Message); err != nil {
			logger.WarnCtx(ctx, "Failed to record status change event for worker %s: %v", podName, err)
		} else {
			logger.DebugCtx(ctx, "Recorded status change event for worker %s: %s -> %s", podName, oldStatusStr, currentStatus)
		}

		// Update status cache
		m.cacheMu.Lock()
		m.workerStatusCache[podName] = currentStatus
		m.cacheMu.Unlock()
	}

	// Handle pending phase detection and recording
	if currentStatus == "Pending" && m.pendingDetector != nil {
		m.handlePendingPhaseChange(ctx, podName, endpoint, info, podConditions)
	} else {
		// Clear phase cache if not pending
		m.cacheMu.Lock()
		delete(m.workerPhaseCache, podName)
		m.cacheMu.Unlock()
	}
}

// handlePendingPhaseChange detects and records pending phase changes.
// Validates: Requirements 1.4, 3.1
func (m *K8sWorkerStatusMonitor) handlePendingPhaseChange(ctx context.Context, podName, endpoint string, info *interfaces.PodInfo, podConditions []interfaces.PodCondition) {
	// Detect the current pending phase
	phaseInfo := m.pendingDetector.DetectPhase(info, podConditions)
	if phaseInfo == nil {
		return
	}

	currentPhase := phaseInfo.Phase

	// Check if phase has changed
	m.cacheMu.RLock()
	oldPhase, hasOldPhase := m.workerPhaseCache[podName]
	m.cacheMu.RUnlock()

	phaseChanged := !hasOldPhase || oldPhase != currentPhase

	// Record phase change event if phase changed
	if phaseChanged && m.statusEventRecorder != nil {
		if err := m.statusEventRecorder.RecordPhaseChange(ctx, podName, endpoint, string(currentPhase), phaseInfo.Reason, phaseInfo.Message, phaseInfo.SpotStatus); err != nil {
			logger.WarnCtx(ctx, "Failed to record phase change event for worker %s: %v", podName, err)
		} else {
			logger.DebugCtx(ctx, "Recorded phase change event for worker %s: %s -> %s", podName, oldPhase, currentPhase)
		}

		// Update phase cache
		m.cacheMu.Lock()
		m.workerPhaseCache[podName] = currentPhase
		m.cacheMu.Unlock()
	}
}

// HandleFailure handles a worker failure and records the event.
// This method should be called when a failure is detected.
//
// Parameters:
//   - ctx: Context for database operations
//   - podName: The name of the pod (used as worker ID)
//   - endpoint: The endpoint name
//   - failureInfo: The failure information
//
// Validates: Requirements 3.1
func (m *K8sWorkerStatusMonitor) HandleFailure(ctx context.Context, podName, endpoint string, failureInfo *interfaces.WorkerFailureInfo) {
	if failureInfo == nil || m.statusEventRecorder == nil {
		return
	}

	// Record failure event
	if err := m.statusEventRecorder.RecordFailure(ctx, podName, endpoint, failureInfo.Reason, failureInfo.SanitizedMsg); err != nil {
		logger.WarnCtx(ctx, "Failed to record failure event for worker %s: %v", podName, err)
	} else {
		logger.DebugCtx(ctx, "Recorded failure event for worker %s: %s - %s", podName, failureInfo.Reason, failureInfo.SanitizedMsg)
	}
}

// normalizeStatus normalizes the pod phase and status to a consistent status string.
// This ensures consistent status values for event recording.
func (m *K8sWorkerStatusMonitor) normalizeStatus(phase, podStatus string) string {
	// Use phase as the primary status indicator
	switch phase {
	case "Running":
		return "Running"
	case "Pending":
		return "Pending"
	case "Succeeded":
		return "Succeeded"
	case "Failed":
		return "Failed"
	case "Unknown":
		return "Unknown"
	default:
		// Fall back to podStatus if phase is not recognized
		if podStatus != "" {
			return podStatus
		}
		return "Unknown"
	}
}

// ClearWorkerCache removes a worker from the status and phase caches.
// This should be called when a worker is deleted.
func (m *K8sWorkerStatusMonitor) ClearWorkerCache(podName string) {
	m.cacheMu.Lock()
	defer m.cacheMu.Unlock()
	delete(m.workerStatusCache, podName)
	delete(m.workerPhaseCache, podName)
}
