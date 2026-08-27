package gmi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"waverless/pkg/interfaces"
)

// bffEnvelope wraps data in a BFF response envelope.
func bffEnvelope(data any) []byte {
	raw, _ := json.Marshal(data)
	resp := bffResponse{Msg: "success", Data: raw}
	b, _ := json.Marshal(resp)
	return b
}

// newTestProvider creates a GMIDeploymentProvider pointing at the given httptest.Server.
func newTestProvider(ts *httptest.Server) *GMIDeploymentProvider {
	return &GMIDeploymentProvider{
		baseURL:               ts.URL,
		client:                ts.Client(),
		workerStatusCallbacks: make(map[uint64]WorkerStatusChangeCallback),
		workerDeleteCallbacks: make(map[uint64]WorkerDeleteCallback),
	}
}

// --- selectWorkersToDrain ---

func TestSelectWorkersToDrain_Empty(t *testing.T) {
	p := &GMIDeploymentProvider{}
	assert.Nil(t, p.selectWorkersToDrain(nil, 1))
	assert.Nil(t, p.selectWorkersToDrain([]*interfaces.PodInfo{}, 1))
}

func TestSelectWorkersToDrain_ZeroCount(t *testing.T) {
	p := &GMIDeploymentProvider{}
	pods := []*interfaces.PodInfo{{Name: "pod-1"}}
	assert.Nil(t, p.selectWorkersToDrain(pods, 0))
}

func TestSelectWorkersToDrain_PrefersUnhealthy(t *testing.T) {
	p := &GMIDeploymentProvider{}
	pods := []*interfaces.PodInfo{
		{Name: "healthy-pod", Phase: "Running", Status: "Running"},
		{Name: "unhealthy-pod", Phase: "Pending", Status: "Pending"},
	}

	result := p.selectWorkersToDrain(pods, 1)
	assert.Equal(t, []string{"unhealthy-pod"}, result)
}

func TestSelectWorkersToDrain_AllHealthy(t *testing.T) {
	p := &GMIDeploymentProvider{}
	pods := []*interfaces.PodInfo{
		{Name: "pod-a", Phase: "Running", Status: "Running"},
		{Name: "pod-b", Phase: "Running", Status: "Running"},
	}

	result := p.selectWorkersToDrain(pods, 1)
	assert.Len(t, result, 1)
}

func TestSelectWorkersToDrain_CountExceedsPods(t *testing.T) {
	p := &GMIDeploymentProvider{}
	pods := []*interfaces.PodInfo{
		{Name: "pod-a", Phase: "Running", Status: "Running"},
		{Name: "pod-b", Phase: "Running", Status: "Running"},
	}

	result := p.selectWorkersToDrain(pods, 5)
	assert.Len(t, result, 2)
}

func TestSelectWorkersToDrain_SkipsTerminating(t *testing.T) {
	p := &GMIDeploymentProvider{}
	pods := []*interfaces.PodInfo{
		{Name: "terminating", Phase: "Running", Status: "Running", DeletionTimestamp: "2026-01-01T00:00:00Z"},
		{Name: "draining", Phase: "Running", Status: "Draining"},
		{Name: "alive", Phase: "Running", Status: "Running"},
	}

	result := p.selectWorkersToDrain(pods, 2)
	assert.Equal(t, []string{"alive"}, result)
}

// --- DrainWorkers ---

func TestDrainWorkers(t *testing.T) {
	var receivedPath string
	var receivedBody map[string]any

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.Write(bffEnvelope(map[string]any{"drained": []string{"pod-1"}, "notFound": []string{}}))
	}))
	defer ts.Close()

	p := newTestProvider(ts)
	err := p.DrainWorkers(context.Background(), "test-model", []string{"pod-1"})

	require.NoError(t, err)
	assert.Equal(t, "/api/v1/models/test-model/drain", receivedPath)
	assert.Equal(t, []any{"pod-1"}, receivedBody["workerIds"])
}

func TestDrainWorkers_Empty(t *testing.T) {
	p := &GMIDeploymentProvider{}
	err := p.DrainWorkers(context.Background(), "test", nil)
	assert.NoError(t, err)
}

func TestDrainWorkers_NilDrainingStore(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(bffEnvelope(map[string]any{"drained": []string{"pod-1"}, "notFound": []string{}}))
	}))
	defer ts.Close()

	p := newTestProvider(ts)
	// drainingStore is nil — should not panic
	err := p.DrainWorkers(context.Background(), "test-model", []string{"pod-1"})
	assert.NoError(t, err)
}

// --- TerminateWorker ---

