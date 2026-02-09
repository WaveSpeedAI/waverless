// Package status provides status tracking functionality for the Waverless platform.
// This file implements the PendingPhaseDetector for detecting specific phases of pending pods.
// It implements the pending phase detection logic for Requirements 1.1, 1.2, 1.3.
package status

import (
	"time"

	"waverless/pkg/interfaces"
)

// PendingPhaseDetector detects the specific phase of a pending pod.
// It analyzes pod conditions and container statuses to determine why a pod is pending.
// Validates: Requirements 1.1, 1.2, 1.3
type PendingPhaseDetector struct {
	// capacityManager is used to get Spot capacity information when phase is WAITING_NODE.
	// This field is optional and can be nil if Spot status is not needed.
	capacityManager CapacityManager
}

// CapacityManager interface for getting Spot capacity information.
// This allows for dependency injection and easier testing.
type CapacityManager interface {
	// GetSpotStatus returns the current Spot capacity status for the given instance type.
	// Returns nil if Spot information is not available.
	GetSpotStatus(instanceType string) *SpotStatus
}

// NewPendingPhaseDetector creates a new PendingPhaseDetector.
// The capacityManager parameter is optional and can be nil.
func NewPendingPhaseDetector(capacityManager CapacityManager) *PendingPhaseDetector {
	return &PendingPhaseDetector{
		capacityManager: capacityManager,
	}
}

// DetectPhase analyzes pod conditions and container statuses to determine the pending phase.
// The detection logic follows this priority order:
// 1. If PodScheduled condition is False with reason "Unschedulable" → WAITING_NODE
// 2. If container status is Waiting with reason "ContainerCreating" or "ImagePullBackOff" → PULLING_IMAGE
// 3. If init containers are running → INITIALIZING
// 4. Otherwise → SCHEDULING (default)
//
// Parameters:
//   - podInfo: The pod information containing container statuses and other details
//   - podConditions: The pod conditions from the Kubernetes API
//
// Returns:
//   - *PendingPhaseInfo containing the detected phase, reason, message, and timestamp
//
// Validates: Requirements 1.1, 1.2, 1.3
func (d *PendingPhaseDetector) DetectPhase(podInfo *interfaces.PodInfo, podConditions []interfaces.PodCondition) *PendingPhaseInfo {
	now := time.Now()

	// Handle nil podInfo - return default SCHEDULING phase
	if podInfo == nil {
		return NewPendingPhaseInfo(
			PendingPhaseScheduling,
			"",
			"Pod information not available",
			now,
		)
	}

	// Step 1: Check for Unschedulable condition → WAITING_NODE
	// Validates: Requirement 1.2
	if phase := d.detectWaitingNode(podConditions, now); phase != nil {
		return phase
	}

	// Step 2: Check for ContainerCreating or ImagePullBackOff → PULLING_IMAGE
	// Validates: Requirement 1.3
	if phase := d.detectPullingImage(podInfo, now); phase != nil {
		return phase
	}

	// Step 3: Check for init containers running → INITIALIZING
	if phase := d.detectInitializing(podInfo, now); phase != nil {
		return phase
	}

	// Step 4: Default to SCHEDULING
	return NewPendingPhaseInfo(
		PendingPhaseScheduling,
		podInfo.Reason,
		podInfo.Message,
		now,
	)
}

// detectWaitingNode checks if the pod is waiting for node scaling.
// Returns WAITING_NODE phase if PodScheduled condition is False with reason "Unschedulable".
// Validates: Requirement 1.2
func (d *PendingPhaseDetector) detectWaitingNode(podConditions []interfaces.PodCondition, now time.Time) *PendingPhaseInfo {
	for _, condition := range podConditions {
		// Check for PodScheduled condition with status False and reason Unschedulable
		if condition.Type == "PodScheduled" &&
			condition.Status == "False" &&
			condition.Reason == "Unschedulable" {

			phaseInfo := NewPendingPhaseInfo(
				PendingPhaseWaitingNode,
				condition.Reason,
				condition.Message,
				now,
			)

			// Try to add Spot status if capacity manager is available
			if d.capacityManager != nil {
				// Extract instance type from the message if possible
				// For now, we use a default instance type
				// In production, this would be extracted from the pod spec or node selector
				spotStatus := d.capacityManager.GetSpotStatus("")
				if spotStatus != nil {
					phaseInfo.WithSpotStatus(spotStatus)
				}
			}

			return phaseInfo
		}
	}
	return nil
}

