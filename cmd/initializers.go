package main

import (
	"fmt"
	"net/http"
	"time"

	"waverless/app/handler"
	"waverless/app/router"
	"waverless/internal/model"
	"waverless/internal/service"
	endpointsvc "waverless/internal/service/endpoint"
	"waverless/internal/worker"
	"waverless/pkg/autoscaler"
	"waverless/pkg/config"
	"waverless/pkg/deploy/k8s"
	"waverless/pkg/logger"
	"waverless/pkg/provider"
	"waverless/pkg/queue/asynq"
	mysqlstore "waverless/pkg/store/mysql"
	redisstore "waverless/pkg/store/redis"

	"github.com/gin-gonic/gin"
)

// initConfig initializes configuration
func (app *Application) initConfig() error {
	if err := config.Init(); err != nil {
		return err
	}
	app.config = config.GlobalConfig
	return nil
}

// initLogger initializes logging
func (app *Application) initLogger() error {
	if err := logger.Init(); err != nil {
		return err
	}
	app.registerCleanup(func() {
		logger.Sync()
		logger.InfoCtx(app.ctx, "Logging system has been closed")
	})
	return nil
}

// initMySQL initializes MySQL
func (app *Application) initMySQL() error {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		app.config.MySQL.User,
		app.config.MySQL.Password,
		app.config.MySQL.Host,
		app.config.MySQL.Port,
		app.config.MySQL.Database,
	)

	repo, err := mysqlstore.NewRepository(dsn)
	if err != nil {
		return err
	}

	app.mysqlRepo = repo
	app.registerCleanup(func() {
		repo.Close()
		logger.InfoCtx(app.ctx, "MySQL connection has been closed")
	})

	return nil
}

// initRedis initializes Redis
func (app *Application) initRedis() error {
	client, err := redisstore.NewRedisClient(app.config)
	if err != nil {
		return err
	}

	app.redisClient = client
	app.registerCleanup(func() {
		client.Close()
		logger.InfoCtx(app.ctx, "Redis connection has been closed")
	})

	return nil
}

// initProviders initializes business providers
func (app *Application) initProviders() error {
	// Initialize Provider Factory
	factory := provider.NewProviderFactory(app.config)

	// Create business providers (Deployment, Queue)
	providers, err := factory.CreateBusinessProviders()
	if err != nil {
		return fmt.Errorf("failed to create business providers: %w", err)
	}

	app.deploymentProvider = providers.Deployment

	// Register cleanup for K8s provider
	if k8sProv, ok := providers.Deployment.(*k8s.K8sDeploymentProvider); ok {
		app.registerCleanup(func() {
			k8sProv.Close()
			logger.InfoCtx(app.ctx, "K8s deployment provider has been closed")
		})
	}

	return nil
}

// initQueue initializes queue manager
func (app *Application) initQueue() error {
	queueMgr, err := asynq.NewManager(app.config)
	if err != nil {
		return err
	}

	app.queueMgr = queueMgr
	app.registerCleanup(func() {
		queueMgr.Close()
		logger.InfoCtx(app.ctx, "Queue manager has been closed")
	})

	return nil
}

