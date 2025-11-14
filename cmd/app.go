package main

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"waverless/app/handler"
	"waverless/internal/jobs"
	"waverless/internal/service"
	endpointsvc "waverless/internal/service/endpoint"
	"waverless/pkg/autoscaler"
	"waverless/pkg/config"
	"waverless/pkg/interfaces"
	"waverless/pkg/logger"
	"waverless/pkg/queue/asynq"
	mysqlstore "waverless/pkg/store/mysql"
	redisstore "waverless/pkg/store/redis"

	"github.com/gin-gonic/gin"
)

// Application manages the lifecycle of the entire application
type Application struct {
	// Infrastructure components
	config      *config.Config
	mysqlRepo   *mysqlstore.Repository
	redisClient *redisstore.RedisClient
	queueMgr    *asynq.Manager

	// Business providers
	deploymentProvider interfaces.DeploymentProvider

	// Service layer
	endpointService   *endpointsvc.Service
	taskService       *service.TaskService
	workerService     *service.WorkerService
	statisticsService *service.StatisticsService
	gpuUsageService   *service.GPUUsageService

	// Handler layer
	taskHandler       *handler.TaskHandler
	workerHandler     *handler.WorkerHandler
	endpointHandler   *handler.EndpointHandler
	autoscalerHandler *handler.AutoScalerHandler
	statisticsHandler *handler.StatisticsHandler
	gpuUsageHandler   *handler.GPUUsageHandler

	// Auto-scaler
	autoscalerMgr *autoscaler.Manager

	// HTTP server
	httpServer *http.Server
	ginEngine  *gin.Engine

	// Background tasks
	jobsManager *jobs.Manager

	// Context management
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Background task cleanup functions
	cleanupFuncs []func()
}

// NewApplication creates a new Application instance
func NewApplication() *Application {
	ctx, cancel := context.WithCancel(context.Background())
	return &Application{
		ctx:          ctx,
		cancel:       cancel,
		cleanupFuncs: make([]func(), 0),
	}
}

// Initialize initializes all application components
func (app *Application) Initialize() error {
	var err error

	// Initialize components in order
	steps := []struct {
		name string
		fn   func() error
	}{
		{"Configuration", app.initConfig},
		{"Logging", app.initLogger},
		{"MySQL", app.initMySQL},
		{"Redis", app.initRedis},
		{"Business Providers", app.initProviders},
		{"Queue Manager", app.initQueue},
		{"Service Layer", app.initServices},
		{"Background Tasks", app.initJobs},
		{"Handler Layer", app.initHandlers},
		{"Auto-scaler", app.initAutoScaler},
		{"HTTP Server", app.initHTTPServer},
	}

	for _, step := range steps {
		logger.InfoCtx(app.ctx, "Initializing %s...", step.name)
		if err = step.fn(); err != nil {
			return fmt.Errorf("failed to initialize %s: %w", step.name, err)
		}
		logger.InfoCtx(app.ctx, "%s initialized successfully", step.name)
	}

	logger.InfoCtx(app.ctx, "Application initialization completed")
	return nil
}

// Start starts all application components
func (app *Application) Start() error {
	logger.InfoCtx(app.ctx, "Starting application components...")

	// 1. Start background tasks
	if app.jobsManager != nil {
		logger.InfoCtx(app.ctx, "Starting background task manager")
		app.jobsManager.Start()
		app.wg.Add(1)
		go func() {
			defer app.wg.Done()
			app.jobsManager.Wait()
		}()
	}

	// 2. Start AutoScaler
	if app.autoscalerMgr != nil {
		if err := app.autoscalerMgr.Start(app.ctx); err != nil {
			logger.ErrorCtx(app.ctx, "Failed to start autoscaler: %v", err)
		} else {
			logger.InfoCtx(app.ctx, "Autoscaler started successfully")
		}
	}

	// 3. Start HTTP server
	app.wg.Add(1)
	go func() {
		defer app.wg.Done()
		addr := fmt.Sprintf(":%d", app.config.Server.Port)
		logger.InfoCtx(app.ctx, "HTTP server listening on: %s", addr)
		if err := app.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.FatalCtx(app.ctx, "HTTP server error: %v", err)
		}
	}()

	logger.InfoCtx(app.ctx, "All components started successfully")
	return nil
}

// Shutdown gracefully shuts down the application
func (app *Application) Shutdown(timeout time.Duration) error {
	logger.InfoCtx(app.ctx, "Starting graceful shutdown (timeout: %v)...", timeout)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// 1. Cancel all background tasks
	logger.InfoCtx(app.ctx, "Canceling background tasks...")
	app.cancel()
	if app.jobsManager != nil {
		app.jobsManager.Stop()
	}

	// 2. Stop HTTP server (stop accepting new requests)
	logger.InfoCtx(app.ctx, "Shutting down HTTP server...")
	if err := app.httpServer.Shutdown(shutdownCtx); err != nil {
		logger.ErrorCtx(app.ctx, "HTTP server shutdown error: %v", err)
	}

	// 3. Wait for all background tasks to complete
	logger.InfoCtx(app.ctx, "Waiting for background tasks to complete...")
	done := make(chan struct{})
	go func() {
		app.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		logger.InfoCtx(app.ctx, "All background tasks completed")
	case <-shutdownCtx.Done():
		logger.WarnCtx(app.ctx, "Shutdown timeout, some tasks may not have completed")
	}

	// 4. Stop AutoScaler
	if app.autoscalerMgr != nil {
		logger.InfoCtx(app.ctx, "Stopping autoscaler...")
		// autoscalerMgr.Stop() triggered via context cancel
	}

	// 5. Execute all cleanup functions (in reverse registration order)
	logger.InfoCtx(app.ctx, "Executing cleanup functions...")
	for i := len(app.cleanupFuncs) - 1; i >= 0; i-- {
		app.cleanupFuncs[i]()
	}

	// 6. Sync logs
	logger.Sync()

	logger.InfoCtx(app.ctx, "Graceful shutdown completed")
	return nil
}

// registerCleanup registers cleanup function
func (app *Application) registerCleanup(cleanup func()) {
	app.cleanupFuncs = append(app.cleanupFuncs, cleanup)
}
