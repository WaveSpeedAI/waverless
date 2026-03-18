package router

import (
	"waverless/app/middleware"

	"github.com/gin-gonic/gin"
)

// setupPortalRoutes sets up portal southbound API routes.
// Caller: Waverless-Portal control plane
// Auth: API Key + portal request headers
// Purpose: Stable southbound APIs for portal integration
func (r *Router) setupPortalRoutes(engine *gin.Engine) {
	if r.portalHandler == nil {
		return
	}

	portal := engine.Group("/portal/v1")
	portal.Use(middleware.PortalAuthMiddleware())
	{
		instance := portal.Group("/instance")
		{
			instance.GET("/info", r.portalHandler.GetInstanceInfo)
			instance.GET("/health", r.portalHandler.GetInstanceHealth)
		}

		tasks := portal.Group("/tasks")
		{
			tasks.GET("/:task_id", r.portalHandler.GetTask)
			tasks.POST("/:task_id/cancel", r.portalHandler.CancelTask)
		}

		workers := portal.Group("/workers")
		{
			workers.GET("/:worker_id", r.portalHandler.GetWorker)
		}

		endpoints := portal.Group("/endpoints")
		{
			endpoints.GET("/by-name/:name", r.portalHandler.GetEndpointByName)
			endpoints.GET("/by-name/:name/workers/sync", r.portalHandler.GetEndpointWorkersForSyncByName)
		}
	}
}
