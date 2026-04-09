package gmi

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMapPhaseToStatus(t *testing.T) {
	tests := []struct {
		phase    string
		expected string
	}{
		{"Running", "RUNNING"},
		{"Deploying", "DEPLOYING"},
		{"Draining", "DRAINING"},
		{"Failed", "FAILED"},
		// All pending variants
		{"", "PENDING"},
		{"Pending", "PENDING"},
		{"Scheduling", "PENDING"},
		{"WaitingForGPU", "PENDING"},
		{"Planned", "PENDING"},
		// Unknown
		{"SomeRandomPhase", "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.phase, func(t *testing.T) {
			assert.Equal(t, tt.expected, mapPhaseToStatus(tt.phase))
		})
	}
}

func TestConvertBFFModelToAppInfo(t *testing.T) {
	model := &bffModelResponse{
		Name:              "test-model",
		Model:             "test-model",
		Status:            "Running",
		Image:             "nginx:latest",
		DesiredReplicas:   3,
		CurrentReplicas:   3,
		ReadyReplicas:     2,
		AvailableReplicas: 2,
		CreatedAt:         "2026-03-26T00:00:00Z",
		ShmSize:           "1Gi",
	}

	app := convertBFFModelToAppInfo(model)

	assert.Equal(t, "test-model", app.Name)
	assert.Equal(t, "RUNNING", app.Status)
	assert.Equal(t, "nginx:latest", app.Image)
	assert.Equal(t, int32(3), app.Replicas)
	assert.Equal(t, int32(2), app.ReadyReplicas)
	assert.Equal(t, int32(2), app.AvailableReplicas)
	assert.Equal(t, "2026-03-26T00:00:00Z", app.CreatedAt)
	assert.Equal(t, "1Gi", app.ShmSize)
	assert.NotNil(t, app.Labels)
}

func TestConvertBFFModelToAppInfo_PendingPhase(t *testing.T) {
	model := &bffModelResponse{
		Name:   "pending-model",
		Status: "Scheduling",
	}

	app := convertBFFModelToAppInfo(model)
	assert.Equal(t, "PENDING", app.Status)
}

func TestConvertBFFPodToPodInfo(t *testing.T) {
	// Ready pod → status should be "Running"
	pod := &bffPodStatus{
		PodName:           "test-pod-abc",
		NodeID:            "node-1",
		ClusterID:         "us-west",
		Phase:             "Running",
		Ready:             true,
		CreatedAt:         "2026-03-26T00:00:00Z",
		DeletionTimestamp: "",
	}

	info := convertBFFPodToPodInfo(pod)

	assert.Equal(t, "test-pod-abc", info.Name)
	assert.Equal(t, "test-pod-abc", info.WorkerID)
	assert.Equal(t, "node-1", info.NodeName)
	assert.Equal(t, "Running", info.Phase)
	assert.Equal(t, "Running", info.Status) // Ready=true → "Running"
	assert.Equal(t, "2026-03-26T00:00:00Z", info.CreatedAt)
	assert.Empty(t, info.DeletionTimestamp)
}

func TestConvertBFFPodToPodInfo_NotReady(t *testing.T) {
	pod := &bffPodStatus{
		PodName: "pending-pod",
		Phase:   "Pending",
		Ready:   false,
	}

	info := convertBFFPodToPodInfo(pod)
	assert.Equal(t, "Pending", info.Status) // Not ready → use Phase as status
}

func TestConvertBFFPodToPodInfo_Terminating(t *testing.T) {
	pod := &bffPodStatus{
		PodName:           "dying-pod",
		Phase:             "Running",
		Ready:             true,
		DeletionTimestamp: "2026-03-26T01:00:00Z",
	}

	info := convertBFFPodToPodInfo(pod)
	assert.Equal(t, "2026-03-26T01:00:00Z", info.DeletionTimestamp)
}

func TestBuildBFFGPUProfile_KnownSpec(t *testing.T) {
	profile := buildBFFGPUProfile("h100-single", 1)

	assert.Equal(t, 1, profile["gpuCountPerPod"])
	assert.Equal(t, []string{"H100"}, profile["acceptedFamilies"])
	assert.Equal(t, "9.0", profile["minCC"])
	assert.Equal(t, "H100", profile["optimalFamily"])
}

func TestBuildBFFGPUProfile_A100(t *testing.T) {
	profile := buildBFFGPUProfile("a100-single", 2)

	assert.Equal(t, 2, profile["gpuCountPerPod"])
	assert.Equal(t, []string{"A100"}, profile["acceptedFamilies"])
	assert.Equal(t, "8.0", profile["minCC"])
}

func TestBuildBFFGPUProfile_UnknownSpec(t *testing.T) {
	profile := buildBFFGPUProfile("custom-gpu", 1)

	assert.Equal(t, 1, profile["gpuCountPerPod"])
	assert.Equal(t, []string{"custom-gpu"}, profile["acceptedFamilies"])
	assert.Equal(t, "7.0", profile["minCC"])
}

func TestBuildBFFGPUProfile_NoSpec(t *testing.T) {
	profile := buildBFFGPUProfile("", 1)

	assert.Equal(t, 1, profile["gpuCountPerPod"])
	assert.Equal(t, []string{"H100"}, profile["acceptedFamilies"])
	assert.Equal(t, "7.0", profile["minCC"])
}

func TestBuildBFFGPUProfile_CPUOnly(t *testing.T) {
	profile := buildBFFGPUProfile("h100-single", 0)

	assert.Equal(t, 0, profile["gpuCountPerPod"])
	assert.Nil(t, profile["acceptedFamilies"])
	assert.Nil(t, profile["minCC"])
}
