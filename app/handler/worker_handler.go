package handler

import (
	"fmt"
	"net/http"

	"waverless/internal/model"
	"waverless/internal/service"
	"waverless/pkg/interfaces"
	"waverless/pkg/logger"

	"github.com/gin-gonic/gin"
)

// WorkerHandler handles worker-related operations
type WorkerHandler struct {
	workerService      *service.WorkerService
	taskService        *service.TaskService
	deploymentProvider interfaces.DeploymentProvider
}

// NewWorkerHandler creates a new worker handler
func NewWorkerHandler(workerService *service.WorkerService, taskService *service.TaskService, deploymentProvider interfaces.DeploymentProvider) *WorkerHandler {
	return &WorkerHandler{
		workerService:      workerService,
		taskService:        taskService,
		deploymentProvider: deploymentProvider,
	}
}

// WorkerWithPodInfo Worker 信息（包含 Pod 状态）
type WorkerWithPodInfo struct {
	model.Worker
	PodPhase          string `json:"podPhase,omitempty"`          // Pod phase: Pending, Running, Succeeded, Failed
	PodStatus         string `json:"podStatus,omitempty"`         // Detailed status: Creating, Running, Terminating, etc.
	PodReason         string `json:"podReason,omitempty"`         // Status reason
	PodMessage        string `json:"podMessage,omitempty"`        // Status message
	PodIP             string `json:"podIP,omitempty"`             // Pod IP
	PodNodeName       string `json:"podNodeName,omitempty"`       // Node name
	PodCreatedAt      string `json:"podCreatedAt,omitempty"`      // Pod creation time
	PodStartedAt      string `json:"podStartedAt,omitempty"`      // Pod start time
	PodRestartCount   int32  `json:"podRestartCount,omitempty"`   // Container restart count
	DeletionTimestamp string `json:"deletionTimestamp,omitempty"` // Set when pod is terminating
}

// Heartbeat handles worker heartbeat (compatible with runpod ping interface)
// @Summary Worker heartbeat
// @Description Worker sends periodic heartbeat to maintain online status
// @Tags worker
// @Accept json
// @Produce json
// @Param worker_id query string true "Worker ID"
// @Param endpoint query string false "Endpoint that worker belongs to"
// @Param job_in_progress query []string false "List of task IDs in progress"
// @Success 200 {object} map[string]string
// @Router /ping [get]
func (h *WorkerHandler) Heartbeat(c *gin.Context) {
	// Get endpoint from URL path (required)
	endpoint := c.Param("endpoint")
	if endpoint == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "endpoint required in URL path"})
		return
	}

	// Get worker_id from URL path (required)
	workerID := c.Param("worker_id")
	if workerID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "worker_id required in URL path"})
		return
	}

	jobsInProgress := c.QueryArray("job_id")

	req := &model.HeartbeatRequest{
		WorkerID:       workerID,
		JobsInProgress: jobsInProgress,
	}

	if err := h.workerService.HandleHeartbeat(c.Request.Context(), req, endpoint); err != nil {
		logger.ErrorCtx(c.Request.Context(), "failed to handle heartbeat: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// PullJobs pulls tasks from queue (compatible with runpod job-take interface)
// @Summary Pull tasks
// @Description Worker pulls pending tasks from queue
// @Tags worker
// @Produce json
// @Param worker_id query string true "Worker ID"
// @Param endpoint query string false "Endpoint that worker belongs to"
// @Param job_in_progress query string false "Whether there are tasks in progress (0 or 1)"
// @Param batch_size query int false "Batch pull count"
// @Success 200 {array} model.JobInfo
// @Success 204 "No tasks available"
// @Router /job-take [get]
func (h *WorkerHandler) PullJobs(c *gin.Context) {
	// Get endpoint from URL path (required)
	endpoint := c.Param("endpoint")
	if endpoint == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "endpoint required in URL path"})
		return
	}

	// Get worker_id from URL path (required)
	workerID := c.Param("worker_id")
	if workerID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "worker_id required in URL path"})
		return
	}

	jobsInProgress := c.QueryArray("job_id")
	batchSize := 1
	if bs := c.Query("batch_size"); bs != "" {
		if _, err := fmt.Sscanf(bs, "%d", &batchSize); err != nil {
			batchSize = 1
		}
	}

	req := &model.JobPullRequest{
		WorkerID:       workerID,
		JobsInProgress: jobsInProgress,
		BatchSize:      batchSize,
	}

	resp, err := h.workerService.PullJobs(c.Request.Context(), req, endpoint)
	if err != nil {
		logger.ErrorCtx(c.Request.Context(), "failed to pull jobs: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if len(resp.Jobs) == 0 {
		c.Status(http.StatusNoContent)
		return
	}

	// Return single task or task list (based on batch_size)
	if batchSize == 1 && len(resp.Jobs) > 0 {
		c.JSON(http.StatusOK, resp.Jobs[0])
	} else {
		c.JSON(http.StatusOK, resp.Jobs)
	}
}

