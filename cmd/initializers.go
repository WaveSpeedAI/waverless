package main

import (
	"context"
	"fmt"
	"net/http"

	"waverless/app/handler"
	"waverless/app/router"
	"waverless/internal/service"
	endpointsvc "waverless/internal/service/endpoint"
	"waverless/internal/service/lifecycle"
	"waverless/pkg/autoscaler"
	"waverless/pkg/capacity"
	"waverless/pkg/config"
	"waverless/pkg/logger"
	"waverless/pkg/provider"
	"waverless/pkg/provider/k8s"
	"waverless/pkg/provider/novita"
	"waverless/pkg/resource"
	"waverless/pkg/status"
	mysqlstore "waverless/pkg/store/mysql"
	redisstore "waverless/pkg/store/redis"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ec2"

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
		app.mysqlRepo.Monitoring,
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

	// Wire status summary updater on worker service so heartbeat-driven status changes
	// (e.g. STARTING → ONLINE) trigger endpoint status summary recomputation.
	app.workerService.SetStatusSummaryUpdater(func(ctx context.Context, endpoint string) {
		if app.endpointService != nil {
			if err := app.endpointService.UpdateStatusSummary(ctx, endpoint); err != nil {
				logger.WarnCtx(ctx, "Failed to update status summary from heartbeat for endpoint %s: %v", endpoint, err)
			}
		}
	})

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

	// Initialize status event service (for status event tracking)
	app.statusEventService = service.NewStatusEventService(app.mysqlRepo.StatusEvent, nil)

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

	// Setup Lifecycle Manager for unified watcher management
	// This replaces all the individual setup*Watcher methods
	if err := app.setupLifecycleManager(k8sDeployProvider, novitaDeployProvider); err != nil {
		logger.WarnCtx(app.ctx, "Failed to setup lifecycle manager: %v (non-critical, continuing)", err)
	}

	// Wire status summary dependencies on endpoint service
	// These are needed for ComputeStatusSummary and UpdateStatusSummary
	// Validates: Requirement 4.3
	app.endpointService.SetWorkerRepository(app.mysqlRepo.Worker)
	app.endpointService.SetEndpointRepository(app.mysqlRepo.Endpoint)

	// Start pod cleanup job for stuck terminating pods (when K8s is enabled)
	if err := app.startPodCleanupJob(k8sDeployProvider); err != nil {
		logger.WarnCtx(app.ctx, "Failed to start pod cleanup job: %v (non-critical, continuing)", err)
		// Non-critical feature, continue startup
	}

	// Setup capacity manager (when K8s is enabled)
	if err := app.setupCapacityManager(k8sDeployProvider); err != nil {
		logger.WarnCtx(app.ctx, "Failed to setup capacity manager: %v (non-critical, continuing)", err)
	}

	// Setup Resource Releaser for automatic cleanup of failed workers
	// This monitors workers with IMAGE_PULL_FAILED status and terminates them after timeout
	// Validates: Requirements 5.1, 5.2, 5.3, 5.4
	if err := app.setupResourceReleaser(); err != nil {
		logger.WarnCtx(app.ctx, "Failed to setup resource releaser: %v (non-critical, continuing)", err)
	}

	return nil
}

