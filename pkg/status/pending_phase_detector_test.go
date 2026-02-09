package status

import (
	"testing"

	"waverless/pkg/interfaces"
)

// mockCapacityManager is a mock implementation of CapacityManager for testing
type mockCapacityManager struct {
	spotStatus *SpotStatus
}

func (m *mockCapacityManager) GetSpotStatus(instanceType string) *SpotStatus {
	return m.spotStatus
}

// TestDetectPhase_Unschedulable_ReturnsWaitingNode tests that Unschedulable condition
// results in WAITING_NODE phase.
func TestDetectPhase_Unschedulable_ReturnsWaitingNode(t *testing.T) {
	detector := NewPendingPhaseDetector(nil)

	podInfo := &interfaces.PodInfo{
		Name:   "test-pod",
		Phase:  "Pending",
		Status: "Pending",
	}

	podConditions := []interfaces.PodCondition{
		{
			Type:    "PodScheduled",
			Status:  "False",
			Reason:  "Unschedulable",
			Message: "0/3 nodes are available: 3 Insufficient nvidia.com/gpu.",
		},
	}

	result := detector.DetectPhase(podInfo, podConditions)

	if result.Phase != PendingPhaseWaitingNode {
		t.Errorf("Expected phase %s, got %s", PendingPhaseWaitingNode, result.Phase)
	}
	if result.Reason != "Unschedulable" {
		t.Errorf("Expected reason 'Unschedulable', got '%s'", result.Reason)
	}
}

// TestDetectPhase_ContainerCreating_ReturnsPullingImage tests that ContainerCreating status
// results in PULLING_IMAGE phase.
func TestDetectPhase_ContainerCreating_ReturnsPullingImage(t *testing.T) {
	detector := NewPendingPhaseDetector(nil)

	podInfo := &interfaces.PodInfo{
		Name:   "test-pod",
		Phase:  "Pending",
		Status: "ContainerCreating",
		Reason: "ContainerCreating",
	}

	podConditions := []interfaces.PodCondition{
		{
			Type:   "PodScheduled",
			Status: "True",
		},
	}

	result := detector.DetectPhase(podInfo, podConditions)

	if result.Phase != PendingPhasePullingImage {
		t.Errorf("Expected phase %s, got %s", PendingPhasePullingImage, result.Phase)
	}
}

// TestDetectPhase_ImagePullBackOff_ReturnsPullingImage tests that ImagePullBackOff status
// results in PULLING_IMAGE phase.
func TestDetectPhase_ImagePullBackOff_ReturnsPullingImage(t *testing.T) {
	detector := NewPendingPhaseDetector(nil)

	podInfo := &interfaces.PodInfo{
		Name:    "test-pod",
		Phase:   "Pending",
		Status:  "ImagePullBackOff",
		Reason:  "ImagePullBackOff",
		Message: "Back-off pulling image \"invalid-image:latest\"",
	}

	podConditions := []interfaces.PodCondition{
		{
			Type:   "PodScheduled",
			Status: "True",
		},
	}

	result := detector.DetectPhase(podInfo, podConditions)

	if result.Phase != PendingPhasePullingImage {
		t.Errorf("Expected phase %s, got %s", PendingPhasePullingImage, result.Phase)
	}
	if result.Reason != "ImagePullBackOff" {
		t.Errorf("Expected reason 'ImagePullBackOff', got '%s'", result.Reason)
	}
}

// TestDetectPhase_InitContainerRunning_ReturnsInitializing tests that init container running
// results in INITIALIZING phase.
func TestDetectPhase_InitContainerRunning_ReturnsInitializing(t *testing.T) {
	detector := NewPendingPhaseDetector(nil)

	podInfo := &interfaces.PodInfo{
		Name:   "test-pod",
		Phase:  "Pending",
		Status: "Init:0/1",
		Reason: "PodInitializing",
	}

	podConditions := []interfaces.PodCondition{
		{
			Type:   "PodScheduled",
			Status: "True",
		},
	}

	result := detector.DetectPhase(podInfo, podConditions)

	if result.Phase != PendingPhaseInitializing {
		t.Errorf("Expected phase %s, got %s", PendingPhaseInitializing, result.Phase)
	}
}

