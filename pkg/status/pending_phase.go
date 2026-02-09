// Package status provides status tracking functionality for the Waverless platform.
// This file defines the PendingPhase types and constants for tracking Worker pending states.
// It implements the pending phase classification logic for Requirement 1.1.
package status

import (
	"time"
)

// PendingPhase represents the specific phase within Pending status.
// When a Worker enters Pending status, it is classified into one of these phases
// to provide more detailed information about why the service is not ready yet.
// Validates: Requirement 1.1
type PendingPhase string

const (
	// PendingPhaseScheduling indicates the Pod is created and waiting for the scheduler.
	// This is the default phase when no specific condition is detected.
	PendingPhaseScheduling PendingPhase = "SCHEDULING"

	// PendingPhaseWaitingNode indicates the Pod is unschedulable and waiting for node scaling.
	// This is detected when PodScheduled condition is False with reason "Unschedulable".
	// Validates: Requirement 1.2
	PendingPhaseWaitingNode PendingPhase = "WAITING_NODE"

	// PendingPhasePullingImage indicates the node is ready but pulling the container image.
	// This is detected when container status is Waiting with reason "ContainerCreating" or "ImagePullBackOff".
	// Validates: Requirement 1.3
	PendingPhasePullingImage PendingPhase = "PULLING_IMAGE"

	// PendingPhaseInitializing indicates the image is pulled and init containers are running.
	PendingPhaseInitializing PendingPhase = "INITIALIZING"
)

// AllPendingPhases returns all valid pending phases.
// This is useful for validation and iteration.
func AllPendingPhases() []PendingPhase {
	return []PendingPhase{
		PendingPhaseScheduling,
		PendingPhaseWaitingNode,
		PendingPhasePullingImage,
		PendingPhaseInitializing,
	}
}

// IsValid checks if the PendingPhase is a valid value.
func (p PendingPhase) IsValid() bool {
	switch p {
	case PendingPhaseScheduling, PendingPhaseWaitingNode, PendingPhasePullingImage, PendingPhaseInitializing:
		return true
	default:
		return false
	}
}

// String returns the string representation of the PendingPhase.
func (p PendingPhase) String() string {
	return string(p)
}

// SpotCapacity represents the AWS Spot capacity classification.
// Validates: Requirement 2.3
type SpotCapacity string

const (
	// SpotCapacityAvailable indicates Spot capacity is available (score >= 7).
	SpotCapacityAvailable SpotCapacity = "AVAILABLE"

	// SpotCapacityLimited indicates Spot capacity is limited (score 4-6).
	SpotCapacityLimited SpotCapacity = "LIMITED"

	// SpotCapacityConstrained indicates Spot capacity is constrained (score < 4).
	SpotCapacityConstrained SpotCapacity = "CONSTRAINED"
)

// SpotStatus contains AWS Spot capacity information.
// This is included in PendingPhaseInfo when the phase is WAITING_NODE.
// Validates: Requirement 2.4
type SpotStatus struct {
	// Capacity is the classified capacity status: AVAILABLE, LIMITED, or CONSTRAINED.
	Capacity SpotCapacity `json:"capacity"`

	// Score is the AWS Spot placement score (1-10).
	Score int `json:"score"`

	// Price is the current Spot price in USD/hour.
	Price float64 `json:"price"`

	// InstanceType is the AWS instance type (e.g., "g4dn.xlarge").
	InstanceType string `json:"instanceType"`
}

// PendingPhaseInfo contains detailed information about the pending phase.
// This structure provides comprehensive information about why a Worker is in Pending status.
// Validates: Requirement 1.1, 1.4
type PendingPhaseInfo struct {
	// Phase is the specific pending phase.
	Phase PendingPhase `json:"phase"`

	// Reason is the Kubernetes reason for the pending state (e.g., "Unschedulable").
	Reason string `json:"reason"`

	// Message is a human-readable message describing the pending state.
	Message string `json:"message"`

	// Since is the timestamp when this phase started.
	// Validates: Requirement 1.4
	Since time.Time `json:"since"`

	// SpotStatus contains AWS Spot capacity information.
	// This is only populated when Phase is WAITING_NODE.
	// Validates: Requirement 2.4
	SpotStatus *SpotStatus `json:"spotStatus,omitempty"`
}

// NewPendingPhaseInfo creates a new PendingPhaseInfo with the given parameters.
func NewPendingPhaseInfo(phase PendingPhase, reason, message string, since time.Time) *PendingPhaseInfo {
	return &PendingPhaseInfo{
		Phase:   phase,
		Reason:  reason,
		Message: message,
		Since:   since,
	}
}

// WithSpotStatus sets the SpotStatus on the PendingPhaseInfo and returns it.
// This is a builder-style method for convenience.
func (p *PendingPhaseInfo) WithSpotStatus(spotStatus *SpotStatus) *PendingPhaseInfo {
	p.SpotStatus = spotStatus
	return p
}

// Duration returns the duration since the phase started.
func (p *PendingPhaseInfo) Duration() time.Duration {
	return time.Since(p.Since)
}

// ClassifySpotCapacity classifies the Spot capacity based on the placement score.
// - Score >= 7: AVAILABLE
// - Score 4-6: LIMITED
// - Score < 4: CONSTRAINED
// Validates: Requirement 2.3
func ClassifySpotCapacity(score int) SpotCapacity {
	switch {
	case score >= 7:
		return SpotCapacityAvailable
	case score >= 4:
		return SpotCapacityLimited
	default:
		return SpotCapacityConstrained
	}
}

// NewSpotStatus creates a new SpotStatus with the given parameters.
// The capacity is automatically classified based on the score.
func NewSpotStatus(score int, price float64, instanceType string) *SpotStatus {
	return &SpotStatus{
		Capacity:     ClassifySpotCapacity(score),
		Score:        score,
		Price:        price,
		InstanceType: instanceType,
	}
}
