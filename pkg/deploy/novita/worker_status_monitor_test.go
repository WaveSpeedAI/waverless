package novita

import (
	"context"
	"sync"
	"testing"
	"time"

	"waverless/pkg/interfaces"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockClientForStatusMonitor implements clientInterface for testing
type mockClientForStatusMonitor struct {
	endpoints *ListEndpointsResponse
	err       error
	callCount int
	mu        sync.Mutex
}

func (m *mockClientForStatusMonitor) CreateEndpoint(ctx context.Context, req *CreateEndpointRequest) (*CreateEndpointResponse, error) {
	return nil, nil
}

func (m *mockClientForStatusMonitor) GetEndpoint(ctx context.Context, endpointID string) (*GetEndpointResponse, error) {
	return nil, nil
}

func (m *mockClientForStatusMonitor) ListEndpoints(ctx context.Context) (*ListEndpointsResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callCount++
	if m.err != nil {
		return nil, m.err
	}
	return m.endpoints, nil
}

func (m *mockClientForStatusMonitor) UpdateEndpoint(ctx context.Context, req *UpdateEndpointRequest) error {
	return nil
}

func (m *mockClientForStatusMonitor) DeleteEndpoint(ctx context.Context, endpointID string) error {
	return nil
}

func (m *mockClientForStatusMonitor) CreateRegistryAuth(ctx context.Context, req *CreateRegistryAuthRequest) (*CreateRegistryAuthResponse, error) {
	return nil, nil
}

func (m *mockClientForStatusMonitor) ListRegistryAuths(ctx context.Context) (*ListRegistryAuthsResponse, error) {
	return nil, nil
}

func (m *mockClientForStatusMonitor) DeleteRegistryAuth(ctx context.Context, authID string) error {
	return nil
}

func (m *mockClientForStatusMonitor) getCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callCount
}

func TestNewNovitaWorkerStatusMonitor(t *testing.T) {
	client := &mockClientForStatusMonitor{}
	monitor := NewNovitaWorkerStatusMonitor(client, nil)

	assert.NotNil(t, monitor)
	assert.Equal(t, DefaultPollInterval, monitor.GetPollInterval())
	assert.NotNil(t, monitor.GetSanitizer())
}

func TestNewNovitaWorkerStatusMonitorWithInterval(t *testing.T) {
	client := &mockClientForStatusMonitor{}

	// Test with valid interval
	monitor := NewNovitaWorkerStatusMonitorWithInterval(client, nil, 10*time.Second)
	assert.Equal(t, 10*time.Second, monitor.GetPollInterval())

	// Test with zero interval (should use default)
	monitor = NewNovitaWorkerStatusMonitorWithInterval(client, nil, 0)
	assert.Equal(t, DefaultPollInterval, monitor.GetPollInterval())

	// Test with negative interval (should use default)
	monitor = NewNovitaWorkerStatusMonitorWithInterval(client, nil, -5*time.Second)
	assert.Equal(t, DefaultPollInterval, monitor.GetPollInterval())
}

