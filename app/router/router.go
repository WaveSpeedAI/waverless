package router

import (
	"waverless/app/handler"
	"waverless/app/middleware"

	"github.com/gin-gonic/gin"
)

// Handlers aggregates all handlers
type Handlers struct {
	Task        *handler.TaskHandler
	Worker      *handler.WorkerHandler
	Endpoint    *handler.EndpointHandler
	Autoscaler  *handler.AutoScalerHandler
	Statistics  *handler.StatisticsHandler
	Spec        *handler.SpecHandler
	Image       *handler.ImageHandler
	Monitoring  *handler.MonitoringHandler
	StatusEvent *handler.StatusEventHandler
}

// Router is the main router structure
type Router struct {
	handlers *Handlers
	// Keep old fields for backward compatibility
	taskHandler        *handler.TaskHandler
	workerHandler      *handler.WorkerHandler
	endpointHandler    *handler.EndpointHandler
	autoscalerHandler  *handler.AutoScalerHandler
	statisticsHandler  *handler.StatisticsHandler
	specHandler        *handler.SpecHandler
	imageHandler       *handler.ImageHandler
	monitoringHandler  *handler.MonitoringHandler
	statusEventHandler *handler.StatusEventHandler
}

// NewRouter creates a new Router
func NewRouter(
	taskHandler *handler.TaskHandler,
	workerHandler *handler.WorkerHandler,
	endpointHandler *handler.EndpointHandler,
	autoscalerHandler *handler.AutoScalerHandler,
	statisticsHandler *handler.StatisticsHandler,
	specHandler *handler.SpecHandler,
	imageHandler *handler.ImageHandler,
	monitoringHandler *handler.MonitoringHandler,
	statusEventHandler *handler.StatusEventHandler,
) *Router {
	handlers := &Handlers{
		Task:        taskHandler,
		Worker:      workerHandler,
		Endpoint:    endpointHandler,
		Autoscaler:  autoscalerHandler,
		Statistics:  statisticsHandler,
		Spec:        specHandler,
		Image:       imageHandler,
		Monitoring:  monitoringHandler,
		StatusEvent: statusEventHandler,
	}

	return &Router{
		handlers:           handlers,
		taskHandler:        taskHandler,
		workerHandler:      workerHandler,
		endpointHandler:    endpointHandler,
		autoscalerHandler:  autoscalerHandler,
		statisticsHandler:  statisticsHandler,
		specHandler:        specHandler,
		imageHandler:       imageHandler,
		monitoringHandler:  monitoringHandler,
		statusEventHandler: statusEventHandler,
	}
}

// Setup configures all routes
func (r *Router) Setup(engine *gin.Engine) {
	// Global middleware
	engine.Use(middleware.Recovery())
	engine.Use(middleware.Logger())

	// Register route groups
	r.setupV1Routes(engine)     // Client API (/v1/*)
	r.setupV2Routes(engine)     // Worker API (/v2/*)
	r.setupAdminRoutes(engine)  // Admin API (/api/v1/*)
	r.setupHealthRoutes(engine) // Health check
}