// initServices initializes service layer
func (app *Application) initServices() error {
	// Worker repository from Redis
	workerRepo := redisstore.NewWorkerRepository(app.redisClient)

	// Get K8s deployment provider for draining check
	var k8sDeployProvider *k8s.K8sDeploymentProvider
	if app.config.K8s.Enabled {
		if k8sProv, ok := app.deploymentProvider.(*k8s.K8sDeploymentProvider); ok {
			k8sDeployProvider = k8sProv
		}
	}

	// Initialize endpoint service
	app.endpointService = endpointsvc.NewService(
		app.mysqlRepo.Endpoint,
		app.mysqlRepo.AutoscalerConfig,
		app.mysqlRepo.Task,
		workerRepo,
		app.deploymentProvider,
	)

	// Initialize task service
	app.taskService = service.NewTaskService(
		app.mysqlRepo.Task,
		app.mysqlRepo.TaskEvent,
		workerRepo,
		app.queueMgr,
		app.endpointService,
		app.mysqlRepo.GPUUsage,
		app.deploymentProvider,
	)

	// Initialize worker service
	app.workerService = service.NewWorkerService(
		workerRepo,
		app.mysqlRepo.Task,
		k8sDeployProvider,
	)

	// Set task service on worker service (for event recording)
	app.workerService.SetTaskService(app.taskService)

	// Initialize statistics service
	app.statisticsService = service.NewStatisticsService(app.mysqlRepo.TaskStatistics)

	// Set statistics service on task service (for incremental statistics updates)
	app.taskService.SetStatisticsService(app.statisticsService)

	// Initialize GPU usage service
	app.gpuUsageService = service.NewGPUUsageService(app.mysqlRepo.GPUUsage)

	// Setup Pod watcher for graceful shutdown (when K8s is enabled)
	if err := app.setupPodWatcher(k8sDeployProvider); err != nil {
		logger.WarnCtx(app.ctx, "Failed to setup pod watcher: %v (non-critical, continuing)", err)
		// Non-critical feature, continue startup
	}

	// Setup Deployment watcher for optimized rolling updates (when K8s is enabled)
	if err := app.setupDeploymentWatcher(k8sDeployProvider); err != nil {
		logger.WarnCtx(app.ctx, "Failed to setup deployment watcher: %v (non-critical, continuing)", err)
		// Non-critical feature, continue startup
	}

	// Start pod cleanup job for stuck terminating pods (when K8s is enabled)
	if err := app.startPodCleanupJob(k8sDeployProvider); err != nil {
		logger.WarnCtx(app.ctx, "Failed to start pod cleanup job: %v (non-critical, continuing)", err)
		// Non-critical feature, continue startup
	}

	return nil
}

// setupPodWatcher sets up Pod deletion listener for graceful shutdown
// When a Pod is marked for deletion by K8s, automatically mark the corresponding Worker as draining and stop accepting new tasks
func (app *Application) setupPodWatcher(k8sProvider *k8s.K8sDeploymentProvider) error {
	if k8sProvider == nil {
		logger.InfoCtx(app.ctx, "K8s provider not available, skipping pod watcher setup")
		return nil
	}

	logger.InfoCtx(app.ctx, "Setting up pod watcher for graceful shutdown...")

	// Register Pod terminating callback
	err := k8sProvider.WatchPodTerminating(app.ctx, func(podName, endpoint string) {
		logger.InfoCtx(app.ctx, "🔔 Pod %s (endpoint: %s) marked for deletion, draining worker...",
			podName, endpoint)

		// 1. Find Worker by PodName
		worker, err := app.workerService.GetWorkerByPodName(app.ctx, endpoint, podName)
		if err != nil {
			logger.WarnCtx(app.ctx, "Pod %s terminating but worker not found: %v", podName, err)
			// Even if worker not found, still mark Pod as draining for observability
			if markErr := k8sProvider.MarkPodDraining(app.ctx, podName); markErr != nil {
				logger.WarnCtx(app.ctx, "Failed to mark pod %s as draining: %v", podName, markErr)
			}
			return
		}

		// 2. Mark Worker as DRAINING (business logic layer: prevent worker from pulling new tasks)
		err = app.workerService.UpdateWorkerStatus(app.ctx, worker.ID, model.WorkerStatusDraining)
		if err != nil {
			logger.ErrorCtx(app.ctx, "Failed to mark worker %s as draining: %v", worker.ID, err)
			return
		}

		logger.InfoCtx(app.ctx, "✅ Worker %s (Pod: %s) marked as DRAINING, will not receive new tasks",
			worker.ID, podName)

		// 3. Mark Pod as DRAINING in K8s (observability: convenient for kubectl to view pod status)
		// Note: No need to set deletion cost here as Pod is already marked for deletion by K8s
		// deletion cost is only used during proactive scale-down to tell K8s which Pod to delete first
		if err := k8sProvider.MarkPodDraining(app.ctx, podName); err != nil {
			logger.WarnCtx(app.ctx, "Failed to mark pod %s as draining in K8s: %v", podName, err)
		} else {
			logger.InfoCtx(app.ctx, "✅ Pod %s marked as draining in K8s (status=draining label)", podName)
		}
	})

	if err != nil {
		return fmt.Errorf("failed to setup pod watcher: %w", err)
	}

	logger.InfoCtx(app.ctx, "✅ Pod watcher registered successfully")
	return nil
}

