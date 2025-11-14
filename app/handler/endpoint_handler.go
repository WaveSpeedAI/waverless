package handler

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gorilla/websocket"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"

	"waverless/internal/model"
	"waverless/internal/service"
	endpointsvc "waverless/internal/service/endpoint"
	"waverless/pkg/deploy/k8s"
	"waverless/pkg/interfaces"
	"waverless/pkg/logger"

	"github.com/gin-gonic/gin"
)

// EndpointHandler handles endpoint lifecycle APIs (metadata + K8s deployment)
type EndpointHandler struct {
	deploymentProvider interfaces.DeploymentProvider
	endpointService    *endpointsvc.Service
	workerService      *service.WorkerService
}

// NewEndpointHandler creates endpoint handler
func NewEndpointHandler(deploymentProvider interfaces.DeploymentProvider, endpointService *endpointsvc.Service, workerService *service.WorkerService) *EndpointHandler {
	return &EndpointHandler{
		deploymentProvider: deploymentProvider,
		endpointService:    endpointService,
		workerService:      workerService,
	}
}

// CreateEndpoint deploys a new endpoint (including metadata and K8s deployment)
// @Summary Create endpoint
// @Description Create a new endpoint: write metadata and trigger K8s deployment
// @Tags Endpoints
// @Accept json
// @Produce json
// @Param request body k8s.DeployAppRequest true "Deployment configuration"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/endpoints [post]
func (h *EndpointHandler) CreateEndpoint(c *gin.Context) {
	var req k8s.DeployAppRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.InfoCtx(c.Request.Context(), "[ERROR] Failed to bind deploy request: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	logger.InfoCtx(c.Request.Context(), "[INFO] Creating endpoint: endpoint=%s, spec=%s, image=%s, replicas=%d, taskTimeout=%d",
		req.Endpoint, req.SpecName, req.Image, req.Replicas, req.TaskTimeout)

	providerReq := &interfaces.DeployRequest{
		Endpoint:    req.Endpoint,
		SpecName:    req.SpecName,
		Image:       req.Image,
		Replicas:    req.Replicas,
		TaskTimeout: req.TaskTimeout,
	}

	metadata := h.buildMetadataFromRequest(c, req)

	var (
		resp *interfaces.DeployResponse
		err  error
	)

	switch {
	case h.endpointService != nil:
		resp, err = h.endpointService.Deploy(c.Request.Context(), providerReq, metadata)
	case h.deploymentProvider != nil:
		resp, err = h.deploymentProvider.Deploy(c.Request.Context(), providerReq)
	default:
		err = fmt.Errorf("deployment provider unavailable")
	}

	if err != nil {
		logger.ErrorCtx(c.Request.Context(), "[ERROR] Failed to deploy app %s: %v", req.Endpoint, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":    err.Error(),
			"endpoint": req.Endpoint,
			"spec":     req.SpecName,
			"details":  fmt.Sprintf("Deployment failed: %v", err),
		})
		return
	}

	logger.InfoCtx(c.Request.Context(), "[INFO] Successfully created endpoint: %s", req.Endpoint)

	c.JSON(http.StatusOK, gin.H{
		"message":   resp.Message,
		"endpoint":  resp.Endpoint,
		"createdAt": resp.CreatedAt,
	})
}

