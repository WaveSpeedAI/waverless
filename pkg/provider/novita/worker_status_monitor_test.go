package novita

import (
	"testing"

	"waverless/pkg/interfaces"

	"github.com/stretchr/testify/assert"
)

func TestNewNovitaWorkerStatusMonitor(t *testing.T) {
	monitor := NewNovitaWorkerStatusMonitor(nil, nil)
	assert.NotNil(t, monitor)
	assert.NotNil(t, monitor.sanitizer)
}

func TestClassifyNovitaFailure_ImagePull(t *testing.T) {
	monitor := NewNovitaWorkerStatusMonitor(nil, nil)

	testCases := []struct {
		name     string
		state    string
		errCode  string
		message  string
		expected interfaces.FailureType
	}{
		{
			name:     "image pull error code",
			state:    "failed",
			errCode:  "image_pull_failed",
			message:  "",
			expected: interfaces.FailureTypeImagePull,
		},
		{
			name:     "image not found in message",
			state:    "failed",
			errCode:  "",
			message:  "image not found in registry",
			expected: interfaces.FailureTypeImagePull,
		},
		{
			name:     "registry error",
			state:    "failed",
			errCode:  "registry_error",
			message:  "",
			expected: interfaces.FailureTypeImagePull,
		},
		{
			name:     "manifest not found",
			state:    "error",
			errCode:  "",
			message:  "manifest not found",
			expected: interfaces.FailureTypeImagePull,
		},
		{
			name:     "pull failed in message",
			state:    "failed",
			errCode:  "",
			message:  "failed to pull image",
			expected: interfaces.FailureTypeImagePull,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := monitor.ClassifyNovitaFailure(tc.state, tc.errCode, tc.message)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestClassifyNovitaFailure_ContainerCrash(t *testing.T) {
	monitor := NewNovitaWorkerStatusMonitor(nil, nil)

	testCases := []struct {
		name     string
		state    string
		errCode  string
		message  string
		expected interfaces.FailureType
	}{
		{
			name:     "crash error code",
			state:    "failed",
			errCode:  "container_crashed",
			message:  "",
			expected: interfaces.FailureTypeContainerCrash,
		},
		{
			name:     "exit error",
			state:    "failed",
			errCode:  "exit_error",
			message:  "",
			expected: interfaces.FailureTypeContainerCrash,
		},
		{
			name:     "oom killed",
			state:    "failed",
			errCode:  "oom_killed",
			message:  "",
			expected: interfaces.FailureTypeContainerCrash,
		},
		{
			name:     "container error in message",
			state:    "failed",
			errCode:  "",
			message:  "container error: process exited",
			expected: interfaces.FailureTypeContainerCrash,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := monitor.ClassifyNovitaFailure(tc.state, tc.errCode, tc.message)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestClassifyNovitaFailure_ResourceLimit(t *testing.T) {
	monitor := NewNovitaWorkerStatusMonitor(nil, nil)

	testCases := []struct {
		name     string
		state    string
		errCode  string
		message  string
		expected interfaces.FailureType
	}{
		{
			name:     "resource error code",
			state:    "failed",
			errCode:  "insufficient_resources",
			message:  "",
			expected: interfaces.FailureTypeResourceLimit,
		},
		{
			name:     "gpu unavailable",
			state:    "failed",
			errCode:  "gpu_unavailable",
			message:  "",
			expected: interfaces.FailureTypeResourceLimit,
		},
		{
			name:     "memory limit in message",
			state:    "failed",
			errCode:  "",
			message:  "memory limit exceeded",
			expected: interfaces.FailureTypeResourceLimit,
		},
		{
			name:     "quota exceeded",
			state:    "failed",
			errCode:  "quota_exceeded",
			message:  "",
			expected: interfaces.FailureTypeResourceLimit,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := monitor.ClassifyNovitaFailure(tc.state, tc.errCode, tc.message)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestClassifyNovitaFailure_Timeout(t *testing.T) {
	monitor := NewNovitaWorkerStatusMonitor(nil, nil)

	testCases := []struct {
		name     string
		state    string
		errCode  string
		message  string
		expected interfaces.FailureType
	}{
		{
			name:     "timeout error code",
			state:    "failed",
			errCode:  "timeout",
			message:  "",
			expected: interfaces.FailureTypeTimeout,
		},
		{
			name:     "deadline exceeded",
			state:    "failed",
			errCode:  "deadline_exceeded",
			message:  "",
			expected: interfaces.FailureTypeTimeout,
		},
		{
			name:     "timed out in message",
			state:    "failed",
			errCode:  "",
			message:  "operation timed out",
			expected: interfaces.FailureTypeTimeout,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := monitor.ClassifyNovitaFailure(tc.state, tc.errCode, tc.message)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestClassifyNovitaFailure_Unknown(t *testing.T) {
	monitor := NewNovitaWorkerStatusMonitor(nil, nil)

	testCases := []struct {
		name     string
		state    string
		errCode  string
		message  string
		expected interfaces.FailureType
	}{
		{
			name:     "generic failed state",
			state:    "failed",
			errCode:  "",
			message:  "unknown error occurred",
			expected: interfaces.FailureTypeUnknown,
		},
		{
			name:     "unrecognized error code",
			state:    "error",
			errCode:  "some_random_error",
			message:  "",
			expected: interfaces.FailureTypeUnknown,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := monitor.ClassifyNovitaFailure(tc.state, tc.errCode, tc.message)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestDetectFailure_NilInput(t *testing.T) {
	monitor := NewNovitaWorkerStatusMonitor(nil, nil)
	result := monitor.DetectFailure(nil)
	assert.Nil(t, result)
}

func TestDetectFailure_HealthyWorker(t *testing.T) {
	monitor := NewNovitaWorkerStatusMonitor(nil, nil)

	info := &interfaces.PodInfo{
		Name:   "worker-1",
		Phase:  "running",
		Status: "Running",
		Reason: "Ready",
	}

	result := monitor.DetectFailure(info)
	assert.Nil(t, result)
}

func TestDetectFailure_FailedWorker(t *testing.T) {
	monitor := NewNovitaWorkerStatusMonitor(nil, nil)

	info := &interfaces.PodInfo{
		Name:    "worker-1",
		Phase:   "failed",
		Status:  "Failed",
		Reason:  "image_pull_failed",
		Message: "failed to pull image: not found",
	}

	result := monitor.DetectFailure(info)
	assert.NotNil(t, result)
	assert.Equal(t, interfaces.FailureTypeImagePull, result.Type)
	assert.Equal(t, "image_pull_failed", result.Reason)
}

func TestDetectFailure_ErrorInMessage(t *testing.T) {
	monitor := NewNovitaWorkerStatusMonitor(nil, nil)

	info := &interfaces.PodInfo{
		Name:    "worker-1",
		Phase:   "running",
		Status:  "Running",
		Reason:  "",
		Message: "error: container crashed",
	}

	result := monitor.DetectFailure(info)
	assert.NotNil(t, result)
	assert.Equal(t, interfaces.FailureTypeContainerCrash, result.Type)
}

func TestContainsAny(t *testing.T) {
	testCases := []struct {
		name     string
		s        string
		substrs  []string
		expected bool
	}{
		{
			name:     "contains first",
			s:        "image pull failed",
			substrs:  []string{"image", "container"},
			expected: true,
		},
		{
			name:     "contains second",
			s:        "container crashed",
			substrs:  []string{"image", "container"},
			expected: true,
		},
		{
			name:     "contains none",
			s:        "unknown error",
			substrs:  []string{"image", "container"},
			expected: false,
		},
		{
			name:     "empty string",
			s:        "",
			substrs:  []string{"image", "container"},
			expected: false,
		},
		{
			name:     "empty substrs",
			s:        "some text",
			substrs:  []string{},
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := containsAny(tc.s, tc.substrs...)
			assert.Equal(t, tc.expected, result)
		})
	}
}
