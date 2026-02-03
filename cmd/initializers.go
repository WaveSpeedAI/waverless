package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"waverless/app/handler"
	"waverless/app/router"
	"waverless/internal/model"
	"waverless/internal/service"
	endpointsvc "waverless/internal/service/endpoint"
	"waverless/pkg/autoscaler"
	"waverless/pkg/capacity"
	"waverless/pkg/config"
	"waverless/pkg/deploy/k8s"
	"waverless/pkg/deploy/novita"
	"waverless/pkg/interfaces"
	"waverless/pkg/logger"
	"waverless/pkg/monitoring"
	"waverless/pkg/provider"
	"waverless/pkg/resource"
	mysqlstore "waverless/pkg/store/mysql"
	redisstore "waverless/pkg/store/redis"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	appsv1 "k8s.io/api/apps/v1"

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
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=UTC",
		app.config.MySQL.User,
		app.config.MySQL.Password,
		app.config.MySQL.Host,
		app.config.MySQL.Port,
		app.config.MySQL.Database,
	)

	repo, err := mysqlstore.NewRepository(dsn, app.config.MySQL.Proxy)
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

// initServices initializes service layer
func (app *Application) initServices() error {

	// Initialize worker service (MySQL-based)
	app.workerService = service.NewWorkerService(
		app.mysqlRepo.Worker,
		app.mysqlRepo.Task,
		app.deploymentProvider,
	)

	// Initialize worker event service for monitoring
	app.workerEventService = service.NewWorkerEventService(app.mysqlRepo.Monitoring)
	app.workerService.SetWorkerEventService(app.workerEventService)

	// Initialize endpoint service
	app.endpointService = endpointsvc.NewService(
		app.mysqlRepo.Endpoint,
		app.mysqlRepo.AutoscalerConfig,
		app.mysqlRepo.Task,
		app.workerService,
		app.deploymentProvider,
	)

	// Initialize task service
	app.taskService = service.NewTaskService(
		app.mysqlRepo.Task,
		app.mysqlRepo.TaskEvent,
		app.endpointService,
		app.deploymentProvider,
	)

	// Set task service on worker service (for event recording)
	app.workerService.SetTaskService(app.taskService)

	// Set worker service on task service (for worker stats recording)
	app.taskService.SetWorkerService(app.workerService)

	// Initialize statistics service
	app.statisticsService = service.NewStatisticsService(app.mysqlRepo.TaskStatistics, app.mysqlRepo.Worker)

	// Set statistics service on task service (for incremental statistics updates)
	app.taskService.SetStatisticsService(app.statisticsService)

	// Initialize spec service
	app.specService = service.NewSpecService(app.mysqlRepo.Spec)

	// Initialize monitoring service
	app.monitoringService = service.NewMonitoringService(app.mysqlRepo.Monitoring)

	// Initialize monitoring collector
	app.monitoringCollector = monitoring.NewCollector(app.mysqlRepo.Monitoring, app.mysqlRepo.Worker, app.mysqlRepo.Task)

	// Get K8s deployment provider for draining check
	var k8sDeployProvider *k8s.K8sDeploymentProvider
	if app.config.K8s.Enabled {
		if k8sProv, ok := app.deploymentProvider.(*k8s.K8sDeploymentProvider); ok {
			k8sDeployProvider = k8sProv
		}
	}

	// Get Novita deployment provider for status sync
	var novitaDeployProvider *novita.NovitaDeploymentProvider
	if app.config.Novita.Enabled {
		if novitaProv, ok := app.deploymentProvider.(*novita.NovitaDeploymentProvider); ok {
			novitaDeployProvider = novitaProv
			// Inject spec service for database access
			if app.specService != nil {
				novitaDeployProvider.SetSpecRepository(app.specService)
				logger.InfoCtx(app.ctx, "Spec service injected into Novita provider - specs will be read from database first")
			}
			// Inject Redis client for draining workers tracking (multi-replica safe)
			if app.redisClient != nil {
				novitaDeployProvider.SetRedisClient(app.redisClient.GetClient())
				logger.InfoCtx(app.ctx, "Redis client injected into Novita provider - draining workers will be tracked in Redis")
			}
		}
	}

	// Setup Pod watcher for graceful shutdown (when K8s is enabled)
	if err := app.setupPodWatcher(k8sDeployProvider); err != nil {
		logger.WarnCtx(app.ctx, "Failed to setup pod watcher: %v (non-critical, continuing)", err)
		// Non-critical feature, continue startup
	}

	// Setup Spot interruption watcher (when K8s is enabled)
	if err := app.setupSpotInterruptionWatcher(k8sDeployProvider); err != nil {
		logger.WarnCtx(app.ctx, "Failed to setup spot interruption watcher: %v (non-critical, continuing)", err)
		// Non-critical feature, continue startup
	}

	// Setup Deployment watcher for optimized rolling updates (when K8s is enabled)
	if err := app.setupDeploymentWatcher(k8sDeployProvider); err != nil {
		logger.WarnCtx(app.ctx, "Failed to setup deployment watcher: %v (non-critical, continuing)", err)
		// Non-critical feature, continue startup
	}

	// Setup Pod status watcher for worker runtime state sync AND failure detection (when K8s is enabled)
	// This combines worker state sync and failure monitoring into a single watcher to avoid duplicate callbacks
	// Validates: Requirements 3.1, 3.2, 3.3, 3.4
	if err := app.setupPodStatusWatcher(k8sDeployProvider); err != nil {
		logger.WarnCtx(app.ctx, "Failed to setup pod status watcher: %v (non-critical, continuing)", err)
	}

	// Start pod cleanup job for stuck terminating pods (when K8s is enabled)
	if err := app.startPodCleanupJob(k8sDeployProvider); err != nil {
		logger.WarnCtx(app.ctx, "Failed to start pod cleanup job: %v (non-critical, continuing)", err)
		// Non-critical feature, continue startup
	}

	// Setup capacity manager (when K8s is enabled)
	if err := app.setupCapacityManager(k8sDeployProvider); err != nil {
		logger.WarnCtx(app.ctx, "Failed to setup capacity manager: %v (non-critical, continuing)", err)
	}

	// Setup Novita status watcher for endpoint status sync (when Novita is enabled)
	if err := app.setupNovitaStatusWatcher(novitaDeployProvider); err != nil {
		logger.WarnCtx(app.ctx, "Failed to setup Novita status watcher: %v (non-critical, continuing)", err)
		// Non-critical feature, continue startup
	}

	// Setup Novita pod status watcher for worker runtime state sync (when Novita is enabled)
	if err := app.setupNovitaPodStatusWatcher(novitaDeployProvider); err != nil {
		logger.WarnCtx(app.ctx, "Failed to setup Novita pod status watcher: %v (non-critical, continuing)", err)
		// Non-critical feature, continue startup
	}

	// Setup Novita Worker status monitor for failure detection and tracking (when Novita is enabled)
	// This monitors worker status changes and updates worker failure information in the database
	// Validates: Requirements 3.1, 3.2, 3.3, 3.4
	if err := app.setupNovitaWorkerStatusMonitor(novitaDeployProvider); err != nil {
		logger.WarnCtx(app.ctx, "Failed to setup Novita worker status monitor: %v (non-critical, continuing)", err)
	}

	// Setup Resource Releaser for automatic cleanup of failed workers
	// This monitors workers with IMAGE_PULL_FAILED status and terminates them after timeout
	// Validates: Requirements 5.1, 5.2, 5.3, 5.4
	if err := app.setupResourceReleaser(); err != nil {
		logger.WarnCtx(app.ctx, "Failed to setup resource releaser: %v (non-critical, continuing)", err)
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

// setupSpotInterruptionWatcher sets up Spot interruption detection
func (app *Application) setupSpotInterruptionWatcher(k8sProvider *k8s.K8sDeploymentProvider) error {
	if k8sProvider == nil {
		logger.InfoCtx(app.ctx, "K8s provider not available, skipping spot interruption watcher setup")
		return nil
	}

	logger.InfoCtx(app.ctx, "Setting up spot interruption watcher...")

	err := k8sProvider.WatchSpotInterruption(app.ctx, func(podName, endpoint, reason string) {
		logger.WarnCtx(app.ctx, "🚨 SPOT INTERRUPTION detected for Pod %s (endpoint: %s), reason: %s",
			podName, endpoint, reason)

		// Find Worker by PodName
		worker, err := app.workerService.GetWorkerByPodName(app.ctx, endpoint, podName)
		if err != nil {
			logger.WarnCtx(app.ctx, "Spot interruption detected but worker not found for pod %s: %v", podName, err)
			return
		}

		// Immediately mark Worker as DRAINING (2-minute grace period starts now)
		err = app.workerService.UpdateWorkerStatus(app.ctx, worker.ID, model.WorkerStatusDraining)
		if err != nil {
			logger.ErrorCtx(app.ctx, "Failed to mark worker %s as draining after spot interruption: %v", worker.ID, err)
			return
		}

		logger.InfoCtx(app.ctx, "✅ Worker %s (Pod: %s) marked as DRAINING due to spot interruption - stopping new task acceptance",
			worker.ID, podName)

		// Log current jobs for monitoring
		if len(worker.JobsInProgress) > 0 {
			logger.InfoCtx(app.ctx, "📋 Worker %s has %d jobs in progress that will continue: %v",
				worker.ID, len(worker.JobsInProgress), worker.JobsInProgress)
		} else {
			logger.InfoCtx(app.ctx, "📋 Worker %s has no jobs in progress - can terminate safely", worker.ID)
		}

		// Mark Pod as draining for observability
		if err := k8sProvider.MarkPodDraining(app.ctx, podName); err != nil {
			logger.WarnCtx(app.ctx, "Failed to mark pod %s as draining: %v", podName, err)
		}
	})

	if err != nil {
		return fmt.Errorf("failed to setup spot interruption watcher: %w", err)
	}

	logger.InfoCtx(app.ctx, "✅ Spot interruption watcher setup complete")
	return nil
}

// setupNovitaStatusWatcher sets up Novita status watcher for endpoint status sync
func (app *Application) setupNovitaStatusWatcher(novitaProvider *novita.NovitaDeploymentProvider) error {
	if novitaProvider == nil {
		logger.InfoCtx(app.ctx, "Novita provider not available, skipping status watcher setup")
		return nil
	}

	logger.InfoCtx(app.ctx, "Setting up Novita status watcher for endpoint status sync...")

	// Register replica watch callback to sync status to database
	err := novitaProvider.WatchReplicas(app.ctx, func(event interfaces.ReplicaEvent) {
		endpoint := event.Name

		// Calculate status based on replica state
		status := "Pending"
		if event.AvailableReplicas == event.DesiredReplicas && event.DesiredReplicas > 0 {
			status = "Running"
		} else if event.DesiredReplicas == 0 {
			status = "Stopped"
		}

		// Update endpoint runtime state in database
		if app.mysqlRepo != nil && app.mysqlRepo.Endpoint != nil {
			runtimeState := map[string]interface{}{
				"replicas":          event.DesiredReplicas,
				"readyReplicas":     event.ReadyReplicas,
				"availableReplicas": event.AvailableReplicas,
			}

			if err := app.mysqlRepo.Endpoint.UpdateRuntimeState(app.ctx, endpoint, status, runtimeState); err != nil {
				logger.ErrorCtx(app.ctx, "Failed to update Novita endpoint runtime state: %v", err)
			}
		}
	})

	if err != nil {
		logger.WarnCtx(app.ctx, "Failed to register Novita status watcher: %v", err)
		return err
	}

	return nil
}

// setupNovitaPodStatusWatcher syncs Novita worker runtime state to worker table
func (app *Application) setupNovitaPodStatusWatcher(novitaProvider *novita.NovitaDeploymentProvider) error {
	if novitaProvider == nil {
		return nil
	}

	logger.InfoCtx(app.ctx, "Setting up Novita pod status watcher for worker runtime sync...")

	// Watch worker status changes
	err := novitaProvider.WatchPodStatusChange(app.ctx, func(workerID, endpoint string, info *interfaces.PodInfo) {
		// For Novita, workerID is the Novita Worker ID (used as podName)
		podName := workerID

		// Check if worker already exists in database
		existingWorker, _ := app.mysqlRepo.Worker.GetByPodName(app.ctx, endpoint, podName)
		isNewWorker := existingWorker == nil

		// Parse timestamps from PodInfo (Novita provider generates these locally)
		var createdAt, startedAt *time.Time
		if info.CreatedAt != "" {
			if t, err := time.Parse(time.RFC3339, info.CreatedAt); err == nil {
				createdAt = &t
			}
		}
		if info.StartedAt != "" {
			if t, err := time.Parse(time.RFC3339, info.StartedAt); err == nil {
				startedAt = &t
			}
		}
		logger.WarnCtx(app.ctx, "existingWorker: %v", existingWorker)
		// If worker already exists in database, preserve existing timestamps
		// This handles the case where Novita provider restarts and loses in-memory state
		// We should not overwrite billing timestamps that were already recorded
		if existingWorker != nil {
			if existingWorker.PodCreatedAt != nil {
				createdAt = nil // Don't update, database already has this
			}
			if existingWorker.PodStartedAt != nil {
				startedAt = nil // Don't update, database already has this
			}
		}

		// Create or update worker (status STARTING until heartbeat)
		// Novita doesn't provide IP/NodeName, but now we have timestamps for billing
		if err := app.mysqlRepo.Worker.UpsertFromPod(app.ctx, podName, endpoint, info.Phase, info.Status, info.Reason, info.Message, "", "", createdAt, startedAt); err != nil {
			logger.WarnCtx(app.ctx, "Failed to upsert worker from Novita worker %s: %v", workerID, err)
		}

		// Record WORKER_STARTED event for new workers only
		if isNewWorker && app.workerEventService != nil {
			app.workerEventService.RecordWorkerStarted(app.ctx, podName, endpoint)
		}
	})

	if err != nil {
		return fmt.Errorf("failed to setup Novita pod status watcher: %w", err)
	}

	// Watch worker deletions to mark workers as OFFLINE
	err = novitaProvider.WatchPodDelete(app.ctx, func(workerID, endpoint string) {
		podName := workerID
		// Record WORKER_OFFLINE event before marking offline
		if app.workerEventService != nil {
			app.workerEventService.RecordWorkerOffline(app.ctx, workerID, endpoint, podName)
		}
		if err := app.mysqlRepo.Worker.MarkOfflineByPodName(app.ctx, podName); err != nil {
			logger.WarnCtx(app.ctx, "Failed to mark worker offline for deleted Novita worker %s: %v", workerID, err)
		}
		// Clear draining state from Redis when worker is deleted/offline
		if err := novitaProvider.ClearDrainingWorker(app.ctx, workerID); err != nil {
			logger.WarnCtx(app.ctx, "Failed to clear draining state for worker %s: %v", workerID, err)
		}
	})
	if err != nil {
		logger.WarnCtx(app.ctx, "Failed to setup Novita pod delete watcher: %v", err)
	}

	logger.InfoCtx(app.ctx, "Novita pod status watcher registered successfully")
	return nil
}

// setupDeploymentWatcher sets up Deployment change listener (optimizes rolling updates)
// This watcher only sets Pod Deletion Cost to guide K8s on which pods to delete first
// It does NOT mark workers as DRAINING - that's handled by setupPodWatcher when pods are actually terminated
func (app *Application) setupDeploymentWatcher(k8sProvider *k8s.K8sDeploymentProvider) error {
	if k8sProvider == nil {
		logger.InfoCtx(app.ctx, "K8s provider not available, skipping deployment watcher setup")
		return nil
	}

	logger.InfoCtx(app.ctx, "Setting up deployment watcher for optimized rolling updates...")

	// Register Deployment spec change callback
	err := k8sProvider.WatchDeploymentSpecChange(app.ctx, func(endpoint string) {
		logger.InfoCtx(app.ctx, "🔄 Deployment spec changed for endpoint %s, setting pod deletion priorities...", endpoint)

		// Get all workers for this endpoint
		workers, err := app.workerService.ListWorkers(app.ctx, endpoint)
		if err != nil {
			logger.ErrorCtx(app.ctx, "Failed to get workers for endpoint %s: %v", endpoint, err)
			return
		}

		if len(workers) == 0 {
			logger.InfoCtx(app.ctx, "No workers found for endpoint %s, nothing to optimize", endpoint)
			return
		}

		logger.InfoCtx(app.ctx, "Found %d workers for endpoint %s, setting deletion priorities based on workload...",
			len(workers), endpoint)

		// Set Pod Deletion Cost based on workload
		// This guides K8s to delete idle pods first, busy pods last
		// Workers will be marked as DRAINING by setupPodWatcher when K8s actually deletes them
		idleCount := 0
		busyCount := 0

		for _, worker := range workers {
			podName := worker.ID // worker.ID == podName (from RUNPOD_POD_ID)

			if worker.CurrentJobs == 0 {
				// Idle worker: Set deletion cost to -1000 (highest priority for deletion)
				// K8s will prefer to delete these pods first during rolling update
				if err := k8sProvider.SetPodDeletionCost(app.ctx, podName, -1000); err != nil {
					logger.WarnCtx(app.ctx, "Failed to set deletion cost for idle worker %s: %v", podName, err)
				} else {
					logger.InfoCtx(app.ctx, "✅ Idle worker %s: deletion-cost = -1000 (will be deleted first by K8s)", podName)
					idleCount++
				}
			} else {
				// Busy worker: Set deletion cost to 1000 (lowest priority for deletion)
				// K8s will delete these pods last, giving them time to finish tasks
				if err := k8sProvider.SetPodDeletionCost(app.ctx, podName, 1000); err != nil {
					logger.WarnCtx(app.ctx, "Failed to set deletion cost for busy worker %s: %v", podName, err)
				} else {
					logger.InfoCtx(app.ctx, "⏳ Busy worker %s (jobs=%d): deletion-cost = 1000 (will be deleted last by K8s)",
						podName, worker.CurrentJobs)
					busyCount++
				}
			}
		}

		logger.InfoCtx(app.ctx, "✅ Pod deletion priorities set for endpoint %s: %d idle workers (delete first), %d busy workers (delete last)",
			endpoint, idleCount, busyCount)
		logger.InfoCtx(app.ctx, "ℹ️  Workers will be marked as DRAINING by PodWatcher when K8s actually deletes them (respects maxUnavailable)")
	})

	if err != nil {
		return fmt.Errorf("failed to register deployment watcher: %w", err)
	}

	// Register deployment status change callback to sync status to database
	err = k8sProvider.WatchDeploymentStatusChange(app.ctx, func(endpoint string, deployment *appsv1.Deployment) {
		// Calculate status
		status := "Pending"
		if deployment.Status.AvailableReplicas == *deployment.Spec.Replicas && *deployment.Spec.Replicas > 0 {
			status = "Running"
		} else if *deployment.Spec.Replicas == 0 {
			status = "Stopped"
		}

		logger.InfoCtx(app.ctx, "📊 Deployment status changed for %s: status=%s, replicas=%d/%d/%d",
			endpoint, status, deployment.Status.ReadyReplicas, deployment.Status.AvailableReplicas, *deployment.Spec.Replicas)

		// Update endpoint in database
		if app.mysqlRepo != nil && app.mysqlRepo.Endpoint != nil {
			runtimeState := map[string]interface{}{
				"namespace":         deployment.Namespace,
				"replicas":          *deployment.Spec.Replicas,
				"readyReplicas":     deployment.Status.ReadyReplicas,
				"availableReplicas": deployment.Status.AvailableReplicas,
			}
			// Extract shmSize and volumeMounts
			if info := k8s.DeploymentToAppInfo(deployment); info != nil {
				if info.ShmSize != "" {
					runtimeState["shmSize"] = info.ShmSize
				}
				if len(info.VolumeMounts) > 0 {
					runtimeState["volumeMounts"] = info.VolumeMounts
				}
			}
			if err := app.mysqlRepo.Endpoint.UpdateRuntimeState(app.ctx, endpoint, status, runtimeState); err != nil {
				logger.ErrorCtx(app.ctx, "Failed to update endpoint runtime state: %v", err)
			}
		}
	})

	if err != nil {
		logger.WarnCtx(app.ctx, "Failed to register deployment status watcher: %v", err)
	}

	logger.InfoCtx(app.ctx, "✅ Deployment watcher registered successfully")
	return nil
}

// initHandlers initializes handler layer
func (app *Application) initHandlers() error {
	// Initialize handlers
	app.taskHandler = handler.NewTaskHandler(app.taskService, app.workerService)
	app.workerHandler = handler.NewWorkerHandler(app.workerService, app.taskService, app.deploymentProvider)
	app.statisticsHandler = handler.NewStatisticsHandler(app.statisticsService, app.workerService)
	app.monitoringHandler = handler.NewMonitoringHandler(app.monitoringService)

	// Initialize Endpoint Handler (for K8s or Novita)
	if app.config.K8s.Enabled || app.config.Novita.Enabled {
		if app.deploymentProvider == nil {
			logger.ErrorCtx(app.ctx, "Deployment provider is enabled but provider is nil")
		} else {
			app.endpointHandler = handler.NewEndpointHandler(app.deploymentProvider, app.endpointService, app.workerService)
			if app.config.K8s.Enabled {
				logger.InfoCtx(app.ctx, "Endpoint handler initialized for K8s")
			}
			if app.config.Novita.Enabled {
				logger.InfoCtx(app.ctx, "Endpoint handler initialized for Novita")
			}
		}
	}

	// Initialize Spec Handler
	app.specHandler = handler.NewSpecHandler(app.specService)
	if app.capacityMgr != nil && app.mysqlRepo != nil {
		app.specHandler.SetCapacityManager(app.capacityMgr, app.mysqlRepo.SpecCapacity)
	}

	// Initialize Image Handler (for DockerHub webhook and image update checking)
	if app.endpointService != nil {
		app.imageHandler = handler.NewImageHandler(app.endpointService, &app.config.Docker)
		logger.InfoCtx(app.ctx, "Image handler initialized")
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
		// Inject spec service into spec manager for database access
		// SpecService implements SpecRepositoryInterface
		if app.specService != nil {
			specManager.SetSpecRepository(app.specService)
			logger.InfoCtx(app.ctx, "Spec service injected into SpecManager - specs will be read from database first")
		}
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
		app.workerService,
		app.mysqlRepo.Task,
		app.mysqlRepo.ScalingEvent,
		app.redisClient.GetClient(),
		specManager,
		app.mysqlRepo.Endpoint,
	)

	app.autoscalerHandler = handler.NewAutoScalerHandler(app.autoscalerMgr, app.endpointService)

	return nil
}

// setupPodStatusWatcher syncs Pod runtime state to worker table AND detects worker failures.
// This combines two responsibilities into a single watcher to avoid duplicate callbacks:
// 1. Worker state sync: Create/update worker records from pod status
// 2. Failure detection: Detect and record worker failures (ImagePullBackOff, CrashLoopBackOff, etc.)
//
// Validates: Requirements 3.1, 3.2, 3.3, 3.4
func (app *Application) setupPodStatusWatcher(k8sProvider *k8s.K8sDeploymentProvider) error {
	if k8sProvider == nil {
		return nil
	}

	logger.InfoCtx(app.ctx, "Setting up pod status watcher for worker runtime sync and failure detection...")

	// Create failure detector for identifying and sanitizing worker failures
	failureDetector := k8s.NewK8sWorkerStatusMonitor(k8sProvider.GetManager(), app.mysqlRepo.Worker)

	err := k8sProvider.WatchPodStatusChange(app.ctx, func(podName, endpoint string, info *interfaces.PodInfo) {
		var createdAt, startedAt *time.Time
		if info.CreatedAt != "" {
			if t, err := time.Parse(time.RFC3339, info.CreatedAt); err == nil {
				createdAt = &t
			}
		}
		if info.StartedAt != "" {
			if t, err := time.Parse(time.RFC3339, info.StartedAt); err == nil {
				startedAt = &t
			}
		}

		// Check if this is a new worker (for WORKER_STARTED event)
		existingWorker, _ := app.mysqlRepo.Worker.GetByPodName(app.ctx, endpoint, podName)
		isNewWorker := existingWorker == nil

		// 1. Create or update worker (status STARTING until heartbeat)
		if err := app.mysqlRepo.Worker.UpsertFromPod(app.ctx, podName, endpoint, info.Phase, info.Status, info.Reason, info.Message, info.IP, info.NodeName, createdAt, startedAt); err != nil {
			logger.WarnCtx(app.ctx, "Failed to upsert worker from pod %s: %v", podName, err)
		}

		// Record WORKER_STARTED event for new workers
		if isNewWorker && app.workerEventService != nil {
			app.workerEventService.RecordWorkerStarted(app.ctx, podName, endpoint)
		}

		// 2. Detect and record worker failures
		// This uses the failure detector to identify failure states and update the worker record
		if failureInfo := failureDetector.DetectFailure(info); failureInfo != nil {
			logger.WarnCtx(app.ctx, "🚨 Worker failure detected: pod=%s, endpoint=%s, type=%s, reason=%s",
				podName, endpoint, failureInfo.Type, failureInfo.SanitizedMsg)

			// Update worker failure information in database
			if err := failureDetector.UpdateWorkerFailure(app.ctx, podName, endpoint, failureInfo); err != nil {
				logger.ErrorCtx(app.ctx, "Failed to update worker failure: pod=%s, error=%v", podName, err)
			} else {
				logger.InfoCtx(app.ctx, "✅ Worker failure recorded in database: pod=%s, type=%s", podName, failureInfo.Type)
			}
		}
	})

	if err != nil {
		return fmt.Errorf("failed to setup pod status watcher: %w", err)
	}

	// Watch pod deletions to mark workers as OFFLINE
	err = k8sProvider.WatchPodDelete(app.ctx, func(podName, endpoint string) {
		// Record WORKER_OFFLINE event before marking offline
		if app.workerEventService != nil {
			app.workerEventService.RecordWorkerOffline(app.ctx, podName, endpoint, podName)
		}
		if err := app.mysqlRepo.Worker.MarkOfflineByPodName(app.ctx, podName); err != nil {
			logger.WarnCtx(app.ctx, "Failed to mark worker offline for deleted pod %s: %v", podName, err)
		}
	})
	if err != nil {
		logger.WarnCtx(app.ctx, "Failed to setup pod delete watcher: %v", err)
	}

	logger.InfoCtx(app.ctx, "✅ Pod status watcher registered successfully (with failure detection)")
	return nil
}

// setupNovitaWorkerStatusMonitor sets up the Novita worker status monitor for failure detection and tracking.
// This monitor uses polling to detect worker status changes and updates worker failure information in the database.
// When a worker enters a failed state (image pull failure, container crash, resource limit, etc.),
// it records the failure type, reason, and sanitized message for user-friendly display.
//
// Validates: Requirements 3.1, 3.2, 3.3, 3.4
func (app *Application) setupNovitaWorkerStatusMonitor(novitaProvider *novita.NovitaDeploymentProvider) error {
	if novitaProvider == nil {
		logger.InfoCtx(app.ctx, "Novita provider not available, skipping worker status monitor setup")
		return nil
	}

	logger.InfoCtx(app.ctx, "Setting up Novita worker status monitor for failure detection...")

	// Get the underlying client from the provider
	// We need access to the client to create the status monitor
	client := novitaProvider.GetClient()
	if client == nil {
		return fmt.Errorf("novita client not available")
	}

	// Create the worker status monitor
	statusMonitor := novita.NewNovitaWorkerStatusMonitor(client, app.mysqlRepo.Worker)

	// Start the status monitor in a goroutine
	// It will poll for worker status changes and update worker failure information
	go func() {
		logger.InfoCtx(app.ctx, "Starting Novita worker status monitor...")

		// The callback is invoked when a worker enters a failed state
		// The monitor already updates the database internally, but we can add additional logging here
		err := statusMonitor.WatchWorkerStatus(app.ctx, func(workerID, endpoint string, info *interfaces.WorkerFailureInfo) {
			logger.WarnCtx(app.ctx, "🚨 Novita worker failure detected and recorded: worker=%s, endpoint=%s, type=%s, reason=%s",
				workerID, endpoint, info.Type, info.SanitizedMsg)

			// Additional actions can be added here in the future:
			// - Send alerts/notifications
			// - Update endpoint health status
			// - Trigger resource cleanup
		})

		if err != nil && err != context.Canceled {
			logger.ErrorCtx(app.ctx, "Novita worker status monitor stopped with error: %v", err)
		} else {
			logger.InfoCtx(app.ctx, "Novita worker status monitor stopped gracefully")
		}
	}()

	logger.InfoCtx(app.ctx, "✅ Novita worker status monitor setup completed")
	return nil
}

// setupResourceReleaser sets up the resource releaser for automatic cleanup of failed workers.
// This component monitors workers with IMAGE_PULL_FAILED status and terminates them after the configured timeout.
// It prevents GPU resources from being wasted on workers that cannot start due to image issues.
//
// The releaser performs the following actions:
// 1. Periodically checks for workers with IMAGE_PULL_FAILED failure type
// 2. If a worker has been in failed state longer than ImagePullTimeout (default: 5 minutes), terminates it
// 3. Updates the endpoint health status based on the ratio of failed workers
//
// Validates: Requirements 5.1, 5.2, 5.3, 5.4
func (app *Application) setupResourceReleaser() error {
	if app.deploymentProvider == nil {
		logger.InfoCtx(app.ctx, "Deployment provider not available, skipping resource releaser setup")
		return nil
	}

	logger.InfoCtx(app.ctx, "Setting up resource releaser for automatic cleanup of failed workers...")

	// Get configuration from config file or use defaults
	releaserConfig := resource.DefaultResourceReleaserConfig()

	// Override with config values if available
	if app.config.ResourceReleaser.ImagePullTimeout > 0 {
		releaserConfig.ImagePullTimeout = app.config.ResourceReleaser.ImagePullTimeout
	}
	if app.config.ResourceReleaser.CheckInterval > 0 {
		releaserConfig.CheckInterval = app.config.ResourceReleaser.CheckInterval
	}
	if app.config.ResourceReleaser.MaxRetries > 0 {
		releaserConfig.MaxRetries = app.config.ResourceReleaser.MaxRetries
	}

	// Create the resource releaser
	releaser := resource.NewResourceReleaser(
		app.deploymentProvider,
		app.mysqlRepo.Worker,
		app.mysqlRepo.Endpoint,
		releaserConfig,
	)

	// Start the releaser in a goroutine
	go func() {
		logger.InfoCtx(app.ctx, "Starting resource releaser with config: imagePullTimeout=%v, checkInterval=%v, maxRetries=%d",
			releaserConfig.ImagePullTimeout, releaserConfig.CheckInterval, releaserConfig.MaxRetries)
		releaser.Start(app.ctx)
	}()

	logger.InfoCtx(app.ctx, "✅ Resource releaser setup completed")
	return nil
}

// initHTTPServer initializes HTTP server
func (app *Application) initHTTPServer() error {
	// Initialize router
	r := router.NewRouter(app.taskHandler, app.workerHandler, app.endpointHandler, app.autoscalerHandler, app.statisticsHandler, app.specHandler, app.imageHandler, app.monitoringHandler)

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

	return nil
}

// setupCapacityManager sets up capacity manager for spec availability tracking
func (app *Application) setupCapacityManager(k8sProvider *k8s.K8sDeploymentProvider) error {
	if k8sProvider == nil {
		logger.InfoCtx(app.ctx, "K8s provider not available, skipping capacity manager setup")
		return nil
	}

	logger.InfoCtx(app.ctx, "Setting up capacity manager for platform: %s", app.config.K8s.Platform)

	// Ensure spec manager has database access
	specMgr := k8sProvider.GetSpecManager()
	if specMgr != nil && app.specService != nil {
		specMgr.SetSpecRepository(app.specService)
	}

	// Select provider based on platform
	var provider capacity.Provider
	var spotChecker *capacity.AWSSpotChecker

	switch app.config.K8s.Platform {
	case "aws-eks":
		// AWS EKS with Karpenter - use NodeClaim watch
		dynamicClient := k8sProvider.GetDynamicClient()
		if dynamicClient != nil && specMgr != nil {
			nodePoolToSpec := specMgr.GetNodePoolToSpecMapping("aws-eks")
			if len(nodePoolToSpec) > 0 {
				logger.InfoCtx(app.ctx, "Using Karpenter provider with %d nodepool mappings: %v", len(nodePoolToSpec), nodePoolToSpec)
				provider = capacity.NewProvider(capacity.ProviderKarpenter, dynamicClient, nodePoolToSpec)
			} else {
				logger.WarnCtx(app.ctx, "No nodepool mappings found, falling back to generic provider")
				provider = capacity.NewProvider(capacity.ProviderGeneric, nil, nil)
			}

			// Setup AWS Spot Checker
			specToInstance := specMgr.GetSpecToInstanceTypeMapping("aws-eks")
			specToNodePool := specMgr.GetSpecToNodePoolMapping("aws-eks")
			if len(specToInstance) > 0 || len(specToNodePool) > 0 {
				ec2Client, region, err := createEC2Client(app.ctx, app.config.K8s.AWS)
				if err != nil {
					logger.WarnCtx(app.ctx, "Failed to create EC2 client for spot checker: %v", err)
				} else {
					spotChecker = capacity.NewAWSSpotChecker(ec2Client, region, specToInstance)
					// Set NodePool fetcher for specs without instance-type configuration
					spotChecker.SetNodePoolFetcher(k8sProvider, specToNodePool)
					logger.InfoCtx(app.ctx, "AWS Spot checker enabled: %d from config, %d from nodepool", len(specToInstance), len(specToNodePool))
				}
			}
		} else {
			logger.WarnCtx(app.ctx, "Dynamic client or spec manager not available, falling back to generic provider")
			provider = capacity.NewProvider(capacity.ProviderGeneric, nil, nil)
		}
	default:
		// aliyun-ack, generic, etc - use generic provider
		provider = capacity.NewProvider(capacity.ProviderGeneric, nil, nil)
	}

	// Create capacity manager
	app.capacityMgr = capacity.NewManager(provider, app.mysqlRepo.SpecCapacity)

	// Set pod count provider for running/pending count updates
	app.capacityMgr.SetPodCountProvider(&k8sPodCountAdapter{provider: k8sProvider})

	// Set spot checker if available
	if spotChecker != nil {
		app.capacityMgr.SetSpotChecker(spotChecker)
	}

	// Set capacity manager on spec manager
	if specMgr != nil {
		specMgr.SetCapacityManager(app.capacityMgr)
	}

	// Start capacity manager in background
	go func() {
		if err := app.capacityMgr.Start(app.ctx); err != nil {
			logger.WarnCtx(app.ctx, "Capacity manager stopped: %v", err)
		}
	}()

	logger.InfoCtx(app.ctx, "✅ Capacity manager setup completed")
	return nil
}

// createEC2Client creates an AWS EC2 client
func createEC2Client(ctx context.Context, awsCfg *config.AWSConfig) (*ec2.Client, string, error) {
	var opts []func(*awsconfig.LoadOptions) error

	// If region is configured
	if awsCfg != nil && awsCfg.Region != "" {
		opts = append(opts, awsconfig.WithRegion(awsCfg.Region))
	}

	// If AK/SK is configured
	if awsCfg != nil && awsCfg.AccessKeyID != "" && awsCfg.SecretAccessKey != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(awsCfg.AccessKeyID, awsCfg.SecretAccessKey, ""),
		))
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, "", err
	}

	return ec2.NewFromConfig(cfg), cfg.Region, nil
}

// k8sPodCountAdapter adapts k8s provider to capacity.PodCountProvider
type k8sPodCountAdapter struct {
	provider *k8s.K8sDeploymentProvider
}

func (a *k8sPodCountAdapter) GetPodCountsBySpec(ctx context.Context) (map[string]capacity.PodCounts, error) {
	k8sCounts, err := a.provider.GetPodCountsBySpec(ctx)
	if err != nil {
		return nil, err
	}

	result := make(map[string]capacity.PodCounts)
	for specName, counts := range k8sCounts {
		result[specName] = capacity.PodCounts{
			Running: counts.Running,
			Pending: counts.Pending,
		}
	}
	return result, nil
}
