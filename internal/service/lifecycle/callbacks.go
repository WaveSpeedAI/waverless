package lifecycle

import (
	"context"
	"sync"
	"time"

	"waverless/internal/model"
	"waverless/internal/service"
	"waverless/pkg/logger"
	"waverless/pkg/provider"
	"waverless/pkg/store/mysql"
)

// CallbackHandler handles all callback processing logic
// Thread-safe: All methods can be called concurrently from multiple watchers
type CallbackHandler struct {
	ctx                context.Context
	workerRepo         *mysql.WorkerRepository
	endpointRepo       *mysql.EndpointRepository
	workerService      *service.WorkerService
	workerEventService *service.WorkerEventService

	// Mutex to protect check-then-act operations for worker creation
	// Key: endpoint:podName
	workerCreationMu sync.Map
}

// NewCallbackHandler creates a new callback handler
func NewCallbackHandler(
	ctx context.Context,
	workerRepo *mysql.WorkerRepository,
	endpointRepo *mysql.EndpointRepository,
	workerService *service.WorkerService,
	workerEventService *service.WorkerEventService,
) *CallbackHandler {
	return &CallbackHandler{
		ctx:                ctx,
		workerRepo:         workerRepo,
		endpointRepo:       endpointRepo,
		workerService:      workerService,
		workerEventService: workerEventService,
	}
}

// HandleWorkerStatusChange handles Worker status changes
// Thread-safe: Can be called concurrently for different or same workers
func (h *CallbackHandler) HandleWorkerStatusChange(event *provider.WorkerStatusEvent) {
	if event == nil || event.PodInfo == nil {
		return
	}

	podName := event.WorkerID
	endpoint := event.Endpoint
	info := event.PodInfo

	// Parse timestamps
	var createdAt, startedAt *time.Time
	if info.CreatedAt != "" {
		if t, err := time.Parse(time.RFC3339, info.CreatedAt); err == nil {
			createdAt = &t
		}
	}
	if info.StartedAt != "" {
		if t, err := time.Parse(time.RFC3339, info.StartedAt); err == nil {
			startedAt = &t
		}
	}

	// Use per-worker lock to prevent race condition in check-then-act pattern
	workerKey := endpoint + ":" + podName
	mu, _ := h.workerCreationMu.LoadOrStore(workerKey, &sync.Mutex{})
	workerMu := mu.(*sync.Mutex)

	workerMu.Lock()
	defer workerMu.Unlock()

	// Check if this is a new Worker (within lock to prevent race)
	existingWorker, _ := h.workerRepo.GetByPodName(h.ctx, endpoint, podName)
	isNewWorker := existingWorker == nil

	// Create or update Worker
	if err := h.workerRepo.UpsertFromPod(h.ctx, podName, endpoint, info.Phase, info.Status, info.Reason, info.Message, info.IP, info.NodeName, createdAt, startedAt); err != nil {
		logger.WarnCtx(h.ctx, "Failed to upsert worker from pod %s: %v", podName, err)
		return
	}

	// Record WORKER_STARTED event only for new workers
	// This is now safe from race conditions due to the lock
	if isNewWorker && h.workerEventService != nil {
		h.workerEventService.RecordWorkerStarted(h.ctx, podName, endpoint)
		logger.InfoCtx(h.ctx, "Recorded WORKER_STARTED event for new worker: %s", podName)
	}
}

// HandleWorkerDelete handles Worker deletion
// Thread-safe: Can be called concurrently for different workers
// Idempotent: Safe to call multiple times for the same worker
//
// NOTE: Due to async event processing, Pod Delete event may arrive before
// the Worker record is created (race condition during rapid scale up/down).
// We retry a few times to handle this edge case.
func (h *CallbackHandler) HandleWorkerDelete(event *provider.WorkerDeleteEvent) {
	if event == nil {
		return
	}

	podName := event.WorkerID
	endpoint := event.Endpoint

	// Record WORKER_OFFLINE event
	if h.workerEventService != nil {
		h.workerEventService.RecordWorkerOffline(h.ctx, podName, endpoint, podName)
	}

	// Mark Worker as OFFLINE with retry
	// Retry is needed because Pod Delete event may arrive before Worker record is created
	// (race condition when K8s rapidly creates and deletes a pod)
	maxRetries := 3
	retryInterval := 500 * time.Millisecond

	for i := 0; i < maxRetries; i++ {
		err := h.workerRepo.MarkOfflineByPodName(h.ctx, podName)
		if err != nil {
			logger.WarnCtx(h.ctx, "Failed to mark worker offline for deleted pod %s (attempt %d/%d): %v",
				podName, i+1, maxRetries, err)
			if i < maxRetries-1 {
				time.Sleep(retryInterval)
			}
			continue
		}

		// Check if any rows were actually updated
		// MarkOfflineByPodName returns nil even if no rows matched
		worker, _ := h.workerRepo.GetByPodName(h.ctx, endpoint, podName)
		if worker != nil && worker.Status == "OFFLINE" {
			logger.InfoCtx(h.ctx, "Successfully marked worker %s as OFFLINE", podName)
			return
		}

		// Worker record might not exist yet, wait and retry
		if i < maxRetries-1 {
			logger.DebugCtx(h.ctx, "Worker record for pod %s not found or not updated, retrying in %v...",
				podName, retryInterval)
			time.Sleep(retryInterval)
		}
	}

	logger.WarnCtx(h.ctx, "Failed to mark worker offline for pod %s after %d retries (worker may not exist yet)",
		podName, maxRetries)
}

