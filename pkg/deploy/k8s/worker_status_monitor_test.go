package k8s

import (
	"testing"

	"waverless/pkg/interfaces"
)

// TestClassifyK8sFailure tests the ClassifyK8sFailure method.
// It verifies that K8s-specific error reasons are correctly mapped to generic FailureTypes.
//
// Validates: Requirements 3.2, 6.2
func TestClassifyK8sFailure(t *testing.T) {
	monitor := &K8sWorkerStatusMonitor{}

	tests := []struct {
		name     string
		reason   string
		message  string
		expected interfaces.FailureType
	}{
		// Image pull failures
		{
			name:     "ImagePullBackOff",
			reason:   "ImagePullBackOff",
			message:  "Back-off pulling image",
			expected: interfaces.FailureTypeImagePull,
		},
		{
			name:     "ErrImagePull",
			reason:   "ErrImagePull",
			message:  "Error pulling image",
			expected: interfaces.FailureTypeImagePull,
		},
		{
			name:     "InvalidImageName",
			reason:   "InvalidImageName",
			message:  "Invalid image name",
			expected: interfaces.FailureTypeImagePull,
		},
		{
			name:     "ImageInspectError",
			reason:   "ImageInspectError",
			message:  "Error inspecting image",
			expected: interfaces.FailureTypeImagePull,
		},
		{
			name:     "image pull error in message",
			reason:   "Failed",
			message:  "Failed to pull image nginx:invalid",
			expected: interfaces.FailureTypeImagePull,
		},
		{
			name:     "image not found in message",
			reason:   "Failed",
			message:  "image not found: nginx:nonexistent",
			expected: interfaces.FailureTypeImagePull,
		},
		{
			name:     "unauthorized image access",
			reason:   "Failed",
			message:  "unauthorized: authentication required for image",
			expected: interfaces.FailureTypeImagePull,
		},

		// Container crash failures
		{
			name:     "CrashLoopBackOff",
			reason:   "CrashLoopBackOff",
			message:  "Container keeps crashing",
			expected: interfaces.FailureTypeContainerCrash,
		},
		{
			name:     "Error",
			reason:   "Error",
			message:  "Container exited with error",
			expected: interfaces.FailureTypeContainerCrash,
		},
		{
			name:     "OOMKilled",
			reason:   "OOMKilled",
			message:  "Container killed due to OOM",
			expected: interfaces.FailureTypeContainerCrash,
		},
		{
			name:     "ContainerCannotRun",
			reason:   "ContainerCannotRun",
			message:  "Container cannot run",
			expected: interfaces.FailureTypeContainerCrash,
		},
		{
			name:     "CreateContainerError",
			reason:   "CreateContainerError",
			message:  "Error creating container",
			expected: interfaces.FailureTypeContainerCrash,
		},

		// Resource limit failures
		{
			name:     "OutOfMemory",
			reason:   "OutOfMemory",
			message:  "Node is out of memory",
			expected: interfaces.FailureTypeResourceLimit,
		},
		{
			name:     "OutOfCpu",
			reason:   "OutOfCpu",
			message:  "Node is out of CPU",
			expected: interfaces.FailureTypeResourceLimit,
		},
		{
			name:     "Unschedulable",
			reason:   "Unschedulable",
			message:  "Pod cannot be scheduled",
			expected: interfaces.FailureTypeResourceLimit,
		},
		{
			name:     "FailedScheduling",
			reason:   "FailedScheduling",
			message:  "Failed to schedule pod",
			expected: interfaces.FailureTypeResourceLimit,
		},
		{
			name:     "insufficient resources in message",
			reason:   "Failed",
			message:  "Insufficient memory to schedule pod",
			expected: interfaces.FailureTypeResourceLimit,
		},
		{
			name:     "no nodes available in message",
			reason:   "Failed",
			message:  "no nodes available to schedule pods",
			expected: interfaces.FailureTypeResourceLimit,
		},

		// Timeout failures
		{
			name:     "timeout in reason",
			reason:   "Timeout",
			message:  "Operation timed out",
			expected: interfaces.FailureTypeTimeout,
		},
		{
			name:     "timeout in message",
			reason:   "Failed",
			message:  "Operation timeout after 30s",
			expected: interfaces.FailureTypeTimeout,
		},

		// Unknown failures
		{
			name:     "unknown reason",
			reason:   "SomeUnknownReason",
			message:  "Some unknown error",
			expected: interfaces.FailureTypeUnknown,
		},
		{
			name:     "empty reason and message",
			reason:   "",
			message:  "",
			expected: interfaces.FailureTypeUnknown,
		},
		{
			name:     "normal running state",
			reason:   "Running",
			message:  "Container is running",
			expected: interfaces.FailureTypeUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := monitor.ClassifyK8sFailure(tt.reason, tt.message)
			if result != tt.expected {
				t.Errorf("ClassifyK8sFailure(%q, %q) = %v, want %v",
					tt.reason, tt.message, result, tt.expected)
			}
		})
	}
}