// setupDeploymentWatcher sets up Deployment change listener (optimizes rolling updates)
func (app *Application) setupDeploymentWatcher(k8sProvider *k8s.K8sDeploymentProvider) error {
	if k8sProvider == nil {
		logger.InfoCtx(app.ctx, "K8s provider not available, skipping deployment watcher setup")
		return nil
	}

	logger.InfoCtx(app.ctx, "Setting up deployment watcher for optimized rolling updates...")

	// Register Deployment spec change callback
	err := k8sProvider.WatchDeploymentSpecChange(app.ctx, func(endpoint string) {
		logger.InfoCtx(app.ctx, "🔄 Deployment spec changed for endpoint %s, optimizing pod replacement...", endpoint)

		// Step 1: Get all workers for this endpoint
		workers, err := app.workerService.ListWorkers(app.ctx, endpoint)
		if err != nil {
			logger.ErrorCtx(app.ctx, "Failed to get workers for endpoint %s: %v", endpoint, err)
			return
		}

		if len(workers) == 0 {
			logger.InfoCtx(app.ctx, "No workers found for endpoint %s, nothing to optimize", endpoint)
			return
		}

		logger.InfoCtx(app.ctx, "Found %d workers for endpoint %s, marking as draining and prioritizing deletion...",
			len(workers), endpoint)

		// Step 2 & 3: Mark all workers as DRAINING and prioritize deletion based on workload
		idleCount := 0
		busyCount := 0

		for _, worker := range workers {
			// 2.1: Mark worker as DRAINING (stops accepting new tasks)
			err := app.workerService.UpdateWorkerStatus(app.ctx, worker.ID, model.WorkerStatusDraining)
			if err != nil {
				logger.WarnCtx(app.ctx, "Failed to mark worker %s as draining: %v", worker.ID, err)
				continue
			}

			// 2.2: Set Pod Deletion Cost based on workload
			podName := worker.ID // worker.ID == podName (from RUNPOD_POD_ID)

			if worker.CurrentJobs == 0 {
				// Idle worker: Set deletion cost to -1000 (highest priority for deletion)
				if err := k8sProvider.SetPodDeletionCost(app.ctx, podName, -1000); err != nil {
					logger.WarnCtx(app.ctx, "Failed to set deletion cost for idle worker %s: %v", podName, err)
				} else {
					logger.InfoCtx(app.ctx, "✅ Idle worker %s: deletion-cost = -1000 (will be deleted first)", podName)
					idleCount++
				}
			} else {
				// Busy worker: Set deletion cost to 1000 (lowest priority for deletion)
				if err := k8sProvider.SetPodDeletionCost(app.ctx, podName, 1000); err != nil {
					logger.WarnCtx(app.ctx, "Failed to set deletion cost for busy worker %s: %v", podName, err)
				} else {
					logger.InfoCtx(app.ctx, "⏳ Busy worker %s (jobs=%d): deletion-cost = 1000 (will be deleted last)",
						podName, worker.CurrentJobs)
					busyCount++
				}
			}

			// 2.3: Mark Pod as draining in K8s (for observability)
			if err := k8sProvider.MarkPodDraining(app.ctx, podName); err != nil {
				logger.WarnCtx(app.ctx, "Failed to mark pod %s as draining: %v", podName, err)
			}
		}

		logger.InfoCtx(app.ctx, "✅ Optimized pod replacement for endpoint %s: %d idle workers (priority deletion), %d busy workers (delayed deletion)",
			endpoint, idleCount, busyCount)
	})

	if err != nil {
		return fmt.Errorf("failed to register deployment watcher: %w", err)
	}

	logger.InfoCtx(app.ctx, "✅ Deployment watcher registered successfully")
	return nil
}

