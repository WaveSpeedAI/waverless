package handler

import (
	"net/http"
	"strconv"

	endpointsvc "waverless/internal/service/endpoint"
	"waverless/pkg/autoscaler"
	"waverless/pkg/interfaces"
	"waverless/pkg/logger"

	"github.com/gin-gonic/gin"
)

// AutoScalerHandler handles autoscaling operations
type AutoScalerHandler struct{
	manager         *autoscaler.Manager
	endpointService *endpointsvc.Service
}

// NewAutoScalerHandler creates autoscaler handler
func NewAutoScalerHandler(manager *autoscaler.Manager, endpointService *endpointsvc.Service) *AutoScalerHandler {
	return &AutoScalerHandler{
		manager:         manager,
		endpointService: endpointService,
	}
}

// GetStatus gets autoscaler status
// @Summary Get autoscaler status
// @Description Get current autoscaler system status, including cluster resources, endpoint status, etc.
// @Tags AutoScaler
// @Produce json
// @Success 200 {object} autoscaler.AutoScalerStatus
// @Router /api/v1/autoscaler/status [get]
func (h *AutoScalerHandler) GetStatus(c *gin.Context) {
	status, err := h.manager.GetStatus(c.Request.Context())
	if err != nil {
		logger.ErrorCtx(c.Request.Context(), "failed to get autoscaler status: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, status)
}

// GetClusterResources gets cluster resource status (lightweight interface)
// @Summary Get cluster resource status
// @Description Returns only cluster resource usage, without endpoint details and events
// @Tags AutoScaler
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /api/v1/autoscaler/cluster-resources [get]
func (h *AutoScalerHandler) GetClusterResources(c *gin.Context) {
	status, err := h.manager.GetStatus(c.Request.Context())
	if err != nil {
		logger.ErrorCtx(c.Request.Context(), "failed to get autoscaler status: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Only return cluster resources
	c.JSON(http.StatusOK, gin.H{
		"enabled":          status.Enabled,
		"running":          status.Running,
		"lastRunTime":      status.LastRunTime,
		"clusterResources": status.ClusterResources,
	})
}

// GetRecentEvents gets recent scaling events (lightweight interface)
// @Summary Get recent scaling events
// @Description Get recent scaling events for all endpoints
// @Tags AutoScaler
// @Param limit query int false "Event limit (default 10)"
// @Produce json
// @Success 200 {array} autoscaler.ScalingEvent
// @Router /api/v1/autoscaler/recent-events [get]
func (h *AutoScalerHandler) GetRecentEvents(c *gin.Context) {
	limit := 10
	if limitStr := c.Query("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	events, err := h.manager.GetScalingHistory(c.Request.Context(), "", limit)
	if err != nil {
		logger.ErrorCtx(c.Request.Context(), "failed to get recent events: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, events)
}

// Enable enables autoscaler
// @Summary Enable autoscaler
// @Description Enable autoscaling functionality
// @Tags AutoScaler
// @Produce json
// @Success 200 {object} map[string]string
// @Router /api/v1/autoscaler/enable [post]
func (h *AutoScalerHandler) Enable(c *gin.Context) {
	h.manager.Enable()
	logger.InfoCtx(c.Request.Context(), "autoscaler enabled")
	c.JSON(http.StatusOK, gin.H{"status": "enabled"})
}

// Disable disables autoscaler
// @Summary Disable autoscaler
// @Description Disable autoscaling functionality
// @Tags AutoScaler
// @Produce json
// @Success 200 {object} map[string]string
// @Router /api/v1/autoscaler/disable [post]
func (h *AutoScalerHandler) Disable(c *gin.Context) {
	h.manager.Disable()
	logger.InfoCtx(c.Request.Context(), "autoscaler disabled")
	c.JSON(http.StatusOK, gin.H{"status": "disabled"})
}

// TriggerScale manually triggers scaling
// @Summary Manually trigger scaling
// @Description Manually trigger one scaling decision and execution
// @Tags AutoScaler
// @Param name path string false "Endpoint name (optional, empty means all)"
// @Produce json
// @Success 200 {object} map[string]string
// @Router /api/v1/autoscaler/trigger [post]
// @Router /api/v1/autoscaler/trigger/{name} [post]
func (h *AutoScalerHandler) TriggerScale(c *gin.Context) {
	endpoint := c.Param("name")

	if err := h.manager.TriggerScale(c.Request.Context(), endpoint); err != nil {
		logger.ErrorCtx(c.Request.Context(), "failed to trigger scale: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	logger.InfoCtx(c.Request.Context(), "scale triggered for endpoint: %s", endpoint)
	c.JSON(http.StatusOK, gin.H{"status": "triggered"})
}

// GetHistory gets scaling history
// @Summary Get scaling history
// @Description Get scaling history events for specified endpoint
// @Tags AutoScaler
// @Param name path string true "Endpoint name"
// @Param limit query int false "Limit (default 20)"
// @Produce json
// @Success 200 {array} autoscaler.ScalingEvent
// @Router /api/v1/autoscaler/history/{name} [get]
func (h *AutoScalerHandler) GetHistory(c *gin.Context) {
	endpoint := c.Param("name")
	limit := 20
	if limitStr := c.Query("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil {
			limit = l
		}
	}

	events, err := h.manager.GetScalingHistory(c.Request.Context(), endpoint, limit)
	if err != nil {
		logger.ErrorCtx(c.Request.Context(), "failed to get scaling history: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, events)
}

// UpdateEndpointConfig updates endpoint autoscaling configuration
// @Summary Update endpoint autoscaling configuration
// @Description Update autoscaling configuration for specified endpoint
// @Tags AutoScaler
// @Param name path string true "Endpoint name"
// @Param config body interfaces.EndpointMetadata true "Endpoint configuration"
// @Produce json
// @Success 200 {object} map[string]string
// @Router /api/v1/autoscaler/endpoints/{name} [put]
func (h *AutoScalerHandler) UpdateEndpointConfig(c *gin.Context) {
	name := c.Param("name")

	var updates interfaces.EndpointMetadata
	if err := c.ShouldBindJSON(&updates); err != nil {
		logger.ErrorCtx(c.Request.Context(), "invalid request: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	// Get existing metadata to preserve fields not being updated
	existingMeta, err := h.endpointService.GetEndpoint(c.Request.Context(), name)
	if err != nil {
		logger.ErrorCtx(c.Request.Context(), "failed to get endpoint: %v", err)
		c.JSON(http.StatusNotFound, gin.H{"error": "endpoint not found"})
		return
	}

	// Merge autoscaling config updates into existing metadata
	// Only update fields that are explicitly provided (non-zero)
	if updates.MinReplicas >= 0 {
		existingMeta.MinReplicas = updates.MinReplicas
	}
	if updates.MaxReplicas > 0 {
		existingMeta.MaxReplicas = updates.MaxReplicas
	}
	if updates.ScaleUpThreshold > 0 {
		existingMeta.ScaleUpThreshold = updates.ScaleUpThreshold
	}
	if updates.ScaleDownIdleTime > 0 {
		existingMeta.ScaleDownIdleTime = updates.ScaleDownIdleTime
	}
	if updates.ScaleUpCooldown > 0 {
		existingMeta.ScaleUpCooldown = updates.ScaleUpCooldown
	}
	if updates.ScaleDownCooldown > 0 {
		existingMeta.ScaleDownCooldown = updates.ScaleDownCooldown
	}
	if updates.Priority >= 0 {
		existingMeta.Priority = updates.Priority
	}
	if updates.HighLoadThreshold > 0 {
		existingMeta.HighLoadThreshold = updates.HighLoadThreshold
	}
	if updates.PriorityBoost > 0 {
		existingMeta.PriorityBoost = updates.PriorityBoost
	}
	if updates.EnableDynamicPrio != nil {
		existingMeta.EnableDynamicPrio = updates.EnableDynamicPrio
	}
	// Update autoscaler enabled override (three-state: nil/default, "disabled", "enabled")
	if updates.AutoscalerEnabled != nil {
		existingMeta.AutoscalerEnabled = updates.AutoscalerEnabled
	}

	// Also update basic fields if provided
	if updates.DisplayName != "" {
		existingMeta.DisplayName = updates.DisplayName
	}
	if updates.Description != "" {
		existingMeta.Description = updates.Description
	}
	if updates.SpecName != "" {
		existingMeta.SpecName = updates.SpecName
	}
	if updates.Image != "" {
		existingMeta.Image = updates.Image
	}
	if updates.Replicas > 0 {
		existingMeta.Replicas = updates.Replicas
	}
	if updates.TaskTimeout > 0 {
		existingMeta.TaskTimeout = updates.TaskTimeout
	}

	// Update metadata
	if err := h.endpointService.UpdateEndpoint(c.Request.Context(), existingMeta); err != nil {
		logger.ErrorCtx(c.Request.Context(), "failed to update endpoint config: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	logger.InfoCtx(c.Request.Context(), "endpoint config updated: %s (maxReplicas=%d, minReplicas=%d, priority=%d)",
		name, existingMeta.MaxReplicas, existingMeta.MinReplicas, existingMeta.Priority)
	c.JSON(http.StatusOK, gin.H{"status": "updated"})
}

// GetEndpointConfig gets endpoint autoscaling configuration
// @Summary Get endpoint autoscaling configuration
// @Description Get autoscaling configuration for specified endpoint
// @Tags AutoScaler
// @Param name path string true "Endpoint name"
// @Produce json
// @Success 200 {object} interfaces.EndpointMetadata
// @Router /api/v1/autoscaler/endpoints/{name} [get]
func (h *AutoScalerHandler) GetEndpointConfig(c *gin.Context) {
	name := c.Param("name")

	config, err := h.endpointService.GetEndpoint(c.Request.Context(), name)
	if err != nil {
		logger.ErrorCtx(c.Request.Context(), "failed to get endpoint config: %v", err)
		c.JSON(http.StatusNotFound, gin.H{"error": "endpoint not found"})
		return
	}

	c.JSON(http.StatusOK, config)
}

// ListEndpoints lists all endpoint configurations
// @Summary List all endpoint configurations
// @Description List autoscaling configurations for all endpoints
// @Tags AutoScaler
// @Produce json
// @Success 200 {array} interfaces.EndpointMetadata
// @Router /api/v1/autoscaler/endpoints [get]
func (h *AutoScalerHandler) ListEndpoints(c *gin.Context) {
	endpoints, err := h.endpointService.ListEndpoints(c.Request.Context())
	if err != nil {
		logger.ErrorCtx(c.Request.Context(), "failed to list endpoints: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, endpoints)
}

// GetGlobalConfig gets global configuration
// @Summary Get global configuration
// @Description Get autoscaler global configuration parameters
// @Tags AutoScaler
// @Produce json
// @Success 200 {object} autoscaler.Config
// @Router /api/v1/autoscaler/config [get]
func (h *AutoScalerHandler) GetGlobalConfig(c *gin.Context) {
	config := h.manager.GetGlobalConfig()
	c.JSON(http.StatusOK, config)
}

// UpdateGlobalConfig updates global configuration
// @Summary Update global configuration
// @Description Update autoscaler global configuration parameters (interval, max_gpu_count, max_cpu_cores, max_memory_gb, starvation_time)
// @Tags AutoScaler
// @Param config body autoscaler.Config true "Global configuration"
// @Produce json
// @Success 200 {object} map[string]string
// @Router /api/v1/autoscaler/config [put]
func (h *AutoScalerHandler) UpdateGlobalConfig(c *gin.Context) {
	var config autoscaler.Config
	if err := c.ShouldBindJSON(&config); err != nil {
		logger.ErrorCtx(c.Request.Context(), "invalid request: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	if err := h.manager.UpdateGlobalConfig(c.Request.Context(), &config); err != nil {
		logger.ErrorCtx(c.Request.Context(), "failed to update global config: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	logger.InfoCtx(c.Request.Context(), "global config updated")
	c.JSON(http.StatusOK, gin.H{"status": "updated"})
}