// SubmitResult submits task result
// @Summary Submit task result
// @Description Worker submits result after completing task (RunPod format compatible)
// @Tags worker
// @Accept json
// @Produce json
// @Param request body model.JobResultRequest true "Task result"
// @Param task_id path string false "Task ID (from URL path)"
// @Param X-Request-ID header string false "Task ID (RunPod compatibility)"
// @Success 200 {object} map[string]string
// @Router /result [post]
func (h *WorkerHandler) SubmitResult(c *gin.Context) {
	var req model.JobResultRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.ErrorCtx(c.Request.Context(), "invalid request: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	// RunPod compatibility: Try to get task_id from multiple sources
	// Priority: 1. URL path 2. X-Request-ID header 3. JSON body
	if req.TaskID == "" {
		// Try URL path parameter first
		taskID := c.Param("task_id")
		if taskID != "" {
			req.TaskID = taskID
			logger.DebugCtx(c.Request.Context(), "got task_id from URL path, task_id: %s", taskID)
		}
	}

	if req.TaskID == "" {
		// Try X-Request-ID header
		taskID := c.GetHeader("X-Request-ID")
		if taskID != "" {
			req.TaskID = taskID
			logger.DebugCtx(c.Request.Context(), "got task_id from X-Request-ID header, task_id: %s", taskID)
		}
	}

	if req.TaskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "task_id required (in URL path, X-Request-ID header, or JSON body)"})
		return
	}

	if err := h.taskService.UpdateTaskResult(c.Request.Context(), &req); err != nil {
		logger.ErrorCtx(c.Request.Context(), "failed to update task result: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// GetWorkerList gets worker list (including Pod status)
// @Summary Get worker list
// @Description Get all workers (including creating, running, and terminating Pods)
// @Tags worker
// @Produce json
// @Param endpoint query string false "Filter by endpoint"
// @Success 200 {array} WorkerWithPodInfo
// @Router /workers [get]
func (h *WorkerHandler) GetWorkerList(c *gin.Context) {
	ctx := c.Request.Context()
	endpoint := c.Query("endpoint")

	// Get all workers from Redis
	workers, err := h.workerService.ListWorkers(ctx, endpoint)
	if err != nil {
		logger.ErrorCtx(ctx, "failed to get worker list: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Get all endpoints to query their pods
	endpoints := make(map[string]struct{})
	if endpoint != "" {
		endpoints[endpoint] = struct{}{}
	} else {
		// Get unique endpoints from workers
		for _, w := range workers {
			if w.Endpoint != "" {
				endpoints[w.Endpoint] = struct{}{}
			}
		}
	}

	// Map: podName -> worker
	workerByPodName := make(map[string]*model.Worker)
	for _, w := range workers {
		if w.PodName != "" {
			workerByPodName[w.PodName] = w
		}
	}

	// Get Pods from K8s for all endpoints
	allPods := make([]*interfaces.PodInfo, 0)
	if h.deploymentProvider != nil {
		for ep := range endpoints {
			pods, err := h.deploymentProvider.GetPods(ctx, ep)
			if err != nil {
				logger.WarnCtx(ctx, "failed to get pods for endpoint %s: %v", ep, err)
				continue
			}
			allPods = append(allPods, pods...)
		}
	}

	// Map: podName -> PodInfo
	podByName := make(map[string]*interfaces.PodInfo)
	for _, pod := range allPods {
		podByName[pod.Name] = pod
	}

	// Merge worker info and pod info
	result := make([]WorkerWithPodInfo, 0)

	// 1. Add all workers with their pod info (if available)
	for _, worker := range workers {
		workerWithPod := WorkerWithPodInfo{
			Worker: *worker,
		}

		if worker.PodName != "" {
			if pod, exists := podByName[worker.PodName]; exists {
				workerWithPod.PodPhase = pod.Phase
				workerWithPod.PodStatus = pod.Status
				workerWithPod.PodReason = pod.Reason
				workerWithPod.PodMessage = pod.Message
				workerWithPod.PodIP = pod.IP
				workerWithPod.PodNodeName = pod.NodeName
				workerWithPod.PodCreatedAt = pod.CreatedAt
				workerWithPod.PodStartedAt = pod.StartedAt
				workerWithPod.PodRestartCount = pod.RestartCount
				workerWithPod.DeletionTimestamp = pod.DeletionTimestamp

				// Remove from map (already processed)
				delete(podByName, worker.PodName)
			}
		}

		result = append(result, workerWithPod)
	}

	// 2. Add pods without matching workers (e.g., pods creating but worker not registered yet)
	for _, pod := range podByName {
		workerWithPod := WorkerWithPodInfo{
			Worker: model.Worker{
				ID:       "", // No worker ID yet
				Endpoint: endpoint,
				Status:   model.WorkerStatusOffline, // Not registered yet
				PodName:  pod.Name,
			},
			PodPhase:          pod.Phase,
			PodStatus:         pod.Status,
			PodReason:         pod.Reason,
			PodMessage:        pod.Message,
			PodIP:             pod.IP,
			PodNodeName:       pod.NodeName,
			PodCreatedAt:      pod.CreatedAt,
			PodStartedAt:      pod.StartedAt,
			PodRestartCount:   pod.RestartCount,
			DeletionTimestamp: pod.DeletionTimestamp,
		}
		result = append(result, workerWithPod)
	}

	c.JSON(http.StatusOK, result)
}

// DescribeWorker gets worker details (including Pod describe)
// @Summary Get worker details
// @Description Get worker and corresponding Pod details (similar to kubectl describe pod)
// @Tags worker
// @Produce json
// @Param name path string true "Endpoint name"
// @Param pod_name path string true "Pod name"
// @Success 200 {object} interfaces.PodDetail
// @Router /endpoints/:name/workers/:pod_name/describe [get]
func (h *WorkerHandler) DescribeWorker(c *gin.Context) {
	ctx := c.Request.Context()
	endpoint := c.Param("name")  // Route parameter is :name not :endpoint
	podName := c.Param("pod_name")

	if endpoint == "" || podName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "endpoint and pod_name are required"})
		return
	}

	if h.deploymentProvider == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "deployment provider not available"})
		return
	}

	podDetail, err := h.deploymentProvider.DescribePod(ctx, endpoint, podName)
	if err != nil {
		logger.ErrorCtx(ctx, "failed to describe pod %s: %v", podName, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, podDetail)
}
