// Package endpoint provides endpoint management services for the Waverless platform.
// This file defines the EndpointStatusSummary types for aggregating worker statuses.
// It implements the status summary data structures for Requirement 4.2.
package endpoint

import (
	"context"
	"fmt"
	"time"

	"waverless/pkg/status"
	"waverless/pkg/store/mysql"
	"waverless/pkg/store/mysql/model"
)

// EndpointStatusSummary aggregates worker statuses for an endpoint.
// This structure provides a comprehensive overview of the endpoint's health and status.
type EndpointStatusSummary struct {
	// TotalWorkers is the total count of all workers for this endpoint.
	TotalWorkers int `json:"totalWorkers"`

	// WorkersByStatus maps worker status to count (e.g., ONLINE: 2, PENDING: 1).
	// The sum of all values should equal TotalWorkers.
	WorkersByStatus map[string]int `json:"workersByStatus"`

	// WorkersByPhase maps pending phase to count (e.g., WAITING_NODE: 1, PULLING_IMAGE: 0).
	// This only counts workers that are in PENDING status.
	WorkersByPhase map[string]int `json:"workersByPhase"`

	// PendingDetails contains detailed information about each pending worker.
	// This is omitted if there are no pending workers.
	PendingDetails []WorkerPendingDetail `json:"pendingDetails,omitempty"`

	// FailureDetails contains detailed information about each failed worker.
	// This is omitted if there are no failed workers.
	FailureDetails []WorkerFailureDetail `json:"failureDetails,omitempty"`

	// SpotCapacity contains the current AWS Spot capacity status.
	// This is omitted if Spot information is not available.
	SpotCapacity *status.SpotStatus `json:"spotCapacity,omitempty"`

	// LastUpdated is the timestamp when this summary was last computed.
	LastUpdated time.Time `json:"lastUpdated"`
}

// WorkerPendingDetail contains detailed information about a pending worker.
// This provides visibility into why a specific worker is not yet ready.
type WorkerPendingDetail struct {
	// WorkerID is the unique identifier of the worker.
	WorkerID string `json:"workerId"`

	// PodName is the Kubernetes pod name for this worker.
	PodName string `json:"podName"`

	// Phase is the specific pending phase (SCHEDULING, WAITING_NODE, PULLING_IMAGE, INITIALIZING).
	Phase status.PendingPhase `json:"phase"`

	// Reason is the Kubernetes reason for the pending state (e.g., "Unschedulable").
	Reason string `json:"reason"`

	// Message is a human-readable message describing the pending state.
	Message string `json:"message"`

	// Since is the timestamp when this pending phase started.
	Since time.Time `json:"since"`
}

// WorkerFailureDetail contains detailed information about a failed worker.
// This provides visibility into what went wrong and how to potentially fix it.
type WorkerFailureDetail struct {
	// WorkerID is the unique identifier of the worker.
	WorkerID string `json:"workerId"`

	// PodName is the Kubernetes pod name for this worker.
	PodName string `json:"podName"`

	// FailureType categorizes the type of failure (e.g., "ImagePullError", "OOMKilled", "CrashLoopBackOff").
	FailureType string `json:"failureType"`

	// Reason is the Kubernetes reason for the failure.
	Reason string `json:"reason"`

	// Suggestion provides a human-readable suggestion for resolving the failure.
	Suggestion string `json:"suggestion"`

	// OccurredAt is the timestamp when the failure occurred.
	OccurredAt time.Time `json:"occurredAt"`
}

// NewEndpointStatusSummary creates a new EndpointStatusSummary with initialized maps.
func NewEndpointStatusSummary() *EndpointStatusSummary {
	return &EndpointStatusSummary{
		TotalWorkers:    0,
		WorkersByStatus: make(map[string]int),
		WorkersByPhase:  make(map[string]int),
		PendingDetails:  []WorkerPendingDetail{},
		FailureDetails:  []WorkerFailureDetail{},
		LastUpdated:     time.Now(),
	}
}

// NewWorkerPendingDetail creates a new WorkerPendingDetail with the given parameters.
func NewWorkerPendingDetail(workerID, podName string, phase status.PendingPhase, reason, message string, since time.Time) WorkerPendingDetail {
	return WorkerPendingDetail{
		WorkerID: workerID,
		PodName:  podName,
		Phase:    phase,
		Reason:   reason,
		Message:  message,
		Since:    since,
	}
}

