package router

import (
	"waverless/app/handler"
	"waverless/app/middleware"

	"github.com/gin-gonic/gin"
)

// Router Router
type Router struct {
	taskHandler       *handler.TaskHandler
	workerHandler     *handler.WorkerHandler
	endpointHandler   *handler.EndpointHandler
	autoscalerHandler *handler.AutoScalerHandler
	statisticsHandler *handler.StatisticsHandler
	specHandler       *handler.SpecHandler
	imageHandler      *handler.ImageHandler
	monitoringHandler *handler.MonitoringHandler
}

// NewRouter creates a new Router
func NewRouter(taskHandler *handler.TaskHandler, workerHandler *handler.WorkerHandler, endpointHandler *handler.EndpointHandler, autoscalerHandler *handler.AutoScalerHandler, statisticsHandler *handler.StatisticsHandler, specHandler *handler.SpecHandler, imageHandler *handler.ImageHandler, monitoringHandler *handler.MonitoringHandler) *Router {
	return &Router{
		taskHandler:       taskHandler,
		workerHandler:     workerHandler,
		endpointHandler:   endpointHandler,
		autoscalerHandler: autoscalerHandler,
		statisticsHandler: statisticsHandler,
		specHandler:       specHandler,
		imageHandler:      imageHandler,
		monitoringHandler: monitoringHandler,
	}
}