// PreviewDeploymentYAML previews endpoint deployment YAML
// @Summary Preview endpoint deployment YAML
// @Description Preview K8s deployment YAML for endpoint
// @Tags Endpoints
// @Accept json
// @Produce plain
// @Param request body k8s.DeployAppRequest true "Deployment configuration"
// @Success 200 {string} string
// @Router /api/v1/endpoints/preview [post]
func (h *EndpointHandler) PreviewDeploymentYAML(c *gin.Context) {
	var req k8s.DeployAppRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	providerReq := &interfaces.DeployRequest{
		Endpoint: req.Endpoint,
		SpecName: req.SpecName,
		Image:    req.Image,
		Replicas: req.Replicas,
	}

	yaml, err := h.deploymentProvider.PreviewDeploymentYAML(c.Request.Context(), providerReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.String(http.StatusOK, yaml)
}

// GetEndpoint gets endpoint details (MySQL metadata primary)
// @Summary Get endpoint details (metadata primary, enriched with runtime status)
// @Description Read endpoint metadata from MySQL first, then enrich with K8s runtime status (if available)
// @Tags Endpoints
// @Produce json
// @Param name path string true "Endpoint name"
// @Success 200 {object} interfaces.EndpointMetadata
// @Router /api/v1/endpoints/{name} [get]
func (h *EndpointHandler) GetEndpoint(c *gin.Context) {
	name := c.Param("name")

	// If metadata store is not available, fall back to legacy behavior
	if h.endpointService == nil {
		h.getEndpointFromRuntimeOnly(c, name)
		return
	}

	metadata, err := h.endpointService.GetEndpoint(c.Request.Context(), name)
	if err != nil || metadata == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "endpoint not found"})
		return
	}

	// Enrich with runtime status if deployment provider is available
	if h.deploymentProvider != nil {
		if app, err := h.deploymentProvider.GetApp(c.Request.Context(), name); err == nil && app != nil {
			metadata.Status = app.Status
			metadata.ReadyReplicas = int(app.ReadyReplicas)
			metadata.AvailableReplicas = int(app.AvailableReplicas)
			if metadata.Namespace == "" {
				metadata.Namespace = app.Namespace
			}
			// Only backfill spec/image/replicas when metadata lacks those fields
			if metadata.Image == "" {
				metadata.Image = app.Image
			}
			if metadata.Replicas == 0 {
				metadata.Replicas = int(app.Replicas)
			}
		} else if err != nil {
			logger.WarnCtx(c.Request.Context(), "failed to fetch runtime status for endpoint %s: %v", name, err)
		}
	}

	c.JSON(http.StatusOK, metadata)
}

// ListEndpoints lists all endpoints
// @Summary List all endpoints (MySQL metadata primary)
// @Description Get all endpoint metadata and enrich with K8s runtime status (if available)
// @Tags Endpoints
// @Produce json
// @Success 200 {array} interfaces.EndpointMetadata
// @Router /api/v1/endpoints [get]
func (h *EndpointHandler) ListEndpoints(c *gin.Context) {
	if h.endpointService == nil {
		h.listEndpointsFromRuntimeOnly(c)
		return
	}

	endpoints, err := h.endpointService.ListEndpoints(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	runtimeMap := make(map[string]*interfaces.AppInfo)
	if h.deploymentProvider != nil {
		if apps, err := h.deploymentProvider.ListApps(c.Request.Context()); err == nil {
			for _, app := range apps {
				runtimeMap[app.Name] = app
			}
		} else {
			logger.WarnCtx(c.Request.Context(), "failed to list runtime apps for enrichment: %v", err)
		}
	}

	for _, endpoint := range endpoints {
		if app, ok := runtimeMap[endpoint.Name]; ok {
			endpoint.Status = app.Status
			endpoint.ReadyReplicas = int(app.ReadyReplicas)
			endpoint.AvailableReplicas = int(app.AvailableReplicas)
			if endpoint.Namespace == "" {
				endpoint.Namespace = app.Namespace
			}
			if endpoint.Image == "" {
				endpoint.Image = app.Image
			}
			if endpoint.Replicas == 0 {
				endpoint.Replicas = int(app.Replicas)
			}
		}
	}

	c.JSON(http.StatusOK, endpoints)
}

// DeleteEndpoint deletes endpoint
// @Summary Delete endpoint
// @Description Delete specified endpoint (including K8s deployment and metadata)
// @Tags Endpoints
// @Param name path string true "Endpoint name"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/endpoints/{name} [delete]
func (h *EndpointHandler) DeleteEndpoint(c *gin.Context) {
	name := c.Param("name")

	switch {
	case h.endpointService != nil:
		if err := h.endpointService.DeleteDeployment(c.Request.Context(), name); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	case h.deploymentProvider != nil:
		if err := h.deploymentProvider.DeleteApp(c.Request.Context(), name); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	default:
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "deployment provider unavailable"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Endpoint deleted successfully",
		"name":    name,
	})
}