// detectPullingImage checks if the pod is pulling container images.
// Returns PULLING_IMAGE phase if container status is Waiting with reason
// "ContainerCreating" or "ImagePullBackOff".
// Validates: Requirement 1.3
func (d *PendingPhaseDetector) detectPullingImage(podInfo *interfaces.PodInfo, now time.Time) *PendingPhaseInfo {
	// Check the pod's overall status and reason
	// When a pod is in ContainerCreating or ImagePullBackOff state,
	// the podInfo.Reason field typically contains this information
	if podInfo.Reason == "ContainerCreating" || podInfo.Reason == "ImagePullBackOff" {
		return NewPendingPhaseInfo(
			PendingPhasePullingImage,
			podInfo.Reason,
			podInfo.Message,
			now,
		)
	}

	// Also check the status field which may contain these values
	if podInfo.Status == "ContainerCreating" || podInfo.Status == "ImagePullBackOff" {
		return NewPendingPhaseInfo(
			PendingPhasePullingImage,
			podInfo.Status,
			podInfo.Message,
			now,
		)
	}

	return nil
}

// detectInitializing checks if the pod's init containers are running.
// Returns INITIALIZING phase if init containers are detected as running.
// This is detected when the pod status indicates init container activity.
func (d *PendingPhaseDetector) detectInitializing(podInfo *interfaces.PodInfo, now time.Time) *PendingPhaseInfo {
	// Check for init container related status
	// Common patterns include:
	// - Status: "Init:0/1", "Init:1/2", etc.
	// - Reason: "PodInitializing"
	if podInfo.Reason == "PodInitializing" {
		return NewPendingPhaseInfo(
			PendingPhaseInitializing,
			podInfo.Reason,
			podInfo.Message,
			now,
		)
	}

	// Check if status indicates init container activity
	// Kubernetes uses "Init:X/Y" format for init container progress
	if len(podInfo.Status) >= 4 && podInfo.Status[:4] == "Init" {
		return NewPendingPhaseInfo(
			PendingPhaseInitializing,
			"InitContainersRunning",
			podInfo.Message,
			now,
		)
	}

	return nil
}

// DetectPhaseFromPodDetail is a convenience method that extracts conditions from PodDetail
// and calls DetectPhase. This is useful when you have a full PodDetail object.
func (d *PendingPhaseDetector) DetectPhaseFromPodDetail(podDetail *interfaces.PodDetail) *PendingPhaseInfo {
	if podDetail == nil {
		return d.DetectPhase(nil, nil)
	}

	// Also check init container statuses from PodDetail for more accurate detection
	if phase := d.detectInitializingFromContainers(podDetail); phase != nil {
		return phase
	}

	// Also check container statuses from PodDetail for more accurate detection
	if phase := d.detectPullingImageFromContainers(podDetail); phase != nil {
		return phase
	}

	return d.DetectPhase(podDetail.PodInfo, podDetail.Conditions)
}

// detectInitializingFromContainers checks init container statuses directly.
// This provides more accurate detection when PodDetail is available.
func (d *PendingPhaseDetector) detectInitializingFromContainers(podDetail *interfaces.PodDetail) *PendingPhaseInfo {
	now := time.Now()

	for _, initContainer := range podDetail.InitContainers {
		// If any init container is in Running state, the pod is initializing
		if initContainer.State == "Running" {
			return NewPendingPhaseInfo(
				PendingPhaseInitializing,
				"InitContainerRunning",
				"Init container "+initContainer.Name+" is running",
				now,
			)
		}
		// If init container is waiting (not yet started), also consider as initializing
		if initContainer.State == "Waiting" && !initContainer.Ready {
			return NewPendingPhaseInfo(
				PendingPhaseInitializing,
				initContainer.Reason,
				initContainer.Message,
				now,
			)
		}
	}

	return nil
}

// detectPullingImageFromContainers checks container statuses directly for image pulling.
// This provides more accurate detection when PodDetail is available.
func (d *PendingPhaseDetector) detectPullingImageFromContainers(podDetail *interfaces.PodDetail) *PendingPhaseInfo {
	now := time.Now()

	// Check main containers
	for _, container := range podDetail.Containers {
		if container.State == "Waiting" {
			if container.Reason == "ContainerCreating" || container.Reason == "ImagePullBackOff" {
				return NewPendingPhaseInfo(
					PendingPhasePullingImage,
					container.Reason,
					container.Message,
					now,
				)
			}
		}
	}

	// Also check init containers for image pulling
	for _, initContainer := range podDetail.InitContainers {
		if initContainer.State == "Waiting" {
			if initContainer.Reason == "ContainerCreating" || initContainer.Reason == "ImagePullBackOff" {
				return NewPendingPhaseInfo(
					PendingPhasePullingImage,
					initContainer.Reason,
					initContainer.Message,
					now,
				)
			}
		}
	}

	return nil
}
