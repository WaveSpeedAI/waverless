package router

import (
	"waverless/app/middleware"

	"github.com/gin-gonic/gin"
)

// setupV2Routes sets up Worker internal communication API routes
// Caller: Worker Pod (RunPod compatible)
// Auth: API Key + Worker ID verification
// Purpose: Task pulling, heartbeat, result submission
func (r *Router) setupV2Routes(engine *gin.Engine) {
	v2 := engine.Group("/v2/:endpoint")
	v2.Use(middleware.AuthMiddleware()) // Add token authentication
	{
		// Task pulling
		// GET /v2/:endpoint/job-take/:worker_id - Pull single task
		v2.GET("/job-take/:worker_id", r.workerHandler.PullJobs)
		// GET /v2/:endpoint/job-take-batch/:worker_id - Batch pull tasks
		v2.GET("/job-take-batch/:worker_id", r.workerHandler.PullJobs)

		// Heartbeat
		// GET /v2/:endpoint/ping/:worker_id - Worker heartbeat
		v2.GET("/ping/:worker_id", r.workerHandler.Heartbeat)

		// Result submission (task_id in URL path)
		// POST /v2/:endpoint/job-done/:worker_id/:task_id - Submit task result
		v2.POST("/job-done/:worker_id/:task_id", r.workerHandler.SubmitResult)
		// POST /v2/:endpoint/job-stream/:worker_id/:task_id - Stream submit task result
		v2.POST("/job-stream/:worker_id/:task_id", r.workerHandler.SubmitResult)
	}
}