// TestDetectPhase_InitStatusFormat_ReturnsInitializing tests that "Init:X/Y" status format
// results in INITIALIZING phase.
func TestDetectPhase_InitStatusFormat_ReturnsInitializing(t *testing.T) {
	detector := NewPendingPhaseDetector(nil)

	podInfo := &interfaces.PodInfo{
		Name:   "test-pod",
		Phase:  "Pending",
		Status: "Init:1/2",
	}

	podConditions := []interfaces.PodCondition{
		{
			Type:   "PodScheduled",
			Status: "True",
		},
	}

	result := detector.DetectPhase(podInfo, podConditions)

	if result.Phase != PendingPhaseInitializing {
		t.Errorf("Expected phase %s, got %s", PendingPhaseInitializing, result.Phase)
	}
}

// TestDetectPhase_NoPodConditions_ReturnsScheduling tests that when no specific conditions
// are detected, the default SCHEDULING phase is returned.
func TestDetectPhase_NoPodConditions_ReturnsScheduling(t *testing.T) {
	detector := NewPendingPhaseDetector(nil)

	podInfo := &interfaces.PodInfo{
		Name:   "test-pod",
		Phase:  "Pending",
		Status: "Pending",
	}

	podConditions := []interfaces.PodCondition{}

	result := detector.DetectPhase(podInfo, podConditions)

	if result.Phase != PendingPhaseScheduling {
		t.Errorf("Expected phase %s, got %s", PendingPhaseScheduling, result.Phase)
	}
}

// TestDetectPhase_NilPodInfo_ReturnsScheduling tests that nil podInfo returns SCHEDULING phase.
func TestDetectPhase_NilPodInfo_ReturnsScheduling(t *testing.T) {
	detector := NewPendingPhaseDetector(nil)

	result := detector.DetectPhase(nil, nil)

	if result.Phase != PendingPhaseScheduling {
		t.Errorf("Expected phase %s, got %s", PendingPhaseScheduling, result.Phase)
	}
}

// TestDetectPhase_WaitingNode_WithSpotStatus tests that WAITING_NODE phase includes
// Spot status when capacity manager is available.
func TestDetectPhase_WaitingNode_WithSpotStatus(t *testing.T) {
	spotStatus := NewSpotStatus(5, 0.50, "g4dn.xlarge")
	mockCM := &mockCapacityManager{spotStatus: spotStatus}
	detector := NewPendingPhaseDetector(mockCM)

	podInfo := &interfaces.PodInfo{
		Name:   "test-pod",
		Phase:  "Pending",
		Status: "Pending",
	}

	podConditions := []interfaces.PodCondition{
		{
			Type:    "PodScheduled",
			Status:  "False",
			Reason:  "Unschedulable",
			Message: "0/3 nodes are available",
		},
	}

	result := detector.DetectPhase(podInfo, podConditions)

	if result.Phase != PendingPhaseWaitingNode {
		t.Errorf("Expected phase %s, got %s", PendingPhaseWaitingNode, result.Phase)
	}
	if result.SpotStatus == nil {
		t.Error("Expected SpotStatus to be set")
	} else {
		if result.SpotStatus.Score != 5 {
			t.Errorf("Expected SpotStatus.Score 5, got %d", result.SpotStatus.Score)
		}
		if result.SpotStatus.Capacity != SpotCapacityLimited {
			t.Errorf("Expected SpotStatus.Capacity %s, got %s", SpotCapacityLimited, result.SpotStatus.Capacity)
		}
	}
}

// TestDetectPhase_PriorityOrder tests that detection follows the correct priority order:
// 1. Unschedulable → WAITING_NODE
// 2. ContainerCreating/ImagePullBackOff → PULLING_IMAGE
// 3. Init containers → INITIALIZING
// 4. Default → SCHEDULING
func TestDetectPhase_PriorityOrder(t *testing.T) {
	detector := NewPendingPhaseDetector(nil)

	// Test case: Both Unschedulable and ContainerCreating - Unschedulable should win
	podInfo := &interfaces.PodInfo{
		Name:   "test-pod",
		Phase:  "Pending",
		Status: "ContainerCreating",
		Reason: "ContainerCreating",
	}

	podConditions := []interfaces.PodCondition{
		{
			Type:   "PodScheduled",
			Status: "False",
			Reason: "Unschedulable",
		},
	}

	result := detector.DetectPhase(podInfo, podConditions)

	if result.Phase != PendingPhaseWaitingNode {
		t.Errorf("Expected WAITING_NODE to take priority, got %s", result.Phase)
	}
}

