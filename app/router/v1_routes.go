package router

import (
	"github.com/gin-gonic/gin"
)

// setupV1Routes sets up client API routes
// Caller: External clients, Portal passthrough
// Auth: API Key (optional)
// Purpose: Task submission, status query, cancellation and other client interfaces
func (r *Router) setupV1Routes(engine *gin.Engine) {
	v1 := engine.Group("/v1")
	{
		// Global task query interfaces (no endpoint required)
		// GET /v1/status/:task_id - Query task status
		v1.GET("/status/:task_id", r.taskHandler.Status)
		// POST /v1/cancel/:task_id - Cancel task
		v1.POST("/cancel/:task_id", r.taskHandler.Cancel)
		// GET /v1/tasks - List tasks (with filtering)
		v1.GET("/tasks", r.taskHandler.ListTasks)

		// Worker management interfaces
		// GET /v1/workers - Get worker list
		v1.GET("/workers", r.workerHandler.GetWorkerList)

		// Endpoint-level routes (require endpoint parameter)
		r.setupV1EndpointRoutes(v1)
	}
}

// setupV1EndpointRoutes sets up V1 endpoint-level routes
// These routes require an endpoint parameter
func (r *Router) setupV1EndpointRoutes(v1 *gin.RouterGroup) {
	endpoint := v1.Group("/:endpoint")
	{
		// POST /:endpoint/run - Async task submission
		endpoint.POST("/run", r.taskHandler.SubmitWithEndpoint)
		// POST /:endpoint/runsync - Sync task submission
		endpoint.POST("/runsync", r.taskHandler.SubmitSyncWithEndpoint)
		// GET /:endpoint/status/:task_id - Query task status (with endpoint)
		endpoint.GET("/status/:task_id", r.taskHandler.Status)
		// POST /:endpoint/cancel/:task_id - Cancel task (with endpoint)
		endpoint.POST("/cancel/:task_id", r.taskHandler.Cancel)
		// GET /:endpoint/stats - Get endpoint statistics
		endpoint.GET("/stats", r.taskHandler.GetEndpointStats)
		// GET /:endpoint/check - Check if task submission is recommended
		endpoint.GET("/check", r.taskHandler.CheckSubmitEligibility)

		// Monitoring API
		if r.monitoringHandler != nil {
			// GET /:endpoint/metrics/realtime - Realtime metrics
			endpoint.GET("/metrics/realtime", r.monitoringHandler.GetRealtimeMetrics)
			// GET /:endpoint/metrics/stats - Statistics metrics
			endpoint.GET("/metrics/stats", r.monitoringHandler.GetStats)
		}
	}
}
