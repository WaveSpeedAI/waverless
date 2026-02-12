// Package gmi provides GMI deployment provider implementation.
// This file implements the GMI Worker Status Monitor for tracking worker failures.
package gmi

import (
	"encoding/json"
	"strings"
	"sync"
	"time"

	"waverless/pkg/interfaces"
	"waverless/pkg/logger"
	"waverless/pkg/status"
	"waverless/pkg/store/mysql"
)

// GMIWorkerStatusMonitor monitors GMI worker status changes and detects failures.
// It uses gmiless API polling to detect status changes (same approach as Novita).
type GMIWorkerStatusMonitor struct {
	workerRepo *mysql.WorkerRepository
	sanitizer  *status.StatusSanitizer

	// workerStates tracks the last known state of each worker
	// key: workerID, value: *monitorWorkerState
	workerStates sync.Map
}

// monitorWorkerState stores the last known state of a worker for the status monitor
type monitorWorkerState struct {
	Status    string    // Worker status: "ONLINE", "OFFLINE", "STARTING", etc.
	Phase     string    // Pod phase
	Reason    string    // Reason if failed
	Message   string    // Status message
	UpdatedAt time.Time // Last update time
}

// NewGMIWorkerStatusMonitor creates a new GMI worker status monitor.
func NewGMIWorkerStatusMonitor(workerRepo *mysql.WorkerRepository) *GMIWorkerStatusMonitor {
	return &GMIWorkerStatusMonitor{
		workerRepo: workerRepo,
		sanitizer:  status.NewStatusSanitizer(),
	}
}

// DetectFailure detects if a worker is in a failed state from PodInfo.
// Returns nil if the worker is not in a failed state.
func (m *GMIWorkerStatusMonitor) DetectFailure(info *interfaces.PodInfo) *interfaces.WorkerFailureInfo {
	if info == nil {
		return nil
	}

	phaseLower := strings.ToLower(info.Phase)
	statusLower := strings.ToLower(info.Status)
	reasonLower := strings.ToLower(info.Reason)
	messageLower := strings.ToLower(info.Message)

	// Check for failure indicators
	isFailed := phaseLower == "failed" || phaseLower == "error" ||
		statusLower == "failed" || statusLower == "error" ||
		strings.Contains(phaseLower, "fail") ||
		strings.Contains(statusLower, "fail") ||
		(reasonLower != "" && reasonLower != "ready") ||
		strings.Contains(messageLower, "error") ||
		strings.Contains(messageLower, "fail")

	if !isFailed {
		return nil
	}

	failureType := m.classifyFailure(info.Phase, info.Status, info.Reason, info.Message)
	return m.createFailureInfo(failureType, info.Phase, info.Reason, info.Message)
}

// classifyFailure converts GMI worker status to generic FailureType.
func (m *GMIWorkerStatusMonitor) classifyFailure(phase, status, reason, message string) interfaces.FailureType {
	allLower := strings.ToLower(phase + " " + status + " " + reason + " " + message)

	// Image pull failures
	if containsAny(allLower, "image", "pull", "registry", "manifest", "repository", "not found") {
		return interfaces.FailureTypeImagePull
	}

	// Container crash failures
	if containsAny(allLower, "crash", "exit", "oom", "killed", "container error") {
		return interfaces.FailureTypeContainerCrash
	}

	// Resource limit failures
	if containsAny(allLower, "resource", "memory", "cpu", "gpu", "quota", "limit", "insufficient", "unavailable") {
		return interfaces.FailureTypeResourceLimit
	}

	// Timeout failures
	if containsAny(allLower, "timeout", "deadline", "timed out") {
		return interfaces.FailureTypeTimeout
	}

	return interfaces.FailureTypeUnknown
}

// createFailureInfo creates a WorkerFailureInfo from state information.
func (m *GMIWorkerStatusMonitor) createFailureInfo(failureType interfaces.FailureType, state, reason, message string) *interfaces.WorkerFailureInfo {
	r := state
	if reason != "" {
		r = reason
	}

	sanitizedMsg := ""
	if m.sanitizer != nil {
		sanitized := m.sanitizer.Sanitize(failureType, r, message)
		if sanitized != nil {
			sanitizedMsg = sanitized.UserMessage
			if sanitized.Suggestion != "" {
				sanitizedMsg += ". " + sanitized.Suggestion
			}
		}
	}

	return &interfaces.WorkerFailureInfo{
		Type:         failureType,
		Reason:       r,
		Message:      message,
		SanitizedMsg: sanitizedMsg,
		OccurredAt:   time.Now(),
	}
}

// UpdateWorkerFailure updates the worker record with failure information in the database.
func (m *GMIWorkerStatusMonitor) UpdateWorkerFailure(workerID, endpoint string, info *interfaces.WorkerFailureInfo) error {
	if m.workerRepo == nil || info == nil {
		return nil
	}

	details := map[string]any{
		"type":         string(info.Type),
		"reason":       info.Reason,
		"message":      info.Message,
		"sanitizedMsg": info.SanitizedMsg,
		"occurredAt":   info.OccurredAt.Format(time.RFC3339),
		"provider":     "gmi",
	}
	detailsJSON, err := json.Marshal(details)
	if err != nil {
		logger.Warnf("GMI: failed to marshal failure details: %v", err)
		detailsJSON = []byte("{}")
	}

	return m.workerRepo.UpdateWorkerFailure(nil, workerID, string(info.Type), info.SanitizedMsg, string(detailsJSON), info.OccurredAt)
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
