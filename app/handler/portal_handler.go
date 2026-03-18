package handler

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"time"

	"waverless/internal/service"
	endpointsvc "waverless/internal/service/endpoint"
	"waverless/pkg/interfaces"
	"waverless/pkg/logger"
	"waverless/pkg/status"

	"github.com/gin-gonic/gin"
)

// PortalHandler serves stable southbound APIs for the portal control plane.
type PortalHandler struct {
	taskService        *service.TaskService
	workerService      *service.WorkerService
	endpointService    *endpointsvc.Service
	deploymentProvider interfaces.DeploymentProvider
}

// NewPortalHandler creates a new portal handler.
func NewPortalHandler(taskService *service.TaskService, workerService *service.WorkerService, endpointService *endpointsvc.Service, deploymentProvider interfaces.DeploymentProvider) *PortalHandler {
	return &PortalHandler{
		taskService:        taskService,
		workerService:      workerService,
		endpointService:    endpointService,
		deploymentProvider: deploymentProvider,
	}
}

type portalInstanceInfoResponse struct {
	ClusterID            string   `json:"cluster_id"`
	InstanceStatus       string   `json:"instance_status"`
	Version              string   `json:"version"`
	SupportedAPIVersions []string `json:"supported_api_versions"`
	Capabilities         []string `json:"capabilities"`
	ProviderTypes        []string `json:"provider_types"`
	ServerTime           string   `json:"server_time"`
}

type portalInstanceHealthResponse struct {
	Status       string            `json:"status"`
	ClusterID    string            `json:"cluster_id"`
	Reachable    bool              `json:"reachable"`
	CheckedAt    string            `json:"checked_at"`
	Dependencies map[string]string `json:"dependencies,omitempty"`
}

type portalTaskResponse struct {
	TaskID          string  `json:"task_id"`
	Status          string  `json:"status"`
	EndpointID      string  `json:"endpoint_id,omitempty"`
	PhysicalName    string  `json:"physical_name"`
	WorkerID        string  `json:"worker_id,omitempty"`
	DelayTimeMS     int64   `json:"delay_time_ms,omitempty"`
	ExecutionTimeMS int64   `json:"execution_time_ms,omitempty"`
	CreatedAt       string  `json:"created_at,omitempty"`
	StartedAt       *string `json:"started_at"`
	CompletedAt     *string `json:"completed_at"`
	Error           string  `json:"error,omitempty"`
	RequestID       string  `json:"request_id,omitempty"`
}

type portalCancelTaskResponse struct {
	TaskID       string `json:"task_id"`
	Accepted     bool   `json:"accepted"`
	ResultStatus string `json:"result_status"`
	RequestID    string `json:"request_id,omitempty"`
}

type portalEndpointResponse struct {
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace,omitempty"`
	DisplayName       string            `json:"display_name,omitempty"`
	Description       string            `json:"description,omitempty"`
	SpecName          string            `json:"spec_name"`
	Image             string            `json:"image"`
	Replicas          int               `json:"replicas"`
	GPUCount          int               `json:"gpu_count"`
	MinReplicas       int               `json:"min_replicas"`
	MaxReplicas       int               `json:"max_replicas"`
	ScaleUpThreshold  int               `json:"scale_up_threshold"`
	ScaleDownIdleTime int               `json:"scale_down_idle_time"`
	ScaleUpCooldown   int               `json:"scale_up_cooldown"`
	ScaleDownCooldown int               `json:"scale_down_cooldown"`
	Priority          int               `json:"priority"`
	EnableDynamicPrio *bool             `json:"enable_dynamic_prio,omitempty"`
	HighLoadThreshold int               `json:"high_load_threshold"`
	PriorityBoost     int               `json:"priority_boost"`
	Env               map[string]string `json:"env,omitempty"`
	TaskTimeout       int               `json:"task_timeout"`
	EnablePtrace      bool              `json:"enable_ptrace"`
	MaxPendingTasks   int               `json:"max_pending_tasks"`
	Status            string            `json:"status"`
	ReadyReplicas     int               `json:"ready_replicas"`
	AvailableReplicas int               `json:"available_replicas"`
	WorkerCount       int               `json:"worker_count"`
	ActiveWorkerCount int               `json:"active_worker_count"`
	TotalTasks        int64             `json:"total_tasks"`
	CompletedTasks    int64             `json:"completed_tasks"`
	FailedTasks       int64             `json:"failed_tasks"`
	PendingTasks      int64             `json:"pending_tasks,omitempty"`
	RunningTasks      int64             `json:"running_tasks,omitempty"`
	CreatedAt         string            `json:"created_at"`
	UpdatedAt         string            `json:"updated_at"`
	HealthStatus      string            `json:"health_status,omitempty"`
	HealthMessage     string            `json:"health_message,omitempty"`
	LastHealthCheckAt string            `json:"last_health_check_at,omitempty"`
	RequestID         string            `json:"request_id,omitempty"`
}