func TestClassifyNovitaFailure_ImagePull(t *testing.T) {
	monitor := NewNovitaWorkerStatusMonitor(&mockClientForStatusMonitor{}, nil)

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
	monitor := NewNovitaWorkerStatusMonitor(&mockClientForStatusMonitor{}, nil)

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
	monitor := NewNovitaWorkerStatusMonitor(&mockClientForStatusMonitor{}, nil)

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
	monitor := NewNovitaWorkerStatusMonitor(&mockClientForStatusMonitor{}, nil)

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
	monitor := NewNovitaWorkerStatusMonitor(&mockClientForStatusMonitor{}, nil)

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

func TestWatchWorkerStatus_NilCallback(t *testing.T) {
	client := &mockClientForStatusMonitor{}
	monitor := NewNovitaWorkerStatusMonitor(client, nil)

	err := monitor.WatchWorkerStatus(context.Background(), nil)
	assert.NoError(t, err)
}

func TestWatchWorkerStatus_DetectsFailure(t *testing.T) {
	client := &mockClientForStatusMonitor{
		endpoints: &ListEndpointsResponse{
			Endpoints: []EndpointListItem{
				{
					ID:      "ep-1",
					Name:    "test-endpoint",
					AppName: "test-app",
					State: StateInfo{
						State: "serving",
					},
					Workers: []WorkerInfo{
						{
							ID: "worker-1",
							State: StateInfo{
								State:   "failed",
								Error:   "image_pull_failed",
								Message: "failed to pull image: not found",
							},
							Healthy: false,
						},
					},
				},
			},
		},
	}

	monitor := NewNovitaWorkerStatusMonitorWithInterval(client, nil, 50*time.Millisecond)

	var receivedFailures []*interfaces.WorkerFailureInfo
	var receivedWorkerIDs []string
	var receivedEndpoints []string
	var mu sync.Mutex

	callback := func(workerID, endpoint string, info *interfaces.WorkerFailureInfo) {
		mu.Lock()
		defer mu.Unlock()
		receivedWorkerIDs = append(receivedWorkerIDs, workerID)
		receivedEndpoints = append(receivedEndpoints, endpoint)
		receivedFailures = append(receivedFailures, info)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	go func() {
		_ = monitor.WatchWorkerStatus(ctx, callback)
	}()

	// Wait for at least one poll cycle
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	require.GreaterOrEqual(t, len(receivedFailures), 1, "Should have received at least one failure")
	assert.Equal(t, "worker-1", receivedWorkerIDs[0])
	assert.Equal(t, "test-endpoint", receivedEndpoints[0])
	assert.Equal(t, interfaces.FailureTypeImagePull, receivedFailures[0].Type)
}

func TestWatchWorkerStatus_NoCallbackForHealthyWorkers(t *testing.T) {
	client := &mockClientForStatusMonitor{
		endpoints: &ListEndpointsResponse{
			Endpoints: []EndpointListItem{
				{
					ID:      "ep-1",
					Name:    "test-endpoint",
					AppName: "test-app",
					State: StateInfo{
						State: "serving",
					},
					Workers: []WorkerInfo{
						{
							ID: "worker-1",
							State: StateInfo{
								State: "running",
							},
							Healthy: true,
						},
					},
				},
			},
		},
	}

	monitor := NewNovitaWorkerStatusMonitorWithInterval(client, nil, 50*time.Millisecond)

	callbackCalled := false
	callback := func(workerID, endpoint string, info *interfaces.WorkerFailureInfo) {
		callbackCalled = true
	}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	go func() {
		_ = monitor.WatchWorkerStatus(ctx, callback)
	}()

	// Wait for poll cycles
	time.Sleep(100 * time.Millisecond)

	assert.False(t, callbackCalled, "Callback should not be called for healthy workers")
}

func TestWatchWorkerStatus_DetectsEndpointFailure(t *testing.T) {
	client := &mockClientForStatusMonitor{
		endpoints: &ListEndpointsResponse{
			Endpoints: []EndpointListItem{
				{
					ID:      "ep-1",
					Name:    "test-endpoint",
					AppName: "test-app",
					State: StateInfo{
						State:   "failed",
						Error:   "image_not_found",
						Message: "image does not exist",
					},
					Workers: []WorkerInfo{},
				},
			},
		},
	}

	monitor := NewNovitaWorkerStatusMonitorWithInterval(client, nil, 50*time.Millisecond)

	var receivedWorkerID string
	var receivedEndpoint string
	var receivedFailure *interfaces.WorkerFailureInfo
	var mu sync.Mutex

	callback := func(workerID, endpoint string, info *interfaces.WorkerFailureInfo) {
		mu.Lock()
		defer mu.Unlock()
		receivedWorkerID = workerID
		receivedEndpoint = endpoint
		receivedFailure = info
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	go func() {
		_ = monitor.WatchWorkerStatus(ctx, callback)
	}()

	// Wait for poll cycle
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	require.NotNil(t, receivedFailure, "Should have received a failure")
	assert.Equal(t, "endpoint-test-endpoint", receivedWorkerID)
	assert.Equal(t, "test-endpoint", receivedEndpoint)
	assert.Equal(t, interfaces.FailureTypeImagePull, receivedFailure.Type)
}

func TestWatchWorkerStatus_NoDuplicateCallbacks(t *testing.T) {
	client := &mockClientForStatusMonitor{
		endpoints: &ListEndpointsResponse{
			Endpoints: []EndpointListItem{
				{
					ID:      "ep-1",
					Name:    "test-endpoint",
					AppName: "test-app",
					State: StateInfo{
						State: "serving",
					},
					Workers: []WorkerInfo{
						{
							ID: "worker-1",
							State: StateInfo{
								State:   "failed",
								Error:   "crash",
								Message: "container crashed",
							},
							Healthy: false,
						},
					},
				},
			},
		},
	}

	monitor := NewNovitaWorkerStatusMonitorWithInterval(client, nil, 30*time.Millisecond)

	callbackCount := 0
	var mu sync.Mutex

	callback := func(workerID, endpoint string, info *interfaces.WorkerFailureInfo) {
		mu.Lock()
		defer mu.Unlock()
		callbackCount++
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	go func() {
		_ = monitor.WatchWorkerStatus(ctx, callback)
	}()

	// Wait for multiple poll cycles
	time.Sleep(150 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	// Should only be called once since state doesn't change
	assert.Equal(t, 1, callbackCount, "Callback should only be called once for the same failure state")
}

func TestIsWorkerFailed(t *testing.T) {
	monitor := NewNovitaWorkerStatusMonitor(&mockClientForStatusMonitor{}, nil)

	testCases := []struct {
		name     string
		worker   *WorkerInfo
		expected bool
	}{
		{
			name:     "nil worker",
			worker:   nil,
			expected: false,
		},
		{
			name: "healthy running worker",
			worker: &WorkerInfo{
				ID:      "w1",
				State:   StateInfo{State: "running"},
				Healthy: true,
			},
			expected: false,
		},
		{
			name: "failed state",
			worker: &WorkerInfo{
				ID:      "w1",
				State:   StateInfo{State: "failed"},
				Healthy: false,
			},
			expected: true,
		},
		{
			name: "error state",
			worker: &WorkerInfo{
				ID:      "w1",
				State:   StateInfo{State: "error"},
				Healthy: false,
			},
			expected: true,
		},
		{
			name: "has error code",
			worker: &WorkerInfo{
				ID:      "w1",
				State:   StateInfo{State: "running", Error: "some_error"},
				Healthy: true,
			},
			expected: true,
		},
		{
			name: "unhealthy with error message",
			worker: &WorkerInfo{
				ID:      "w1",
				State:   StateInfo{State: "running", Message: "error occurred"},
				Healthy: false,
			},
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := monitor.isWorkerFailed(tc.worker)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestClearWorkerStates(t *testing.T) {
	monitor := NewNovitaWorkerStatusMonitor(&mockClientForStatusMonitor{}, nil)

	// Add some states
	monitor.workerStates.Store("worker-1", &workerState{State: "failed"})
	monitor.workerStates.Store("worker-2", &workerState{State: "running"})

	// Verify states exist
	_, ok1 := monitor.workerStates.Load("worker-1")
	_, ok2 := monitor.workerStates.Load("worker-2")
	assert.True(t, ok1)
	assert.True(t, ok2)

	// Clear states
	monitor.ClearWorkerStates()

	// Verify states are cleared
	_, ok1 = monitor.workerStates.Load("worker-1")
	_, ok2 = monitor.workerStates.Load("worker-2")
	assert.False(t, ok1)
	assert.False(t, ok2)
}

func TestSetPollInterval(t *testing.T) {
	monitor := NewNovitaWorkerStatusMonitor(&mockClientForStatusMonitor{}, nil)

	// Test setting valid interval
	monitor.SetPollInterval(10 * time.Second)
	assert.Equal(t, 10*time.Second, monitor.GetPollInterval())

	// Test setting zero interval (should not change)
	monitor.SetPollInterval(0)
	assert.Equal(t, 10*time.Second, monitor.GetPollInterval())

	// Test setting negative interval (should not change)
	monitor.SetPollInterval(-5 * time.Second)
	assert.Equal(t, 10*time.Second, monitor.GetPollInterval())
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