// initHandlers initializes handler layer
func (app *Application) initHandlers() error{
	// Initialize handlers
	app.taskHandler = handler.NewTaskHandler(app.taskService, app.workerService)
	app.workerHandler = handler.NewWorkerHandler(app.workerService, app.taskService, app.deploymentProvider)
	app.statisticsHandler = handler.NewStatisticsHandler(app.statisticsService)
	app.gpuUsageHandler = handler.NewGPUUsageHandler(
		app.gpuUsageService,
		app.mysqlRepo.Task,
		app.mysqlRepo.Endpoint,
		app.deploymentProvider,
	)

	// Initialize K8s Handler (Endpoint Handler)
	if app.config.K8s.Enabled {
		if app.deploymentProvider == nil {
			logger.ErrorCtx(app.ctx, "K8s is enabled but deployment provider is nil")
		} else {
			app.endpointHandler = handler.NewEndpointHandler(app.deploymentProvider, app.endpointService, app.workerService)
		}
	}

	return nil
}

// initAutoScaler initializes auto-scaler
func (app *Application) initAutoScaler() error {
	if !app.config.K8s.Enabled {
		logger.InfoCtx(app.ctx, "K8s not enabled, skipping autoscaler initialization")
		return nil
	}

	if !app.config.AutoScaler.Enabled {
		logger.InfoCtx(app.ctx, "AutoScaler not enabled")
		return nil
	}

	// Get spec manager from K8s deployment provider
	var specManager *k8s.SpecManager
	if k8sProvider, ok := app.deploymentProvider.(*k8s.K8sDeploymentProvider); ok {
		specManager = k8sProvider.GetSpecManager()
	} else {
		logger.WarnCtx(app.ctx, "AutoScaler requires K8s deployment provider, skipping initialization")
		return nil
	}

	autoscalerConfig := &autoscaler.Config{
		Enabled:        app.config.AutoScaler.Enabled,
		Interval:       app.config.AutoScaler.Interval,
		MaxGPUCount:    app.config.AutoScaler.MaxGPUCount,
		MaxCPUCores:    app.config.AutoScaler.MaxCPUCores,
		MaxMemoryGB:    app.config.AutoScaler.MaxMemoryGB,
		StarvationTime: app.config.AutoScaler.StarvationTime,
	}

	app.autoscalerMgr = autoscaler.NewManager(
		autoscalerConfig,
		app.deploymentProvider,
		app.endpointService,
		redisstore.NewWorkerRepository(app.redisClient),
		app.mysqlRepo.Task,
		app.mysqlRepo.ScalingEvent,
		app.redisClient.GetClient(),
		specManager,
	)

	app.autoscalerHandler = handler.NewAutoScalerHandler(app.autoscalerMgr, app.endpointService)

	return nil
}

// initHTTPServer initializes HTTP server
func (app *Application) initHTTPServer() error{
	// Initialize router
	r := router.NewRouter(app.taskHandler, app.workerHandler, app.endpointHandler, app.autoscalerHandler, app.statisticsHandler, app.gpuUsageHandler)

	// Set Gin mode
	gin.SetMode(app.config.Server.Mode)

	// Create Gin engine
	app.ginEngine = gin.New()
	app.ginEngine.Use(gin.Recovery())

	// Setup routes
	r.Setup(app.ginEngine)

	// Create HTTP server
	app.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", app.config.Server.Port),
		Handler: app.ginEngine,
	}

	// Start legacy scaler (if needed)
	// NOTE: This is the legacy scaler, may need to be removed or integrated in future refactoring
	scalerConfig := worker.ScalerConfig{
		MinWorkers:         1,
		MaxWorkers:         10,
		ScaleUpThreshold:   5.0,
		ScaleDownThreshold: 1.0,
		CheckInterval:      30 * time.Second,
		CooldownPeriod:     5 * time.Minute,
	}

	scaler := worker.NewScaler(scalerConfig, app.taskService, app.workerService)
	app.wg.Add(1)
	go func() {
		defer app.wg.Done()
		scaler.Start(app.ctx)
	}()

	return nil
}