// NewWorkerFailureDetail creates a new WorkerFailureDetail with the given parameters.
func NewWorkerFailureDetail(workerID, podName, failureType, reason, suggestion string, occurredAt time.Time) WorkerFailureDetail {
	return WorkerFailureDetail{
		WorkerID:    workerID,
		PodName:     podName,
		FailureType: failureType,
		Reason:      reason,
		Suggestion:  suggestion,
		OccurredAt:  occurredAt,
	}
}

// HasPendingWorkers returns true if there are any pending workers.
func (s *EndpointStatusSummary) HasPendingWorkers() bool {
	return len(s.PendingDetails) > 0
}

// HasFailedWorkers returns true if there are any failed workers.
func (s *EndpointStatusSummary) HasFailedWorkers() bool {
	return len(s.FailureDetails) > 0
}

// GetOnlineCount returns the count of online workers.
func (s *EndpointStatusSummary) GetOnlineCount() int {
	return s.WorkersByStatus["ONLINE"]
}

// GetPendingCount returns the count of pending workers.
func (s *EndpointStatusSummary) GetPendingCount() int {
	return s.WorkersByStatus["PENDING"]
}

// GetFailedCount returns the count of failed workers.
func (s *EndpointStatusSummary) GetFailedCount() int {
	return s.WorkersByStatus["FAILED"]
}

// AddWorkerStatus increments the count for a given worker status.
func (s *EndpointStatusSummary) AddWorkerStatus(status string) {
	s.WorkersByStatus[status]++
	s.TotalWorkers++
}

// AddWorkerPhase increments the count for a given pending phase.
func (s *EndpointStatusSummary) AddWorkerPhase(phase status.PendingPhase) {
	s.WorkersByPhase[string(phase)]++
}

// AddPendingDetail adds a pending worker detail to the summary.
func (s *EndpointStatusSummary) AddPendingDetail(detail WorkerPendingDetail) {
	s.PendingDetails = append(s.PendingDetails, detail)
}

// AddFailureDetail adds a failure worker detail to the summary.
func (s *EndpointStatusSummary) AddFailureDetail(detail WorkerFailureDetail) {
	s.FailureDetails = append(s.FailureDetails, detail)
}

// SetSpotCapacity sets the Spot capacity status.
func (s *EndpointStatusSummary) SetSpotCapacity(spotStatus *status.SpotStatus) {
	s.SpotCapacity = spotStatus
}

// UpdateTimestamp updates the LastUpdated timestamp to the current time.
func (s *EndpointStatusSummary) UpdateTimestamp() {
	s.LastUpdated = time.Now()
}

// workerRepository defines the interface for accessing worker data with pending phase information.
// This interface is used by ComputeStatusSummary to get workers with their pending phase details.
type workerRepository interface {
	// GetByEndpoint returns all active workers for an endpoint (excluding OFFLINE workers).
	GetByEndpoint(ctx context.Context, endpoint string) ([]*model.Worker, error)
}

// capacityManager defines the interface for accessing Spot capacity status.
// This interface is used by ComputeStatusSummary to get Spot status for WAITING_NODE phase.
type capacityManager interface {
	// GetSpotStatusBySpec returns the current Spot capacity status for the given spec name.
	GetSpotStatusBySpec(specName string) *status.SpotStatus
}

// StatusSummaryDependencies holds optional dependencies for ComputeStatusSummary.
// These are injected via setter methods after Service construction.
type StatusSummaryDependencies struct {
	workerRepo      workerRepository
	capacityManager capacityManager
	endpointRepo    endpointRepositoryForSummary
}

// SetWorkerRepository sets the worker repository for status summary computation.
// This must be called before ComputeStatusSummary can be used.
func (s *Service) SetWorkerRepository(repo *mysql.WorkerRepository) {
	if s.statusSummaryDeps == nil {
		s.statusSummaryDeps = &StatusSummaryDependencies{}
	}
	s.statusSummaryDeps.workerRepo = repo
}

// SetCapacityManager sets the capacity manager for Spot status lookup.
// This is optional - if not set, SpotCapacity will be nil in the summary.
func (s *Service) SetCapacityManager(mgr capacityManager) {
	if s.statusSummaryDeps == nil {
		s.statusSummaryDeps = &StatusSummaryDependencies{}
	}
	s.statusSummaryDeps.capacityManager = mgr
}