// setupLifecycleManager sets up the unified lifecycle manager for all providers
// This replaces the individual setup*Watcher methods with a centralized approach
func (app *Application) setupLifecycleManager(k8sProvider *k8s.K8sDeploymentProvider, novitaProvider *novita.NovitaDeploymentProvider) error {
	// Create lifecycle manager
	app.lifecycleManager = lifecycle.NewManager(
		app.ctx,
		app.mysqlRepo.Worker,
		app.mysqlRepo.Endpoint,
		app.workerService,
		app.workerEventService,
	)

	// Wire endpoint service for status summary updates on worker status changes
	// Validates: Requirement 4.3
	if app.endpointService != nil {
		app.lifecycleManager.SetEndpointService(app.endpointService)
	}

	// Wire status event recorder and pending phase detector for status_events recording.
	// This enables recording of status changes, pending phase transitions, and failures.
	// Validates: Requirements 1.4, 3.1
	if app.statusEventService != nil {
		app.lifecycleManager.SetStatusEventRecorder(app.statusEventService)
		logger.InfoCtx(app.ctx, "Status event recorder wired into lifecycle manager")
	}
	// Create and wire pending phase detector
	pendingDetector := status.NewPendingPhaseDetector(nil)
	app.lifecycleManager.SetPendingPhaseDetector(pendingDetector)
	logger.InfoCtx(app.ctx, "Pending phase detector wired into lifecycle manager")

	// Register K8s provider if enabled
	if k8sProvider != nil {
		logger.InfoCtx(app.ctx, "Registering K8s provider with lifecycle manager...")
		if err := app.lifecycleManager.RegisterK8sProvider(k8sProvider); err != nil {
			logger.WarnCtx(app.ctx, "Failed to register K8s provider: %v", err)
		}
	}

	// Register Novita provider if enabled
	if novitaProvider != nil {
		logger.InfoCtx(app.ctx, "Registering Novita provider with lifecycle manager...")
		if err := app.lifecycleManager.RegisterNovitaProvider(novitaProvider); err != nil {
			logger.WarnCtx(app.ctx, "Failed to register Novita provider: %v", err)
		}
	}

	logger.InfoCtx(app.ctx, "✅ Lifecycle manager setup completed with providers: %v", app.lifecycleManager.GetRegisteredProviders())
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

	// Initialize Status Event Handler (for status event API)
	if app.statusEventService != nil {
		app.statusEventHandler = handler.NewStatusEventHandler(app.statusEventService)
		logger.InfoCtx(app.ctx, "Status event handler initialized")
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
	if app.config.ResourceReleaser.NodeProvisionTimeout > 0 {
		releaserConfig.NodeProvisionTimeout = app.config.ResourceReleaser.NodeProvisionTimeout
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
		logger.InfoCtx(app.ctx, "Starting resource releaser with config: imagePullTimeout=%v, nodeProvisionTimeout=%v, checkInterval=%v, maxRetries=%d",
			releaserConfig.ImagePullTimeout, releaserConfig.NodeProvisionTimeout, releaserConfig.CheckInterval, releaserConfig.MaxRetries)
		releaser.Start(app.ctx)
	}()

	logger.InfoCtx(app.ctx, "✅ Resource releaser setup completed")
	return nil
}

// initHTTPServer initializes HTTP server
func (app *Application) initHTTPServer() error {
	// Initialize router
	r := router.NewRouter(app.taskHandler, app.workerHandler, app.endpointHandler, app.autoscalerHandler, app.statisticsHandler, app.specHandler, app.imageHandler, app.monitoringHandler, app.statusEventHandler)

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

	// Wire capacity manager into endpoint service for Spot status lookup in status summary
	// Validates: Requirement 4.3
	if app.endpointService != nil {
		app.endpointService.SetCapacityManager(app.capacityMgr)
	}

	// Wire spot price lookup into lifecycle manager for recording spot price at worker creation
	if app.lifecycleManager != nil {
		app.lifecycleManager.SetSpotPriceLookup(&capacitySpotPriceLookup{mgr: app.capacityMgr})
	}

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

// capacitySpotPriceLookup adapts capacity.Manager to lifecycle.SpotPriceLookup
type capacitySpotPriceLookup struct {
	mgr *capacity.Manager
}

func (a *capacitySpotPriceLookup) GetSpotPriceBySpec(specName string) (float64, string, bool) {
	spotStatus := a.mgr.GetSpotStatusBySpec(specName)
	if spotStatus == nil || spotStatus.Price <= 0 {
		return 0, "", false
	}
	return spotStatus.Price, spotStatus.InstanceType, true
}