// HandleWorkerDraining handles Worker Draining
// Thread-safe: Can be called concurrently for different workers
// Idempotent: Safe to call multiple times for the same worker
func (h *CallbackHandler) HandleWorkerDraining(event *provider.WorkerDrainingEvent) {
	if event == nil {
		return
	}

	podName := event.WorkerID
	endpoint := event.Endpoint

	logger.InfoCtx(h.ctx, "Worker %s (endpoint: %s) marked for draining, reason: %s",
		podName, endpoint, event.Reason)

	// Find Worker
	worker, err := h.workerService.GetWorkerByPodName(h.ctx, endpoint, podName)
	if err != nil {
		logger.WarnCtx(h.ctx, "Worker %s draining but not found: %v", podName, err)
		return
	}

	// Check if already in terminal state (OFFLINE)
	// Don't override terminal states with DRAINING
	if worker.Status == model.WorkerStatusOffline {
		logger.InfoCtx(h.ctx, "Worker %s already in terminal state %s, skipping draining", worker.ID, worker.Status)
		return
	}

	// Check if already DRAINING (idempotent)
	if worker.Status == model.WorkerStatusDraining {
		logger.DebugCtx(h.ctx, "Worker %s already in DRAINING state, skipping", worker.ID)
		return
	}

	// Mark Worker as DRAINING
	err = h.workerService.UpdateWorkerStatus(h.ctx, worker.ID, model.WorkerStatusDraining)
	if err != nil {
		logger.ErrorCtx(h.ctx, "Failed to mark worker %s as draining: %v", worker.ID, err)
		return
	}

	logger.InfoCtx(h.ctx, "Worker %s (Pod: %s) marked as DRAINING, will not receive new tasks",
		worker.ID, podName)
}

// HandleWorkerFailure handles Worker failure
// Thread-safe: Can be called concurrently for different workers
// Idempotent: Safe to call multiple times (updates failure info)
func (h *CallbackHandler) HandleWorkerFailure(event *provider.WorkerFailureEvent) {
	if event == nil || event.FailureInfo == nil {
		return
	}

	podName := event.WorkerID
	endpoint := event.Endpoint
	failureInfo := event.FailureInfo

	logger.WarnCtx(h.ctx, "Worker failure detected: pod=%s, endpoint=%s, type=%s, reason=%s",
		podName, endpoint, failureInfo.Type, failureInfo.SanitizedMsg)

	// Update Worker failure information
	if err := h.workerRepo.UpdateWorkerFailure(h.ctx, podName, string(failureInfo.Type), failureInfo.SanitizedMsg, failureInfo.Message, time.Now()); err != nil {
		logger.ErrorCtx(h.ctx, "Failed to update worker failure: pod=%s, error=%v", podName, err)
	} else {
		logger.InfoCtx(h.ctx, "Worker failure recorded in database: pod=%s, type=%s", podName, failureInfo.Type)
	}
}

// HandleEndpointStatusChange handles Endpoint status changes
// Thread-safe: Can be called concurrently for different endpoints
// Idempotent: Safe to call multiple times (updates runtime state)
func (h *CallbackHandler) HandleEndpointStatusChange(event *provider.EndpointStatusEvent) {
	if event == nil {
		return
	}

	endpoint := event.Endpoint

	// Update Endpoint runtime state
	if h.endpointRepo != nil {
		runtimeState := map[string]any{
			"replicas":          event.DesiredReplicas,
			"readyReplicas":     event.ReadyReplicas,
			"availableReplicas": event.AvailableReplicas,
		}

		if err := h.endpointRepo.UpdateRuntimeState(h.ctx, endpoint, event.Status, runtimeState); err != nil {
			logger.ErrorCtx(h.ctx, "Failed to update endpoint runtime state: %v", err)
		}
	}
}
