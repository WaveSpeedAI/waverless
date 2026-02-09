package router

import (
	"github.com/gin-gonic/gin"
)

// setupAdminRoutes sets up admin API routes
// Caller: Web UI frontend
// Auth: Optional (internal network)
// Purpose: Endpoint, Task, Spec, AutoScaler management interfaces
func (r *Router) setupAdminRoutes(engine *gin.Engine) {
	// Only register admin routes when endpointHandler is available
	if r.endpointHandler == nil {
		return
	}

	api := engine.Group("/api/v1")
	{
		// Worker detail API (query by database ID, no status filter)
		api.GET("/workers/:id", r.workerHandler.GetWorkerByID)

		// Worker status events (Requirements 3.4, 3.5)
		if r.statusEventHandler != nil {
			api.GET("/workers/:id/status-events", r.statusEventHandler.GetWorkerStatusEvents)
		}

		// Register feature module routes
		r.setupEndpointRoutes(api)
		r.setupTaskRoutes(api)
		r.setupSpecRoutes(api)
		r.setupK8sRoutes(api)
		r.setupConfigRoutes(api)
		r.setupWebhookRoutes(api)
		r.setupAutoscalerRoutes(api)
		r.setupStatisticsRoutes(api)
	}
}

// setupEndpointRoutes sets up endpoint management routes
func (r *Router) setupEndpointRoutes(api *gin.RouterGroup) {
	endpoints := api.Group("/endpoints")
	{
		// Endpoint lifecycle management
		endpoints.POST("", r.endpointHandler.CreateEndpoint)                             // Create endpoint
		endpoints.POST("/preview", r.endpointHandler.PreviewDeploymentYAML)              // Preview YAML
		endpoints.GET("", r.endpointHandler.ListEndpoints)                               // List endpoints
		endpoints.GET("/:name", r.endpointHandler.GetEndpoint)                           // Get endpoint details
		endpoints.PUT("/:name", r.endpointHandler.UpdateEndpoint)                        // Update metadata
		endpoints.PATCH("/:name/deployment", r.endpointHandler.UpdateEndpointDeployment) // Update deployment
		endpoints.DELETE("/:name", r.endpointHandler.DeleteEndpoint)                     // Delete endpoint
		endpoints.GET("/:name/logs", r.endpointHandler.GetEndpointLogs)                  // Get logs

		// Worker management
		endpoints.GET("/:name/workers", r.endpointHandler.GetEndpointWorkers)              // Get workers
		endpoints.GET("/:name/workers/sync", r.endpointHandler.GetEndpointWorkersForSync)  // For Portal sync
		endpoints.GET("/:name/workers/:pod_name/describe", r.workerHandler.DescribeWorker) // Describe worker
		endpoints.GET("/:name/workers/:pod_name/yaml", r.workerHandler.GetWorkerYAML)      // Get worker YAML
		endpoints.GET("/:name/workers/exec", r.endpointHandler.ExecWorker)                 // Worker Exec (WebSocket)

		// Status events (Requirements 3.4, 3.5)
		if r.statusEventHandler != nil {
			endpoints.GET("/:name/status-events", r.statusEventHandler.ListStatusEvents) // List status events for endpoint
		}

		// Image update check
		if r.imageHandler != nil {
			endpoints.POST("/:name/check-image", r.imageHandler.CheckImageUpdate) // Check single endpoint image update
			endpoints.POST("/check-images", r.imageHandler.CheckAllImagesUpdate)  // Check all endpoints image update
		}
	}
}

// setupTaskRoutes sets up task history query routes
func (r *Router) setupTaskRoutes(api *gin.RouterGroup) {
	tasks := api.Group("/tasks")
	{
		tasks.GET("/:task_id/execution-history", r.taskHandler.GetTaskExecutionHistory) // Get execution history
		tasks.GET("/:task_id/events", r.taskHandler.GetTaskEvents)                      // Get all events
		tasks.GET("/:task_id/timeline", r.taskHandler.GetTaskTimeline)                  // Get timeline
	}
}