// GetEndpointLogs gets endpoint logs
// @Summary Get endpoint logs
// @Description Get endpoint K8s logs
// @Tags Endpoints
// @Produce plain
// @Param name path string true "Endpoint name"
// @Param lines query int false "Number of log lines" default(100)
// @Param pod_name query string false "Pod name (optional, get specific Pod logs if specified)"
// @Success 200 {string} string
// @Router /api/v1/endpoints/{name}/logs [get]
func (h *EndpointHandler) GetEndpointLogs(c *gin.Context) {
	name := c.Param("name")
	linesStr := c.DefaultQuery("lines", "100")
	podName := c.Query("pod_name")

	lines, err := strconv.Atoi(linesStr)
	if err != nil {
		lines = 100
	}

	var logs string
	if podName != "" {
		logs, err = h.deploymentProvider.GetAppLogs(c.Request.Context(), name, lines, podName)
	} else {
		logs, err = h.deploymentProvider.GetAppLogs(c.Request.Context(), name, lines)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.String(http.StatusOK, logs)
}

// ListSpecs lists all specs
// @Summary List all specs
// @Description Get all available resource specs
// @Tags Specs
// @Produce json
// @Success 200 {array} interfaces.SpecInfo
// @Router /api/v1/specs [get]
func (h *EndpointHandler) ListSpecs(c *gin.Context) {
	specs, err := h.deploymentProvider.ListSpecs(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, specs)
}

// GetSpec gets spec details
// @Summary Get spec details
// @Description Get detailed information for specified spec
// @Tags Specs
// @Produce json
// @Param name path string true "Spec name"
// @Success 200 {object} interfaces.SpecInfo
// @Router /api/v1/specs/{name} [get]
func (h *EndpointHandler) GetSpec(c *gin.Context) {
	name := c.Param("name")

	spec, err := h.deploymentProvider.GetSpec(c.Request.Context(), name)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, spec)
}

// UpdateEndpoint updates endpoint metadata and autoscaling configuration
// @Summary Update endpoint configuration (metadata + autoscaling config)
// @Description Update endpoint metadata and autoscaling config (does not directly modify K8s replicas/image)
// @Tags Endpoints
// @Accept json
// @Produce json
// @Param name path string true "Endpoint name"
// @Param request body interfaces.UpdateEndpointConfigRequest true "Configuration update request"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/endpoints/{name} [put]
func (h *EndpointHandler) UpdateEndpoint(c *gin.Context) {
	name := c.Param("name")

	if h.endpointService == nil {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "metadata store not available"})
		return
	}

	var req interfaces.UpdateEndpointConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get existing metadata
	existingMeta, err := h.endpointService.GetEndpoint(c.Request.Context(), name)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "endpoint not found"})
		return
	}

	// Apply updates - only update fields that are explicitly provided (not nil)
	// Using pointers allows us to distinguish between "not provided" and "set to zero/empty"
	// This prevents concurrent updates from overwriting each other's changes

	// Basic metadata
	if req.DisplayName != nil {
		existingMeta.DisplayName = *req.DisplayName
	}
	if req.Description != nil {
		existingMeta.Description = *req.Description
	}
	if req.TaskTimeout != nil {
		existingMeta.TaskTimeout = *req.TaskTimeout
	}

	// Autoscaling configuration
	if req.MinReplicas != nil {
		existingMeta.MinReplicas = *req.MinReplicas
	}
	if req.MaxReplicas != nil {
		existingMeta.MaxReplicas = *req.MaxReplicas
	}
	if req.Priority != nil {
		existingMeta.Priority = *req.Priority
	}
	if req.ScaleUpThreshold != nil {
		existingMeta.ScaleUpThreshold = *req.ScaleUpThreshold
	}
	if req.ScaleDownIdleTime != nil {
		existingMeta.ScaleDownIdleTime = *req.ScaleDownIdleTime
	}
	if req.ScaleUpCooldown != nil {
		existingMeta.ScaleUpCooldown = *req.ScaleUpCooldown
	}
	if req.ScaleDownCooldown != nil {
		existingMeta.ScaleDownCooldown = *req.ScaleDownCooldown
	}
	if req.EnableDynamicPrio != nil {
		existingMeta.EnableDynamicPrio = req.EnableDynamicPrio
	}
	if req.HighLoadThreshold != nil {
		existingMeta.HighLoadThreshold = *req.HighLoadThreshold
	}
	if req.PriorityBoost != nil {
		existingMeta.PriorityBoost = *req.PriorityBoost
	}
	if req.AutoscalerEnabled != nil {
		existingMeta.AutoscalerEnabled = req.AutoscalerEnabled
	}

	// Save the updated metadata
	// This will update both endpoints table and autoscaler_configs table
	if err := h.endpointService.SaveEndpoint(c.Request.Context(), existingMeta); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	logger.InfoCtx(c.Request.Context(), "Endpoint configuration updated: %s", name)
	c.JSON(http.StatusOK, gin.H{
		"message": "Endpoint configuration updated successfully",
		"name":    name,
	})
}