func TestTerminateWorker(t *testing.T) {
	var receivedBody map[string]any

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.Write(bffEnvelope(map[string]any{"drained": []string{"pod-1"}, "notFound": []string{}}))
	}))
	defer ts.Close()

	p := newTestProvider(ts)
	err := p.TerminateWorker(context.Background(), "test-model", "pod-1", "image_validation_failed")

	require.NoError(t, err)
	assert.Equal(t, []any{"pod-1"}, receivedBody["workerIds"])
}

// --- ScaleApp drain-first ---

func TestScaleApp_DrainFirst(t *testing.T) {
	var calls []string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, fmt.Sprintf("%s %s", r.Method, r.URL.Path))

		switch {
		case r.Method == "GET" && r.URL.Path == "/api/v1/models/test":
			// GetApp returns 2 replicas
			w.Write(bffEnvelope(bffModelResponse{
				Name:            "test",
				Status:          "Running",
				DesiredReplicas: 2,
				ReadyReplicas:   2,
				Pods: []bffPodStatus{
					{PodName: "test-pod-1", Phase: "Running", Ready: true},
					{PodName: "test-pod-2", Phase: "Running", Ready: true},
				},
			}))
		case r.Method == "GET" && r.URL.Path == "/api/v1/models":
			// ListApps for GetPods
			w.Write(bffEnvelope([]bffModelResponse{}))
		case r.URL.Path == "/api/v1/models/test/drain":
			w.Write(bffEnvelope(map[string]any{"drained": []string{"test-pod-1"}, "notFound": []string{}}))
		case r.URL.Path == "/api/v1/models/test/scale":
			w.Write(bffEnvelope(map[string]any{"status": "ok"}))
		default:
			w.WriteHeader(404)
		}
	}))
	defer ts.Close()

	p := newTestProvider(ts)
	err := p.ScaleApp(context.Background(), "test", 1)
	require.NoError(t, err)

	// Verify drain was called BEFORE scale
	assert.Contains(t, calls, "POST /api/v1/models/test/drain")
	assert.Contains(t, calls, "POST /api/v1/models/test/scale")

	drainIdx, scaleIdx := -1, -1
	for i, c := range calls {
		if c == "POST /api/v1/models/test/drain" {
			drainIdx = i
		}
		if c == "POST /api/v1/models/test/scale" {
			scaleIdx = i
		}
	}
	assert.Greater(t, scaleIdx, drainIdx, "scale should happen after drain")
}

func TestScaleApp_ScaleUp_NoDrain(t *testing.T) {
	var drainCalled bool

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/models/test/drain" {
			drainCalled = true
		}

		switch {
		case r.Method == "GET" && r.URL.Path == "/api/v1/models/test":
			w.Write(bffEnvelope(bffModelResponse{
				Name:            "test",
				Status:          "Running",
				DesiredReplicas: 1,
				ReadyReplicas:   1,
			}))
		case r.URL.Path == "/api/v1/models/test/scale":
			w.Write(bffEnvelope(map[string]any{"status": "ok"}))
		default:
			w.Write(bffEnvelope(nil))
		}
	}))
	defer ts.Close()

	p := newTestProvider(ts)
	err := p.ScaleApp(context.Background(), "test", 3)
	require.NoError(t, err)
	assert.False(t, drainCalled, "drain should NOT be called on scale-up")
}

// --- IsPodTerminating ---

func TestIsPodTerminating_Empty(t *testing.T) {
	p := &GMIDeploymentProvider{}
	result, err := p.IsPodTerminating(context.Background(), "")
	assert.NoError(t, err)
	assert.False(t, result)
}

func TestIsPodTerminating_LocalCache(t *testing.T) {
	p := &GMIDeploymentProvider{}
	p.workerStates.Store("test-pod", &gmiWorkerState{
		DeletionTimestamp: "2026-01-01T00:00:00Z",
	})

	result, err := p.IsPodTerminating(context.Background(), "test-pod")
	assert.NoError(t, err)
	assert.True(t, result)
}

func TestIsPodTerminating_NoDeletion_NotTerminating(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(bffEnvelope([]bffModelResponse{}))
	}))
	defer ts.Close()

	p := newTestProvider(ts)
	p.workerStates.Store("test-pod", &gmiWorkerState{
		DeletionTimestamp: "",
	})

	result, err := p.IsPodTerminating(context.Background(), "test-pod")
	assert.NoError(t, err)
	assert.False(t, result)
}

// --- processPodStateChange detects DeletionTimestamp ---