// Setup sets up routes
func (r *Router) Setup(engine *gin.Engine) {
	engine.Use(middleware.Recovery())
	engine.Use(middleware.Logger())
	// V1 API - Client task management interface
	v1 := engine.Group("/v1")
	{
		// Global task query interface (no endpoint required)
		v1.GET("/status/:task_id", r.taskHandler.Status)
		v1.POST("/cancel/:task_id", r.taskHandler.Cancel)
		v1.GET("/tasks", r.taskHandler.ListTasks) // List tasks with optional filtering

		// Worker management interface
		v1.GET("/workers", r.workerHandler.GetWorkerList)

		// Endpoint-specific routes (endpoint required)
		endpoint := v1.Group("/:endpoint")
		{
			endpoint.POST("/run", r.taskHandler.SubmitWithEndpoint)
			endpoint.POST("/runsync", r.taskHandler.SubmitSyncWithEndpoint)
			endpoint.GET("/status/:task_id", r.taskHandler.Status)       // Reuse existing
			endpoint.POST("/cancel/:task_id", r.taskHandler.Cancel)      // Reuse existing
			endpoint.GET("/stats", r.taskHandler.GetEndpointStats)       // endpoint statistics
			endpoint.GET("/check", r.taskHandler.CheckSubmitEligibility) // check if task submission is recommended

			// Monitoring APIs
			if r.monitoringHandler != nil {
				endpoint.GET("/metrics/realtime", r.monitoringHandler.GetRealtimeMetrics)
				endpoint.GET("/metrics/stats", r.monitoringHandler.GetStats)
			}
		}
	}

	// V2 API - RunPod Worker compatible interface (endpoint required)
	v2 := engine.Group("/v2/:endpoint")
	v2.Use(middleware.AuthMiddleware()) // Add simple token authentication
	{
		// Task pulling
		v2.GET("/job-take/:worker_id", r.workerHandler.PullJobs)
		v2.GET("/job-take-batch/:worker_id", r.workerHandler.PullJobs) // Batch pull

		// Heartbeat
		v2.GET("/ping/:worker_id", r.workerHandler.Heartbeat)

		// Result submission (task_id in URL path)
		v2.POST("/job-done/:worker_id/:task_id", r.workerHandler.SubmitResult)
		v2.POST("/job-stream/:worker_id/:task_id", r.workerHandler.SubmitResult)
	}

	// API v1 - Endpoint management interface (K8s or Novita, if enabled)
	if r.endpointHandler != nil {
		api := engine.Group("/api/v1")
		{
			// Worker detail API (by database ID, regardless of status)
			api.GET("/workers/:id", r.workerHandler.GetWorkerByID)

			// Endpoint lifecycle management
			endpoints := api.Group("/endpoints")
			{
				endpoints.POST("", r.endpointHandler.CreateEndpoint)                               // Create endpoint (metadata + deployment)
				endpoints.POST("/preview", r.endpointHandler.PreviewDeploymentYAML)                // Preview YAML
				endpoints.GET("", r.endpointHandler.ListEndpoints)                                 // List endpoints
				endpoints.GET("/:name", r.endpointHandler.GetEndpoint)                             // Get endpoint detail
				endpoints.PUT("/:name", r.endpointHandler.UpdateEndpoint)                          // Update metadata
				endpoints.PATCH("/:name/deployment", r.endpointHandler.UpdateEndpointDeployment)   // Update deployment
				endpoints.DELETE("/:name", r.endpointHandler.DeleteEndpoint)                       // Delete endpoint
				endpoints.GET("/:name/logs", r.endpointHandler.GetEndpointLogs)                    // Logs
				endpoints.GET("/:name/workers", r.endpointHandler.GetEndpointWorkers)              // Workers
				endpoints.GET("/:name/workers/sync", r.endpointHandler.GetEndpointWorkersForSync)  // Workers for Portal sync (includes recently terminated)
				endpoints.GET("/:name/workers/:pod_name/describe", r.workerHandler.DescribeWorker) // Describe Worker (Pod detail)
				endpoints.GET("/:name/workers/:pod_name/yaml", r.workerHandler.GetWorkerYAML)      // Get Worker Pod YAML
				endpoints.GET("/:name/workers/exec", r.endpointHandler.ExecWorker)                 // Worker Exec (WebSocket)

				// Image update check
				if r.imageHandler != nil {
					endpoints.POST("/:name/check-image", r.imageHandler.CheckImageUpdate) // Check image update for specific endpoint
					endpoints.POST("/check-images", r.imageHandler.CheckAllImagesUpdate)  // Check image updates for all endpoints
				}
			}

			// Task history APIs
			tasks := api.Group("/tasks")
			{
				tasks.GET("/:task_id/execution-history", r.taskHandler.GetTaskExecutionHistory) // Get execution history (extend field)
				tasks.GET("/:task_id/events", r.taskHandler.GetTaskEvents)                      // Get all events
				tasks.GET("/:task_id/timeline", r.taskHandler.GetTaskTimeline)                  // Get timeline
			}

			// Spec management APIs (CRUD, from database)
			if r.specHandler != nil {
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

			// K8s resources APIs
			k8s := api.Group("/k8s")
			{
				k8s.GET("/pvcs", r.endpointHandler.ListPVCs) // List PVCs
			}

			// Configuration APIs
			config := api.Group("/config")
			{
				config.GET("/default-env", r.endpointHandler.GetDefaultEnv) // Get default environment variables from ConfigMap
			}

			// Webhook APIs
			if r.imageHandler != nil {
				webhooks := api.Group("/webhooks")
				{
					webhooks.POST("/dockerhub", r.imageHandler.DockerHubWebhook) // DockerHub webhook
				}
			}

			// AutoScaler management
			if r.autoscalerHandler != nil {
				autoscaler := api.Group("/autoscaler")
				{
					// Full status (legacy, prefer using separate endpoints below)
					autoscaler.GET("/status", r.autoscalerHandler.GetStatus)

					// Lightweight endpoints for better performance
					autoscaler.GET("/cluster-resources", r.autoscalerHandler.GetClusterResources) // Cluster resources only
					autoscaler.GET("/recent-events", r.autoscalerHandler.GetRecentEvents)         // Recent events only

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

			// Statistics APIs
			if r.statisticsHandler != nil {
				statistics := api.Group("/statistics")
				{
					statistics.GET("/overview", r.statisticsHandler.GetOverview)                      // Global statistics
					statistics.GET("/endpoints", r.statisticsHandler.GetTopEndpoints)                 // Top endpoints by task volume
					statistics.GET("/endpoints/:endpoint", r.statisticsHandler.GetEndpointStatistics) // Specific endpoint statistics
				}
			}
		}
	}

	// Health check
	engine.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})
}