// TestDetectPhaseFromPodDetail_WithContainerStatuses tests detection from PodDetail
// with container statuses.
func TestDetectPhaseFromPodDetail_WithContainerStatuses(t *testing.T) {
	detector := NewPendingPhaseDetector(nil)

	podDetail := &interfaces.PodDetail{
		PodInfo: &interfaces.PodInfo{
			Name:   "test-pod",
			Phase:  "Pending",
			Status: "Pending",
		},
		Containers: []interfaces.ContainerInfo{
			{
				Name:   "main",
				State:  "Waiting",
				Reason: "ContainerCreating",
			},
		},
		Conditions: []interfaces.PodCondition{
			{
				Type:   "PodScheduled",
				Status: "True",
			},
		},
	}

	result := detector.DetectPhaseFromPodDetail(podDetail)

	if result.Phase != PendingPhasePullingImage {
		t.Errorf("Expected phase %s, got %s", PendingPhasePullingImage, result.Phase)
	}
}

// TestDetectPhaseFromPodDetail_WithInitContainers tests detection from PodDetail
// with init container statuses.
func TestDetectPhaseFromPodDetail_WithInitContainers(t *testing.T) {
	detector := NewPendingPhaseDetector(nil)

	podDetail := &interfaces.PodDetail{
		PodInfo: &interfaces.PodInfo{
			Name:   "test-pod",
			Phase:  "Pending",
			Status: "Pending",
		},
		InitContainers: []interfaces.ContainerInfo{
			{
				Name:  "init-container",
				State: "Running",
				Ready: false,
			},
		},
		Containers: []interfaces.ContainerInfo{
			{
				Name:  "main",
				State: "Waiting",
				Ready: false,
			},
		},
		Conditions: []interfaces.PodCondition{
			{
				Type:   "PodScheduled",
				Status: "True",
			},
		},
	}

	result := detector.DetectPhaseFromPodDetail(podDetail)

	if result.Phase != PendingPhaseInitializing {
		t.Errorf("Expected phase %s, got %s", PendingPhaseInitializing, result.Phase)
	}
}

// TestDetectPhaseFromPodDetail_NilPodDetail tests that nil PodDetail returns SCHEDULING phase.
func TestDetectPhaseFromPodDetail_NilPodDetail(t *testing.T) {
	detector := NewPendingPhaseDetector(nil)

	result := detector.DetectPhaseFromPodDetail(nil)

	if result.Phase != PendingPhaseScheduling {
		t.Errorf("Expected phase %s, got %s", PendingPhaseScheduling, result.Phase)
	}
}

// TestDetectPhase_StatusFieldContainerCreating tests detection when Status field
// contains ContainerCreating (not Reason field).
func TestDetectPhase_StatusFieldContainerCreating(t *testing.T) {
	detector := NewPendingPhaseDetector(nil)

	podInfo := &interfaces.PodInfo{
		Name:   "test-pod",
		Phase:  "Pending",
		Status: "ContainerCreating",
		// Reason is empty
	}

	podConditions := []interfaces.PodCondition{
		{
			Type:   "PodScheduled",
			Status: "True",
		},
	}

	result := detector.DetectPhase(podInfo, podConditions)

	if result.Phase != PendingPhasePullingImage {
		t.Errorf("Expected phase %s, got %s", PendingPhasePullingImage, result.Phase)
	}
}

// TestDetectPhase_MultipleConditions tests detection with multiple pod conditions.
func TestDetectPhase_MultipleConditions(t *testing.T) {
	detector := NewPendingPhaseDetector(nil)

	podInfo := &interfaces.PodInfo{
		Name:   "test-pod",
		Phase:  "Pending",
		Status: "Pending",
	}

	podConditions := []interfaces.PodCondition{
		{
			Type:   "Initialized",
			Status: "False",
		},
		{
			Type:    "PodScheduled",
			Status:  "False",
			Reason:  "Unschedulable",
			Message: "No nodes available",
		},
		{
			Type:   "Ready",
			Status: "False",
		},
	}

	result := detector.DetectPhase(podInfo, podConditions)

	if result.Phase != PendingPhaseWaitingNode {
		t.Errorf("Expected phase %s, got %s", PendingPhaseWaitingNode, result.Phase)
	}
}