// UpdateEndpointDeployment updates endpoint deployment (image, replicas, etc.)
// @Summary Update endpoint deployment
// @Description Update endpoint's K8s deployment, such as upgrading image, adjusting replica count, etc.
// @Tags Endpoints
// @Accept json
// @Produce json
// @Param name path string true "Endpoint name"
// @Param request body interfaces.UpdateDeploymentRequest true "Update request"
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/endpoints/{name}/deployment [patch]
func (h *EndpointHandler) UpdateEndpointDeployment(c *gin.Context) {
	name := c.Param("name")

	var req interfaces.UpdateDeploymentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.ErrorCtx(c.Request.Context(), "Failed to bind update deployment request: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Ensure name matches URL param
	req.Endpoint = name

	logger.InfoCtx(c.Request.Context(), "Updating deployment: endpoint=%s, spec=%s, image=%s, replicas=%v",
		name, req.SpecName, req.Image, req.Replicas)

	var (
		resp *interfaces.DeployResponse
		err  error
	)

	switch {
	case h.endpointService != nil:
		resp, err = h.endpointService.UpdateDeployment(c.Request.Context(), &req)
	case h.deploymentProvider != nil:
		resp, err = h.deploymentProvider.UpdateDeployment(c.Request.Context(), &req)
	default:
		err = fmt.Errorf("deployment provider unavailable")
	}

	if err != nil {
		logger.ErrorCtx(c.Request.Context(), "Failed to update deployment %s: %v", name, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":    err.Error(),
			"endpoint": name,
			"details":  fmt.Sprintf("Update failed: %v", err),
		})
		return
	}

	logger.InfoCtx(c.Request.Context(), "Successfully updated deployment: %s", name)

	c.JSON(http.StatusOK, gin.H{
		"message":  resp.Message,
		"endpoint": resp.Endpoint,
	})
}

// getEndpointFromRuntimeOnly is used when metadata storage is unavailable.
func (h *EndpointHandler) getEndpointFromRuntimeOnly(c *gin.Context, name string) {
	if h.deploymentProvider == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "deployment provider unavailable"})
		return
	}

	app, err := h.deploymentProvider.GetApp(c.Request.Context(), name)
	if err != nil || app == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "endpoint not found"})
		return
	}

	result := &interfaces.EndpointMetadata{
		Name:              app.Name,
		DisplayName:       app.Name,
		Namespace:         app.Namespace,
		Image:             app.Image,
		Replicas:          int(app.Replicas),
		Status:            app.Status,
		ReadyReplicas:     int(app.ReadyReplicas),
		AvailableReplicas: int(app.AvailableReplicas),
	}

	c.JSON(http.StatusOK, result)
}

// listEndpointsFromRuntimeOnly is used when metadata storage is unavailable.
func (h *EndpointHandler) listEndpointsFromRuntimeOnly(c *gin.Context) {
	if h.deploymentProvider == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "deployment provider unavailable"})
		return
	}

	apps, err := h.deploymentProvider.ListApps(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	result := make([]*interfaces.EndpointMetadata, len(apps))
	for i, app := range apps {
		result[i] = &interfaces.EndpointMetadata{
			Name:              app.Name,
			DisplayName:       app.Name,
			Namespace:         app.Namespace,
			Image:             app.Image,
			Replicas:          int(app.Replicas),
			Status:            app.Status,
			ReadyReplicas:     int(app.ReadyReplicas),
			AvailableReplicas: int(app.AvailableReplicas),
		}
	}

	c.JSON(http.StatusOK, result)
}