type portalWorkerResponse struct {
	WorkerID          string  `json:"worker_id"`
	Endpoint          string  `json:"endpoint"`
	PodName           string  `json:"pod_name,omitempty"`
	Status            string  `json:"status"`
	Concurrency       int     `json:"concurrency"`
	CurrentJobs       int     `json:"current_jobs"`
	Version           string  `json:"version,omitempty"`
	LastHeartbeat     string  `json:"last_heartbeat"`
	TerminatedAt      *string `json:"terminated_at,omitempty"`
	PodStartedAt      *string `json:"pod_started_at,omitempty"`
	FailureType       string  `json:"failure_type,omitempty"`
	FailureReason     string  `json:"failure_reason,omitempty"`
	FailureSuggestion string  `json:"failure_suggestion,omitempty"`
	FailureOccurredAt *string `json:"failure_occurred_at,omitempty"`
	RequestID         string  `json:"request_id,omitempty"`
}

// GetInstanceInfo returns static instance metadata for portal capability discovery.
func (h *PortalHandler) GetInstanceInfo(c *gin.Context) {
	clusterID, requestID := portalContextValues(c)

	c.JSON(http.StatusOK, portalInstanceInfoResponse{
		ClusterID:            clusterID,
		InstanceStatus:       "ONLINE",
		Version:              "unknown",
		SupportedAPIVersions: []string{"portal-v1"},
		Capabilities: []string{
			"portal_instance_info",
			"portal_task_query",
			"portal_task_cancel",
			"portal_endpoint_query",
			"portal_worker_query",
		},
		ProviderTypes: providerTypes(h.deploymentProvider),
		ServerTime:    time.Now().UTC().Format(time.RFC3339),
	})

	logger.DebugCtx(c.Request.Context(), "portal instance info served, cluster_id=%s, request_id=%s", clusterID, requestID)
}

// GetInstanceHealth returns portal-oriented health information.
func (h *PortalHandler) GetInstanceHealth(c *gin.Context) {
	clusterID, _ := portalContextValues(c)

	resp := portalInstanceHealthResponse{
		Status:    "ok",
		ClusterID: clusterID,
		Reachable: true,
		CheckedAt: time.Now().UTC().Format(time.RFC3339),
		Dependencies: map[string]string{
			"mysql": "unknown",
			"redis": "unknown",
		},
	}

	c.JSON(http.StatusOK, resp)
}

// GetTask returns a portal-stable task response while reusing the existing task service.
func (h *PortalHandler) GetTask(c *gin.Context) {
	taskID := c.Param("task_id")
	if taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "task_id required"})
		return
	}

	requestID := c.GetString("portal_request_id")
	resp, err := h.taskService.GetTaskStatus(c.Request.Context(), taskID)
	if err != nil {
		logger.ErrorCtx(c.Request.Context(), "failed to get portal task status, task_id=%s, error=%v", taskID, err)
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
		return
	}

	c.JSON(http.StatusOK, portalTaskResponse{
		TaskID:          resp.ID,
		Status:          resp.Status,
		PhysicalName:    resp.Endpoint,
		WorkerID:        resp.WorkerID,
		DelayTimeMS:     resp.DelayTime,
		ExecutionTimeMS: resp.ExecutionMS,
		CreatedAt:       resp.CreatedAt,
		StartedAt:       nil,
		CompletedAt:     nil,
		Error:           resp.Error,
		RequestID:       requestID,
	})
}