func TestProcessPodStateChange_DetectsDeletionTimestamp(t *testing.T) {
	callbackCh := make(chan bool, 1)
	p := &GMIDeploymentProvider{
		workerStatusCallbacks: map[uint64]WorkerStatusChangeCallback{
			1: func(workerID, endpoint string, info *interfaces.PodInfo) {
				callbackCh <- true
			},
		},
		workerStatusCallbacksLock: sync.RWMutex{},
		workerDeleteCallbacks:     make(map[uint64]WorkerDeleteCallback),
	}

	// First: store a state without DeletionTimestamp
	p.workerStates.Store("pod-1", &gmiWorkerState{
		ID:       "pod-1",
		Endpoint: "test",
		Status:   "Running",
	})

	// Second: process with DeletionTimestamp set (same Phase)
	pod := &bffPodStatus{
		PodName:           "pod-1",
		Phase:             "Running",
		Ready:             true,
		DeletionTimestamp: "2026-01-01T00:00:00Z",
	}
	p.processPodStateChange("test", "pod-1", pod)

	// Callback is async (goroutine) — wait for it
	select {
	case <-callbackCh:
		// OK
	case <-time.After(2 * time.Second):
		t.Fatal("callback should fire on DeletionTimestamp change")
	}

	// Verify state updated
	stateI, _ := p.workerStates.Load("pod-1")
	state := stateI.(*gmiWorkerState)
	assert.Equal(t, "2026-01-01T00:00:00Z", state.DeletionTimestamp)
}

// --- detectDeletedWorkers passes DeletionTimestamp ---

func TestDetectDeletedWorkers_PassesDeletionTimestamp(t *testing.T) {
	deletedAtCh := make(chan *time.Time, 1)
	p := &GMIDeploymentProvider{
		workerStatusCallbacks: make(map[uint64]WorkerStatusChangeCallback),
		workerDeleteCallbacks: map[uint64]WorkerDeleteCallback{
			1: func(workerID, endpoint string, deletedAt *time.Time) {
				deletedAtCh <- deletedAt
			},
		},
		workerDeleteCallbacksLock: sync.RWMutex{},
	}

	// Store a worker with DeletionTimestamp
	p.workerStates.Store("pod-1", &gmiWorkerState{
		ID:                "pod-1",
		Endpoint:          "test-model",
		Status:            "Running",
		DeletionTimestamp: "2026-01-15T12:30:00Z",
	})

	// Detect with empty current set → pod-1 is deleted
	p.detectDeletedWorkers(map[string]bool{})

	select {
	case deletedAt := <-deletedAtCh:
		require.NotNil(t, deletedAt)
		expected, _ := time.Parse(time.RFC3339, "2026-01-15T12:30:00Z")
		assert.Equal(t, expected, *deletedAt)
	case <-time.After(2 * time.Second):
		t.Fatal("delete callback should fire")
	}
}

func TestDetectDeletedWorkers_NilWhenMalformedDeletionTimestamp(t *testing.T) {
	deletedAtCh := make(chan *time.Time, 1)
	p := &GMIDeploymentProvider{
		workerStatusCallbacks: make(map[uint64]WorkerStatusChangeCallback),
		workerDeleteCallbacks: map[uint64]WorkerDeleteCallback{
			1: func(workerID, endpoint string, deletedAt *time.Time) {
				deletedAtCh <- deletedAt
			},
		},
		workerDeleteCallbacksLock: sync.RWMutex{},
	}

	// Store a worker with malformed DeletionTimestamp
	p.workerStates.Store("pod-bad", &gmiWorkerState{
		ID:                "pod-bad",
		Endpoint:          "test-model",
		Status:            "Running",
		DeletionTimestamp: "not-a-date",
	})

	p.detectDeletedWorkers(map[string]bool{})

	select {
	case deletedAt := <-deletedAtCh:
		assert.Nil(t, deletedAt)
	case <-time.After(2 * time.Second):
		t.Fatal("delete callback should fire")
	}
}

func TestDetectDeletedWorkers_NilWhenNoDeletionTimestamp(t *testing.T) {
	deletedAtCh := make(chan *time.Time, 1)
	p := &GMIDeploymentProvider{
		workerStatusCallbacks: make(map[uint64]WorkerStatusChangeCallback),
		workerDeleteCallbacks: map[uint64]WorkerDeleteCallback{
			1: func(workerID, endpoint string, deletedAt *time.Time) {
				deletedAtCh <- deletedAt
			},
		},
		workerDeleteCallbacksLock: sync.RWMutex{},
	}

	// Store a worker without DeletionTimestamp
	p.workerStates.Store("pod-2", &gmiWorkerState{
		ID:       "pod-2",
		Endpoint: "test-model",
		Status:   "Running",
	})

	// Detect with empty current set → pod-2 is deleted
	p.detectDeletedWorkers(map[string]bool{})

	select {
	case deletedAt := <-deletedAtCh:
		assert.Nil(t, deletedAt)
	case <-time.After(2 * time.Second):
		t.Fatal("delete callback should fire")
	}
}