func (h *EndpointHandler) buildMetadataFromRequest(c *gin.Context, req k8s.DeployAppRequest) *interfaces.EndpointMetadata {
	if h.endpointService == nil {
		return nil
	}

	existingMeta, err := h.endpointService.GetEndpoint(c.Request.Context(), req.Endpoint)
	if err != nil || existingMeta == nil {
		enableDynamicPrio := true
		if req.EnableDynamicPrio != nil {
			enableDynamicPrio = *req.EnableDynamicPrio
		}

		maxReplicas := req.MaxReplicas
		if maxReplicas == 0 {
			maxReplicas = 10
		}

		return &interfaces.EndpointMetadata{
			Name:              req.Endpoint,
			DisplayName:       req.Endpoint,
			SpecName:          req.SpecName,
			Image:             req.Image,
			Replicas:          req.Replicas,
			TaskTimeout:       req.TaskTimeout,
			Status:            "Deploying",
			MinReplicas:       req.MinReplicas,
			MaxReplicas:       maxReplicas,
			ScaleUpThreshold:  req.ScaleUpThreshold,
			ScaleDownIdleTime: req.ScaleDownIdleTime,
			ScaleUpCooldown:   req.ScaleUpCooldown,
			ScaleDownCooldown: req.ScaleDownCooldown,
			Priority:          req.Priority,
			EnableDynamicPrio: &enableDynamicPrio,
			HighLoadThreshold: req.HighLoadThreshold,
			PriorityBoost:     req.PriorityBoost,
		}
	}

	metadata := existingMeta
	metadata.SpecName = req.SpecName
	metadata.Image = req.Image
	metadata.Replicas = req.Replicas
	metadata.TaskTimeout = req.TaskTimeout
	metadata.Status = "Deploying"

	if req.MaxReplicas > 0 {
		metadata.MaxReplicas = req.MaxReplicas
		metadata.MinReplicas = req.MinReplicas
		if req.ScaleUpThreshold > 0 {
			metadata.ScaleUpThreshold = req.ScaleUpThreshold
		}
		if req.ScaleDownIdleTime > 0 {
			metadata.ScaleDownIdleTime = req.ScaleDownIdleTime
		}
		if req.ScaleUpCooldown > 0 {
			metadata.ScaleUpCooldown = req.ScaleUpCooldown
		}
		if req.ScaleDownCooldown > 0 {
			metadata.ScaleDownCooldown = req.ScaleDownCooldown
		}
		if req.Priority > 0 {
			metadata.Priority = req.Priority
		}
		if req.EnableDynamicPrio != nil {
			metadata.EnableDynamicPrio = req.EnableDynamicPrio
		}
		if req.HighLoadThreshold > 0 {
			metadata.HighLoadThreshold = req.HighLoadThreshold
		}
		if req.PriorityBoost > 0 {
			metadata.PriorityBoost = req.PriorityBoost
		}
	}

	return metadata
}

// GetEndpointWorkers gets endpoint workers list (including Pod information)
// @Summary Get endpoint workers (with Pod status)
// @Description Get all workers for specified endpoint, including creating, running, and terminating Pods
// @Tags Endpoints
// @Produce json
// @Param name path string true "Endpoint name"
// @Success 200 {array} WorkerWithPodInfo
// @Router /api/v1/endpoints/{name}/workers [get]
func (h *EndpointHandler) GetEndpointWorkers(c *gin.Context) {
	ctx := c.Request.Context()
	endpoint := c.Param("name")

	if h.workerService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "worker service unavailable"})
		return
	}

	// Get all workers from Redis (worker.ID == pod.Name)
	workers, err := h.workerService.ListWorkers(ctx, endpoint)
	if err != nil {
		logger.ErrorCtx(ctx, "Failed to get workers for endpoint %s: %v", endpoint, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Map: worker.ID (== pod.Name) -> worker
	workerByID := make(map[string]*model.Worker)
	for _, w := range workers {
		if w.ID != "" {
			workerByID[w.ID] = w
		}
	}

	// Get Pods from K8s
	allPods := make([]*interfaces.PodInfo, 0)
	if h.deploymentProvider != nil {
		pods, err := h.deploymentProvider.GetPods(ctx, endpoint)
		if err != nil {
			logger.WarnCtx(ctx, "failed to get pods for endpoint %s: %v", endpoint, err)
		} else {
			allPods = pods
		}
	}

	// Map: pod.Name -> PodInfo
	podByName := make(map[string]*interfaces.PodInfo)
	for _, pod := range allPods {
		podByName[pod.Name] = pod
	}

	// Merge worker info and pod info
	// Worker.ID == Pod.Name, use Pod as primary entity, Worker info as supplement
	type WorkerWithPodInfo struct {
		model.Worker
		PodPhase          string `json:"podPhase,omitempty"`
		PodStatus         string `json:"podStatus,omitempty"`
		PodReason         string `json:"podReason,omitempty"`
		PodMessage        string `json:"podMessage,omitempty"`
		PodIP             string `json:"podIP,omitempty"`
		PodNodeName       string `json:"podNodeName,omitempty"`
		PodCreatedAt      string `json:"podCreatedAt,omitempty"`
		PodStartedAt      string `json:"podStartedAt,omitempty"`
		PodRestartCount   int32  `json:"podRestartCount,omitempty"`
		DeletionTimestamp string `json:"deletionTimestamp,omitempty"`
	}

	result := make([]WorkerWithPodInfo, 0)

	// Iterate all Pods (use Pod as primary entity)
	for _, pod := range allPods {
		var workerWithPod WorkerWithPodInfo

		// Try to match Worker (worker.ID == pod.Name)
		if worker, exists := workerByID[pod.Name]; exists {
			// Pod matched to Worker (Worker registered and sent heartbeat)
			workerWithPod = WorkerWithPodInfo{
				Worker: *worker,
			}
		} else {
			// Pod has no matching Worker (creating or offline)
			workerWithPod = WorkerWithPodInfo{
				Worker: model.Worker{
					ID:       pod.Name,
					Endpoint: endpoint,
					Status:   model.WorkerStatusOffline, // Worker not registered or offline
					PodName:  pod.Name,
				},
			}
		}

		// Attach Pod information
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

		result = append(result, workerWithPod)
	}

	c.JSON(http.StatusOK, result)
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins, production should use stricter checks
	},
}