// ComputeStatusSummary computes the status summary for an endpoint.
// It aggregates worker statuses, collects pending and failure details, and gets Spot capacity status.
//
// Implementation logic:
// 1. Get all workers for the endpoint from WorkerRepository
// 2. Count workers by status (WorkersByStatus)
// 3. For PENDING workers, count by phase (WorkersByPhase)
// 4. Collect pending worker details (PendingDetails)
// 5. Collect failed worker details (FailureDetails)
// 6. If any worker is in WAITING_NODE phase, get Spot capacity status
//
func (s *Service) ComputeStatusSummary(ctx context.Context, endpoint string) (*EndpointStatusSummary, error) {
	// Check if worker repository is configured
	if s.statusSummaryDeps == nil || s.statusSummaryDeps.workerRepo == nil {
		return nil, fmt.Errorf("worker repository not configured for status summary")
	}

	// Get all workers for the endpoint
	workers, err := s.statusSummaryDeps.workerRepo.GetByEndpoint(ctx, endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to get workers for endpoint %s: %w", endpoint, err)
	}

	// Initialize the summary
	summary := NewEndpointStatusSummary()

	// Track if any worker is in WAITING_NODE phase for Spot status lookup
	hasWaitingNodeWorker := false
	var specName string

	// Process each worker
	for _, worker := range workers {
		// Count by status
		summary.AddWorkerStatus(worker.Status)

		// Check if worker is in a pending-like status (STARTING is the pending status in this system)
		isPending := worker.Status == "STARTING" || worker.Status == "PENDING"

		if isPending && worker.PendingPhase != nil {
			phase := status.PendingPhase(*worker.PendingPhase)

			// Count by phase
			summary.AddWorkerPhase(phase)

			// Collect pending details
			var since time.Time
			if worker.PendingPhaseSince != nil {
				since = *worker.PendingPhaseSince
			} else {
				since = worker.CreatedAt
			}

			var reason, message string
			if worker.PendingReason != nil {
				reason = *worker.PendingReason
			}
			if worker.PendingMessage != nil {
				message = *worker.PendingMessage
			}

			detail := NewWorkerPendingDetail(
				worker.WorkerID,
				worker.PodName,
				phase,
				reason,
				message,
				since,
			)
			summary.AddPendingDetail(detail)

			// Check for WAITING_NODE phase
			if phase == status.PendingPhaseWaitingNode {
				hasWaitingNodeWorker = true
			}
		} else if isPending {
			// Worker is pending but no phase detected yet, default to SCHEDULING
			summary.AddWorkerPhase(status.PendingPhaseScheduling)

			detail := NewWorkerPendingDetail(
				worker.WorkerID,
				worker.PodName,
				status.PendingPhaseScheduling,
				"",
				"Waiting for scheduler",
				worker.CreatedAt,
			)
			summary.AddPendingDetail(detail)
		}

		// Check for failed workers
		if worker.FailureType != "" {
			var occurredAt time.Time
			if worker.FailureOccurredAt != nil {
				occurredAt = *worker.FailureOccurredAt
			} else {
				occurredAt = worker.UpdatedAt
			}

			suggestion := getFailureSuggestion(worker.FailureType)

			detail := NewWorkerFailureDetail(
				worker.WorkerID,
				worker.PodName,
				worker.FailureType,
				worker.FailureReason,
				suggestion,
				occurredAt,
			)
			summary.AddFailureDetail(detail)
		}
	}

	// Get Spot capacity status if any worker is in WAITING_NODE phase
	if hasWaitingNodeWorker && s.statusSummaryDeps.capacityManager != nil {
		// Get the endpoint's spec name for Spot status lookup
		if specName == "" {
			// Try to get spec name from endpoint metadata
			endpointMeta, err := s.GetEndpoint(ctx, endpoint)
			if err == nil && endpointMeta != nil {
				specName = endpointMeta.SpecName
			}
		}

		if specName != "" {
			spotStatus := s.statusSummaryDeps.capacityManager.GetSpotStatusBySpec(specName)
			summary.SetSpotCapacity(spotStatus)
		}
	}

	// Update timestamp
	summary.UpdateTimestamp()

	return summary, nil
}