// CancelTask cancels a task with a stable portal response envelope.
func (h *PortalHandler) CancelTask(c *gin.Context) {
	taskID := c.Param("task_id")
	if taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "task_id required"})
		return
	}

	requestID := c.GetString("portal_request_id")
	if err := h.taskService.CancelTask(c.Request.Context(), taskID); err != nil {
		logger.ErrorCtx(c.Request.Context(), "failed to cancel portal task, task_id=%s, error=%v", taskID, err)

		resultStatus := "NOT_FOUND"
		statusCode := http.StatusNotFound
		if strings.Contains(strings.ToLower(err.Error()), "finished") {
			resultStatus = "ALREADY_TERMINAL"
			statusCode = http.StatusConflict
		}

		c.JSON(statusCode, portalCancelTaskResponse{
			TaskID:       taskID,
			Accepted:     false,
			ResultStatus: resultStatus,
			RequestID:    requestID,
		})
		return
	}

	c.JSON(http.StatusOK, portalCancelTaskResponse{
		TaskID:       taskID,
		Accepted:     true,
		ResultStatus: "CANCEL_REQUESTED",
		RequestID:    requestID,
	})
}

// GetEndpointByName returns a portal-stable endpoint response for a runtime endpoint name.
func (h *PortalHandler) GetEndpointByName(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "endpoint name required"})
		return
	}

	if h.endpointService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "endpoint service unavailable"})
		return
	}

	metadata, err := h.endpointService.GetEndpoint(c.Request.Context(), name)
	if err != nil || metadata == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "endpoint not found"})
		return
	}

	resp := portalEndpointResponse{
		Name:              metadata.Name,
		Namespace:         metadata.Namespace,
		DisplayName:       metadata.DisplayName,
		Description:       metadata.Description,
		SpecName:          metadata.SpecName,
		Image:             metadata.Image,
		Replicas:          metadata.Replicas,
		GPUCount:          metadata.GpuCount,
		MinReplicas:       metadata.MinReplicas,
		MaxReplicas:       metadata.MaxReplicas,
		ScaleUpThreshold:  metadata.ScaleUpThreshold,
		ScaleDownIdleTime: metadata.ScaleDownIdleTime,
		ScaleUpCooldown:   metadata.ScaleUpCooldown,
		ScaleDownCooldown: metadata.ScaleDownCooldown,
		Priority:          metadata.Priority,
		EnableDynamicPrio: metadata.EnableDynamicPrio,
		HighLoadThreshold: metadata.HighLoadThreshold,
		PriorityBoost:     metadata.PriorityBoost,
		Env:               metadata.Env,
		TaskTimeout:       metadata.TaskTimeout,
		EnablePtrace:      metadata.EnablePtrace,
		MaxPendingTasks:   metadata.MaxPendingTasks,
		Status:            metadata.Status,
		ReadyReplicas:     metadata.ReadyReplicas,
		AvailableReplicas: metadata.AvailableReplicas,
		WorkerCount:       metadata.WorkerCount,
		ActiveWorkerCount: metadata.ActiveWorkerCount,
		TotalTasks:        metadata.TotalTasks,
		CompletedTasks:    metadata.CompletedTasks,
		FailedTasks:       metadata.FailedTasks,
		PendingTasks:      metadata.PendingTasks,
		RunningTasks:      metadata.RunningTasks,
		CreatedAt:         metadata.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:         metadata.UpdatedAt.UTC().Format(time.RFC3339),
		HealthStatus:      metadata.HealthStatus,
		HealthMessage:     metadata.HealthMessage,
		RequestID:         c.GetString("portal_request_id"),
	}
	if metadata.LastHealthCheckAt != nil {
		resp.LastHealthCheckAt = metadata.LastHealthCheckAt.UTC().Format(time.RFC3339)
	}

	c.JSON(http.StatusOK, resp)
}