// setupSpecRoutes sets up spec management routes
func (r *Router) setupSpecRoutes(api *gin.RouterGroup) {
	if r.specHandler == nil {
		return
	}

	specs := api.Group("/specs")
	{
		specs.GET("/capacity", r.specHandler.ListSpecsWithCapacity) // List specs with capacity (must be before /:name)
		specs.POST("", r.specHandler.CreateSpec)                    // Create spec
		specs.GET("", r.specHandler.ListSpecs)                      // List specs
		specs.GET("/:name", r.specHandler.GetSpec)                  // Get spec
		specs.GET("/:name/capacity", r.specHandler.GetSpecCapacity) // Get spec capacity
		specs.PUT("/:name", r.specHandler.UpdateSpec)               // Update spec
		specs.DELETE("/:name", r.specHandler.DeleteSpec)            // Delete spec
	}
}

// setupK8sRoutes sets up K8s resource routes
func (r *Router) setupK8sRoutes(api *gin.RouterGroup) {
	k8s := api.Group("/k8s")
	{
		k8s.GET("/pvcs", r.endpointHandler.ListPVCs) // List PVCs
	}
}

// setupConfigRoutes sets up configuration routes
func (r *Router) setupConfigRoutes(api *gin.RouterGroup) {
	config := api.Group("/config")
	{
		config.GET("/default-env", r.endpointHandler.GetDefaultEnv) // Get default environment variables
	}
}

// setupWebhookRoutes sets up webhook routes
func (r *Router) setupWebhookRoutes(api *gin.RouterGroup) {
	if r.imageHandler == nil {
		return
	}

	webhooks := api.Group("/webhooks")
	{
		webhooks.POST("/dockerhub", r.imageHandler.DockerHubWebhook) // DockerHub webhook
	}
}

// setupAutoscalerRoutes sets up autoscaler management routes
func (r *Router) setupAutoscalerRoutes(api *gin.RouterGroup) {
	if r.autoscalerHandler == nil {
		return
	}

	autoscaler := api.Group("/autoscaler")
	{
		// Status queries
		autoscaler.GET("/status", r.autoscalerHandler.GetStatus)                      // Full status
		autoscaler.GET("/cluster-resources", r.autoscalerHandler.GetClusterResources) // Cluster resources
		autoscaler.GET("/recent-events", r.autoscalerHandler.GetRecentEvents)         // Recent events

		// Control
		autoscaler.POST("/enable", r.autoscalerHandler.Enable)
		autoscaler.POST("/disable", r.autoscalerHandler.Disable)
		autoscaler.POST("/trigger", r.autoscalerHandler.TriggerScale)
		autoscaler.POST("/trigger/:name", r.autoscalerHandler.TriggerScale)

		// Configuration
		autoscaler.GET("/config", r.autoscalerHandler.GetGlobalConfig)
		autoscaler.PUT("/config", r.autoscalerHandler.UpdateGlobalConfig)
		autoscaler.GET("/endpoints", r.autoscalerHandler.ListEndpoints)
		autoscaler.GET("/endpoints/:name", r.autoscalerHandler.GetEndpointConfig)
		autoscaler.PUT("/endpoints/:name", r.autoscalerHandler.UpdateEndpointConfig)

		// History
		autoscaler.GET("/history/:name", r.autoscalerHandler.GetHistory)
	}
}

// setupStatisticsRoutes sets up statistics routes
func (r *Router) setupStatisticsRoutes(api *gin.RouterGroup) {
	if r.statisticsHandler == nil {
		return
	}

	statistics := api.Group("/statistics")
	{
		statistics.GET("/overview", r.statisticsHandler.GetOverview)                      // Global statistics
		statistics.GET("/endpoints", r.statisticsHandler.GetTopEndpoints)                 // Top endpoints
		statistics.GET("/endpoints/:endpoint", r.statisticsHandler.GetEndpointStatistics) // Specific endpoint statistics
	}
}

// setupHealthRoutes sets up health check routes
func (r *Router) setupHealthRoutes(engine *gin.Engine) {
	engine.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})
}
