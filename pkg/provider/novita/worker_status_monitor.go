// Package novita provides Novita deployment provider implementation.
// This file implements the Novita Worker Status Monitor for detecting worker failures.
package novita

import (
	"strings"
	"time"

	"waverless/pkg/interfaces"
	"waverless/pkg/status"
)

// NovitaWorkerStatusMonitor detects Novita worker failures from PodInfo.
// It is used by the lifecycle manager to classify failures from status change events.
//
// Note: This monitor does NOT do polling. Polling is handled by
// NovitaDeploymentProvider.runReplicaWatcher() which calls processWorkerState().
// This monitor is only used as a failure classifier via DetectFailure().
type NovitaWorkerStatusMonitor struct {
	sanitizer *status.StatusSanitizer
}

// NewNovitaWorkerStatusMonitor creates a new Novita worker status monitor.
// The client and workerRepo parameters are kept for API compatibility but not used.
func NewNovitaWorkerStatusMonitor(client clientInterface, workerRepo interface{}) *NovitaWorkerStatusMonitor {
	return &NovitaWorkerStatusMonitor{
		sanitizer: status.NewStatusSanitizer(),
	}
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
		(reasonLower != "" && reasonLower != "ready") ||
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

// ClassifyNovitaFailure converts Novita status to generic FailureType.
// This method maps Novita-specific error states to the generic failure types
// defined in pkg/interfaces/image_validation.go.
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
		OccurredAt:   time.Now(),
	}
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