// ExecWorker executes commands in worker Pod via WebSocket
// @Summary Worker Pod Exec
// @Description WebSocket connection to exec into worker pod
// @Tags Endpoints
// @Param name path string true "Endpoint name"
// @Param worker_id query string true "Worker ID (Pod Name)"
// @Router /api/v1/endpoints/{name}/workers/exec [get]
func (h *EndpointHandler) ExecWorker(c *gin.Context) {
	workerID := c.Query("worker_id")
	if workerID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "worker_id is required"})
		return
	}

	// Get K8s provider
	k8sProvider, ok := h.deploymentProvider.(*k8s.K8sDeploymentProvider)
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "K8s provider not available"})
		return
	}

	config := k8sProvider.GetRestConfig()
	clientset := k8sProvider.GetClientset()
	namespace := k8sProvider.GetNamespace()

	if config == nil || clientset == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "K8s configuration not available"})
		return
	}

	// Upgrade to WebSocket
	ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logger.ErrorCtx(c.Request.Context(), "Failed to upgrade to websocket: %v", err)
		return
	}
	defer ws.Close()

	// Get endpoint name from URL path parameter
	endpointName := c.Param("name")
	// Default container name: {endpoint}-worker
	containerName := endpointName + "-worker"

	// Create exec request
	req := clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(workerID).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: containerName,
			Command:   []string{"/bin/bash"},
			Stdin:     true,
			Stdout:    true,
			Stderr:    true,
			TTY:       true,
		}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		logger.ErrorCtx(c.Request.Context(), "Failed to create executor: %v", err)
		ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Error: %v\n", err)))
		return
	}

	// Create terminal handler
	termHandler := &terminalHandler{
		ws:       ws,
		resizeCh: make(chan remotecommand.TerminalSize),
	}

	// Execute
	err = executor.StreamWithContext(c.Request.Context(), remotecommand.StreamOptions{
		Stdin:             termHandler,
		Stdout:            termHandler,
		Stderr:            termHandler,
		Tty:               true,
		TerminalSizeQueue: termHandler,
	})

	if err != nil {
		logger.ErrorCtx(c.Request.Context(), "Exec failed: %v", err)
		ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("\r\nExec ended: %v\n", err)))
	}
}

// terminalHandler handles WebSocket terminal I/O
type terminalHandler struct {
	ws       *websocket.Conn
	resizeCh chan remotecommand.TerminalSize
}

func (t *terminalHandler) Read(p []byte) (int, error) {
	_, msg, err := t.ws.ReadMessage()
	if err != nil {
		return 0, err
	}
	return copy(p, msg), nil
}

func (t *terminalHandler) Write(p []byte) (int, error) {
	err := t.ws.WriteMessage(websocket.BinaryMessage, p)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func (t *terminalHandler) Next() *remotecommand.TerminalSize {
	size := <-t.resizeCh
	return &size
}