// getFailureSuggestion returns a human-readable suggestion for resolving a failure type.
func getFailureSuggestion(failureType string) string {
	switch failureType {
	case "IMAGE_PULL_FAILED":
		return "Check if the image exists and is accessible. Verify image name, tag, and registry credentials."
	case "CONTAINER_CRASH":
		return "Check container logs for crash details. The application may have an error or missing dependencies."
	case "RESOURCE_LIMIT":
		return "The container exceeded resource limits. Consider increasing memory or CPU limits."
	case "TIMEOUT":
		return "The container took too long to start. Check if the image is large or if there are network issues."
	case "OOM_KILLED":
		return "The container was killed due to out of memory. Increase the memory limit for this endpoint."
	default:
		return "Check the worker logs for more details about this failure."
	}
}

// ToMap converts the EndpointStatusSummary to a map for JSON storage in the database.
// This is used by UpdateStatusSummary to store the summary in the endpoints table.
func (s *EndpointStatusSummary) ToMap() map[string]any {
	result := map[string]any{
		"totalWorkers":    s.TotalWorkers,
		"workersByStatus": s.WorkersByStatus,
		"workersByPhase":  s.WorkersByPhase,
		"lastUpdated":     s.LastUpdated.Format(time.RFC3339),
	}

	// Only include pendingDetails if there are pending workers
	if len(s.PendingDetails) > 0 {
		pendingDetails := make([]map[string]any, len(s.PendingDetails))
		for i, detail := range s.PendingDetails {
			pendingDetails[i] = map[string]any{
				"workerId": detail.WorkerID,
				"podName":  detail.PodName,
				"phase":    string(detail.Phase),
				"reason":   detail.Reason,
				"message":  detail.Message,
				"since":    detail.Since.Format(time.RFC3339),
			}
		}
		result["pendingDetails"] = pendingDetails
	}

	// Only include failureDetails if there are failed workers
	if len(s.FailureDetails) > 0 {
		failureDetails := make([]map[string]any, len(s.FailureDetails))
		for i, detail := range s.FailureDetails {
			failureDetails[i] = map[string]any{
				"workerId":    detail.WorkerID,
				"podName":     detail.PodName,
				"failureType": detail.FailureType,
				"reason":      detail.Reason,
				"suggestion":  detail.Suggestion,
				"occurredAt":  detail.OccurredAt.Format(time.RFC3339),
			}
		}
		result["failureDetails"] = failureDetails
	}

	// Only include spotCapacity if available
	if s.SpotCapacity != nil {
		result["spotCapacity"] = map[string]any{
			"capacity":     s.SpotCapacity.Capacity,
			"score":        s.SpotCapacity.Score,
			"price":        s.SpotCapacity.Price,
			"instanceType": s.SpotCapacity.InstanceType,
		}
	}

	return result
}

// endpointRepositoryForSummary defines the interface for updating endpoint status summary.
// This is a subset of the full endpointRepository interface, used specifically for status summary updates.
type endpointRepositoryForSummary interface {
	UpdateStatusSummary(ctx context.Context, endpointName string, statusSummary map[string]any) error
}

// SetEndpointRepository sets the endpoint repository for status summary persistence.
// This must be called before UpdateStatusSummary can be used.
func (s *Service) SetEndpointRepository(repo *mysql.EndpointRepository) {
	if s.statusSummaryDeps == nil {
		s.statusSummaryDeps = &StatusSummaryDependencies{}
	}
	s.statusSummaryDeps.endpointRepo = repo
}

// UpdateStatusSummary computes and persists the status summary for an endpoint.
// This method should be called when any Worker status changes to update the cached summary.
//
// Implementation logic:
// 1. Call ComputeStatusSummary to get the current status summary
// 2. Convert the summary to a map using ToMap()
// 3. Call EndpointRepository.UpdateStatusSummary to persist the summary
//
func (s *Service) UpdateStatusSummary(ctx context.Context, endpoint string) error {
	// Compute the current status summary
	summary, err := s.ComputeStatusSummary(ctx, endpoint)
	if err != nil {
		return fmt.Errorf("failed to compute status summary for endpoint %s: %w", endpoint, err)
	}

	// Check if endpoint repository is configured for persistence
	if s.statusSummaryDeps == nil || s.statusSummaryDeps.endpointRepo == nil {
		return fmt.Errorf("endpoint repository not configured for status summary persistence")
	}

	// Convert summary to map and persist
	summaryMap := summary.ToMap()
	if err := s.statusSummaryDeps.endpointRepo.UpdateStatusSummary(ctx, endpoint, summaryMap); err != nil {
		return fmt.Errorf("failed to persist status summary for endpoint %s: %w", endpoint, err)
	}

	return nil
}