// GetWorker returns a portal-stable worker response while reusing the existing worker service.
func (h *PortalHandler) GetWorker(c *gin.Context) {
	workerID := c.Param("worker_id")
	if workerID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "worker_id required"})
		return
	}

	if h.workerService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "worker service unavailable"})
		return
	}

	worker, err := h.workerService.GetWorkerByWorkerID(c.Request.Context(), workerID)
	if err != nil {
		logger.ErrorCtx(c.Request.Context(), "failed to get portal worker, worker_id=%s, error=%v", workerID, err)
		c.JSON(http.StatusNotFound, gin.H{"error": "worker not found"})
		return
	}

	requestID := c.GetString("portal_request_id")
	resp := portalWorkerResponse{
		WorkerID:      worker.WorkerID,
		Endpoint:      worker.Endpoint,
		PodName:       worker.PodName,
		Status:        worker.Status,
		Concurrency:   worker.Concurrency,
		CurrentJobs:   worker.CurrentJobs,
		Version:       worker.Version,
		LastHeartbeat: worker.LastHeartbeat.Format(time.RFC3339),
		RequestID:     requestID,
	}

	if worker.TerminatedAt != nil {
		terminatedAt := worker.TerminatedAt.Format(time.RFC3339)
		resp.TerminatedAt = &terminatedAt
	}
	if worker.PodStartedAt != nil {
		podStartedAt := worker.PodStartedAt.Format(time.RFC3339)
		resp.PodStartedAt = &podStartedAt
	}
	if worker.FailureType != "" {
		resp.FailureType = worker.FailureType
		resp.FailureReason = worker.FailureReason
		sanitized := status.NewStatusSanitizer().Sanitize(interfaces.FailureType(worker.FailureType), worker.FailureReason, "")
		if sanitized != nil {
			resp.FailureSuggestion = sanitized.Suggestion
		}
		if worker.FailureOccurredAt != nil {
			failureOccurredAt := worker.FailureOccurredAt.Format(time.RFC3339)
			resp.FailureOccurredAt = &failureOccurredAt
		}
	}

	c.JSON(http.StatusOK, resp)
}