// TestIsFailureState tests the isFailureState function.
// It verifies that failure states are correctly identified.
func TestIsFailureState(t *testing.T) {
	tests := []struct {
		name     string
		reason   string
		status   string
		expected bool
	}{
		// Failure states
		{"ImagePullBackOff reason", "ImagePullBackOff", "", true},
		{"ErrImagePull reason", "ErrImagePull", "", true},
		{"CrashLoopBackOff reason", "CrashLoopBackOff", "", true},
		{"Error reason", "Error", "", true},
		{"OOMKilled reason", "OOMKilled", "", true},
		{"Unschedulable reason", "Unschedulable", "", true},
		{"Failed status", "", "Failed", true},
		{"Failed reason", "Failed", "", true},
		{"ImagePullBackOff status", "", "ImagePullBackOff", true},

		// Non-failure states
		{"Running status", "", "Running", false},
		{"Ready reason", "Ready", "", false},
		{"ContainerCreating reason", "ContainerCreating", "", false},
		{"Pending status", "", "Pending", false},
		{"empty", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isFailureState(tt.reason, tt.status)
			if result != tt.expected {
				t.Errorf("isFailureState(%q, %q) = %v, want %v",
					tt.reason, tt.status, result, tt.expected)
			}
		})
	}
}

// TestDetectFailure tests the detectFailure method.
// It verifies that failures are correctly detected from PodInfo.
func TestDetectFailure(t *testing.T) {
	monitor := &K8sWorkerStatusMonitor{}

	tests := []struct {
		name          string
		info          *interfaces.PodInfo
		expectFailure bool
		expectedType  interfaces.FailureType
	}{
		{
			name:          "nil info",
			info:          nil,
			expectFailure: false,
		},
		{
			name: "ImagePullBackOff failure",
			info: &interfaces.PodInfo{
				Name:    "test-pod",
				Reason:  "ImagePullBackOff",
				Message: "Back-off pulling image",
				Status:  "Waiting",
			},
			expectFailure: true,
			expectedType:  interfaces.FailureTypeImagePull,
		},
		{
			name: "CrashLoopBackOff failure",
			info: &interfaces.PodInfo{
				Name:    "test-pod",
				Reason:  "CrashLoopBackOff",
				Message: "Container keeps crashing",
				Status:  "Waiting",
			},
			expectFailure: true,
			expectedType:  interfaces.FailureTypeContainerCrash,
		},
		{
			name: "Running state - no failure",
			info: &interfaces.PodInfo{
				Name:    "test-pod",
				Reason:  "Ready",
				Message: "All containers are ready",
				Status:  "Running",
			},
			expectFailure: false,
		},
		{
			name: "ContainerCreating - no failure",
			info: &interfaces.PodInfo{
				Name:    "test-pod",
				Reason:  "ContainerCreating",
				Message: "Container is being created",
				Status:  "Pending",
			},
			expectFailure: false,
		},
		{
			name: "Failure in status field",
			info: &interfaces.PodInfo{
				Name:    "test-pod",
				Reason:  "",
				Message: "Error pulling image",
				Status:  "ErrImagePull",
			},
			expectFailure: true,
			expectedType:  interfaces.FailureTypeImagePull,
		},
		// DeletionTimestamp tests - pods being gracefully terminated should NOT be marked as failures
		{
			name: "Error reason with DeletionTimestamp - graceful termination, not a crash",
			info: &interfaces.PodInfo{
				Name:              "test-pod",
				Reason:            "Error",
				Message:           "Container exited with error",
				Status:            "Terminated",
				Phase:             "Failed",
				DeletionTimestamp: "2026-02-01T13:30:47Z",
			},
			expectFailure: false, // Should NOT be marked as failure because DeletionTimestamp is set
		},
		{
			name: "CrashLoopBackOff with DeletionTimestamp - pod being deleted",
			info: &interfaces.PodInfo{
				Name:              "test-pod",
				Reason:            "CrashLoopBackOff",
				Message:           "Container keeps crashing",
				Status:            "Waiting",
				DeletionTimestamp: "2026-02-01T13:30:47Z",
			},
			expectFailure: false, // Should NOT be marked as failure because DeletionTimestamp is set
		},
		{
			name: "ImagePullBackOff with DeletionTimestamp - pod being deleted",
			info: &interfaces.PodInfo{
				Name:              "test-pod",
				Reason:            "ImagePullBackOff",
				Message:           "Back-off pulling image",
				Status:            "Waiting",
				DeletionTimestamp: "2026-02-01T13:30:47Z",
			},
			expectFailure: false, // Should NOT be marked as failure because DeletionTimestamp is set
		},
		{
			name: "Error reason without DeletionTimestamp - real crash",
			info: &interfaces.PodInfo{
				Name:              "test-pod",
				Reason:            "Error",
				Message:           "Container exited with error",
				Status:            "Terminated",
				Phase:             "Failed",
				DeletionTimestamp: "", // Empty - not being deleted
			},
			expectFailure: true,
			expectedType:  interfaces.FailureTypeContainerCrash,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := monitor.DetectFailure(tt.info)
			if tt.expectFailure {
				if result == nil {
					t.Errorf("detectFailure() returned nil, expected failure")
					return
				}
				if result.Type != tt.expectedType {
					t.Errorf("detectFailure().Type = %v, want %v", result.Type, tt.expectedType)
				}
			} else {
				if result != nil {
					t.Errorf("detectFailure() returned %v, expected nil", result)
				}
			}
		})
	}
}
