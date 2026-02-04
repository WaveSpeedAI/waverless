package lifecycle

import (
	"context"
	"time"

	"waverless/internal/model"
	"waverless/internal/service"
	"waverless/pkg/logger"
	"waverless/pkg/store/mysql"
)

// CallbackHandler handles all callback processing logic
type CallbackHandler struct {
	ctx                context.Context
	workerRepo         *mysql.WorkerRepository
	endpointRepo       *mysql.EndpointRepository
	workerService      *service.WorkerService
	workerEventService *service.WorkerEventService
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
func (h *CallbackHandler) HandleWorkerStatusChange(event *WorkerStatusEvent) {
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

	// Check if this is a new Worker
	existingWorker, _ := h.workerRepo.GetByPodName(h.ctx, endpoint, podName)
	isNewWorker := existingWorker == nil

	// Create or update Worker
	if err := h.workerRepo.UpsertFromPod(h.ctx, podName, endpoint, info.Phase, info.Status, info.Reason, info.Message, info.IP, info.NodeName, createdAt, startedAt); err != nil {
		logger.WarnCtx(h.ctx, "Failed to upsert worker from pod %s: %v", podName, err)
	}

	// Record WORKER_STARTED event
	if isNewWorker && h.workerEventService != nil {
		h.workerEventService.RecordWorkerStarted(h.ctx, podName, endpoint)
	}
}

// HandleWorkerDelete handles Worker deletion
func (h *CallbackHandler) HandleWorkerDelete(event *WorkerDeleteEvent) {
	if event == nil {
		return
	}

	podName := event.WorkerID
	endpoint := event.Endpoint

	// Record WORKER_OFFLINE event
	if h.workerEventService != nil {
		h.workerEventService.RecordWorkerOffline(h.ctx, podName, endpoint, podName)
	}

	// Mark Worker as OFFLINE
	if err := h.workerRepo.MarkOfflineByPodName(h.ctx, podName); err != nil {
		logger.WarnCtx(h.ctx, "Failed to mark worker offline for deleted pod %s: %v", podName, err)
	}
}

// HandleWorkerDraining handles Worker Draining
func (h *CallbackHandler) HandleWorkerDraining(event *WorkerDrainingEvent) {
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
func (h *CallbackHandler) HandleWorkerFailure(event *WorkerFailureEvent) {
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
func (h *CallbackHandler) HandleEndpointStatusChange(event *EndpointStatusEvent) {
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