// GetEndpointWorkersForSyncByName returns worker sync data for a runtime endpoint name.
// This is a pragmatic bridge route for portal sync before endpoint_id-based southbound routing is fully adopted.
func (h *PortalHandler) GetEndpointWorkersForSyncByName(c *gin.Context) {
	endpoint := c.Param("name")
	if endpoint == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "endpoint name required"})
		return
	}

	if h.taskService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "worker service unavailable"})
		return
	}

	ctx := c.Request.Context()
	if h.workerService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "worker service unavailable"})
		return
	}

	workers, err := h.workerService.ListWorkersForSync(ctx, endpoint)
	if err != nil {
		logger.ErrorCtx(ctx, "failed to get portal sync workers for endpoint %s: %v", endpoint, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	type workerWithPodInfo struct {
		ID                string   `json:"id"`
		Endpoint          string   `json:"endpoint"`
		PodName           string   `json:"pod_name,omitempty"`
		Status            string   `json:"status"`
		Concurrency       int      `json:"concurrency"`
		CurrentJobs       int      `json:"current_jobs"`
		JobsInProgress    []string `json:"jobs_in_progress"`
		LastHeartbeat     string   `json:"last_heartbeat"`
		LastTaskTime      string   `json:"last_task_time,omitempty"`
		Version           string   `json:"version,omitempty"`
		RegisteredAt      string   `json:"registered_at"`
		PodPhase          string   `json:"podPhase,omitempty"`
		PodStatus         string   `json:"podStatus,omitempty"`
		PodReason         string   `json:"podReason,omitempty"`
		PodMessage        string   `json:"podMessage,omitempty"`
		PodIP             string   `json:"podIP,omitempty"`
		PodNodeName       string   `json:"podNodeName,omitempty"`
		PodCreatedAt      string   `json:"podCreatedAt,omitempty"`
		PodStartedAt      string   `json:"podStartedAt,omitempty"`
		PodRestartCount   int32    `json:"podRestartCount,omitempty"`
		DeletionTimestamp string   `json:"deletionTimestamp,omitempty"`
		TerminatedAt      string   `json:"terminatedAt,omitempty"`
		FailureType       string   `json:"failureType,omitempty"`
		FailureReason     string   `json:"failureReason,omitempty"`
		FailureSuggestion string   `json:"failureSuggestion,omitempty"`
		FailureOccurredAt string   `json:"failureOccurredAt,omitempty"`
	}

	result := make([]workerWithPodInfo, 0, len(workers))
	sanitizer := status.NewStatusSanitizer()

	for _, worker := range workers {
		var jobsInProgress []string
		if worker.JobsInProgress != "" {
			_ = json.Unmarshal([]byte(worker.JobsInProgress), &jobsInProgress)
		}
		if jobsInProgress == nil {
			jobsInProgress = []string{}
		}

		item := workerWithPodInfo{
			ID:             worker.WorkerID,
			Endpoint:       worker.Endpoint,
			PodName:        worker.PodName,
			Status:         worker.Status,
			Concurrency:    worker.Concurrency,
			CurrentJobs:    worker.CurrentJobs,
			JobsInProgress: jobsInProgress,
			LastHeartbeat:  worker.LastHeartbeat.Format(time.RFC3339),
			Version:        worker.Version,
			RegisteredAt:   worker.CreatedAt.Format(time.RFC3339),
		}

		if worker.LastTaskTime != nil {
			item.LastTaskTime = worker.LastTaskTime.Format(time.RFC3339)
		}
		if worker.PodStartedAt != nil {
			item.PodStartedAt = worker.PodStartedAt.Format(time.RFC3339)
		}
		if worker.TerminatedAt != nil {
			item.TerminatedAt = worker.TerminatedAt.Format(time.RFC3339)
		}
		if worker.FailureType != "" {
			item.FailureType = worker.FailureType
			item.FailureReason = worker.FailureReason
			sanitized := sanitizer.Sanitize(interfaces.FailureType(worker.FailureType), worker.FailureReason, "")
			if sanitized != nil {
				item.FailureSuggestion = sanitized.Suggestion
			}
			if worker.FailureOccurredAt != nil {
				item.FailureOccurredAt = worker.FailureOccurredAt.Format(time.RFC3339)
			}
		}

		if rs := worker.RuntimeState; rs != nil {
			if v, ok := rs["phase"].(string); ok {
				item.PodPhase = v
			}
			if v, ok := rs["status"].(string); ok {
				item.PodStatus = v
			}
			if v, ok := rs["reason"].(string); ok {
				item.PodReason = v
			}
			if v, ok := rs["message"].(string); ok {
				item.PodMessage = v
			}
			if v, ok := rs["ip"].(string); ok {
				item.PodIP = v
			}
			if v, ok := rs["nodeName"].(string); ok {
				item.PodNodeName = v
			}
			if v, ok := rs["createdAt"].(string); ok {
				item.PodCreatedAt = v
			}
			if v, ok := rs["startedAt"].(string); ok && item.PodStartedAt == "" {
				item.PodStartedAt = v
			}
		}

		result = append(result, item)
	}

	c.JSON(http.StatusOK, result)
}

func portalContextValues(c *gin.Context) (clusterID string, requestID string) {
	clusterID = c.GetString("portal_cluster_id")
	requestID = c.GetString("portal_request_id")
	return clusterID, requestID
}

func providerTypes(provider interfaces.DeploymentProvider) []string {
	if provider == nil {
		return []string{"unknown"}
	}

	typeName := strings.ToLower(reflect.TypeOf(provider).String())
	switch {
	case strings.Contains(typeName, "k8s"):
		return []string{"k8s"}
	case strings.Contains(typeName, "novita"):
		return []string{"novita"}
	case strings.Contains(typeName, "docker"):
		return []string{"docker"}
	default:
		return []string{"unknown"}
	}
}
