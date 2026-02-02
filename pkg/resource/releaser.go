// Package resource provides resource management functionality for the Waverless platform.
// It includes the ResourceReleaser which monitors workers with image pull failures
// and releases resources when the timeout is exceeded.
package resource

import (
	"context"
	"sync"
	"time"

	"waverless/pkg/interfaces"
	"waverless/pkg/logger"
	"waverless/pkg/store/mysql"
	"waverless/pkg/store/mysql/model"

	"go.uber.org/zap"
)

// ResourceReleaserConfig contains configuration for the ResourceReleaser.
// These values can be configured via environment variables or config file.
// Validates: Requirements 5.1, 8.1, 8.2
type ResourceReleaserConfig struct {
	// ImagePullTimeout is the maximum time to wait for image pull before terminating the worker.
	// Default: 5 minutes
	ImagePullTimeout time.Duration `yaml:"imagePullTimeout"`

	// CheckInterval is the interval between checks for stuck workers.
	// Default: 30 seconds
	CheckInterval time.Duration `yaml:"checkInterval"`

	// MaxRetries is the maximum number of termination retries before giving up.
	// Default: 3
	MaxRetries int `yaml:"maxRetries"`
}

// DefaultResourceReleaserConfig returns the default configuration for ResourceReleaser.
func DefaultResourceReleaserConfig() *ResourceReleaserConfig {
	return &ResourceReleaserConfig{
		ImagePullTimeout: 5 * time.Minute,
		CheckInterval:    30 * time.Second,
		MaxRetries:       3,
	}
}

// failedWorkerInfo tracks the first failure time for a worker.
type failedWorkerInfo struct {
	firstFailureTime time.Time
	retryCount       int
}

// ResourceReleaser monitors workers with image pull failures and releases resources
// when the timeout is exceeded. It implements the resource release logic for
// Requirements 5.1, 5.2, 5.3.
type ResourceReleaser struct {
	// deployProvider is the deployment provider that implements WorkerTerminator
	deployProvider interfaces.DeploymentProvider

	// workerRepo is the repository for worker database operations
	workerRepo *mysql.WorkerRepository

	// endpointRepo is the repository for endpoint database operations
	endpointRepo *mysql.EndpointRepository

	// config contains the releaser configuration
	config *ResourceReleaserConfig

	// failedWorkers tracks workers that have failed and their first failure time
	// Key: workerID (pod name), Value: failedWorkerInfo
	failedWorkers sync.Map

	// mu protects concurrent access to internal state
	mu sync.RWMutex

	// running indicates if the releaser is currently running
	running bool
}

// NewResourceReleaser creates a new ResourceReleaser with the given dependencies.
//
// Parameters:
//   - deployProvider: The deployment provider (must implement WorkerTerminator for termination)
//   - workerRepo: Repository for worker database operations
//   - endpointRepo: Repository for endpoint database operations
//   - config: Configuration for the releaser (uses defaults if nil)
//
// Returns:
//   - A new ResourceReleaser instance
func NewResourceReleaser(
	deployProvider interfaces.DeploymentProvider,
	workerRepo *mysql.WorkerRepository,
	endpointRepo *mysql.EndpointRepository,
	config *ResourceReleaserConfig,
) *ResourceReleaser {
	if config == nil {
		config = DefaultResourceReleaserConfig()
	}

	return &ResourceReleaser{
		deployProvider: deployProvider,
		workerRepo:     workerRepo,
		endpointRepo:   endpointRepo,
		config:         config,
	}
}

// Start starts the resource releaser background job.
// It periodically checks for stuck workers and releases resources.
// This method blocks until the context is cancelled.
//
// Parameters:
//   - ctx: Context for cancellation
//
// Validates: Requirements 5.1, 5.2
func (r *ResourceReleaser) Start(ctx context.Context) {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		logger.Warn("ResourceReleaser is already running")
		return
	}
	r.running = true
	r.mu.Unlock()

	logger.Info("ResourceReleaser started",
		zap.Duration("imagePullTimeout", r.config.ImagePullTimeout),
		zap.Duration("checkInterval", r.config.CheckInterval),
		zap.Int("maxRetries", r.config.MaxRetries),
	)

	ticker := time.NewTicker(r.config.CheckInterval)
	defer ticker.Stop()

	// Run initial check
	r.CheckAndRelease(ctx)

	for {
		select {
		case <-ctx.Done():
			r.mu.Lock()
			r.running = false
			r.mu.Unlock()
			logger.Info("ResourceReleaser stopped")
			return
		case <-ticker.C:
			r.CheckAndRelease(ctx)
		}
	}
}

// CheckAndRelease checks for stuck workers and releases resources.
// It performs the following steps:
// 1. Get all workers with IMAGE_PULL_FAILED or CONTAINER_CRASH failure type
// 2. Check if failure duration exceeds timeout
// 3. Call provider's TerminateWorker if available
// 4. Update endpoint health status
//
// Parameters:
//   - ctx: Context for database and provider operations
//
// Validates: Requirements 5.2, 5.3
func (r *ResourceReleaser) CheckAndRelease(ctx context.Context) {
	// Step 1: Get all workers with IMAGE_PULL_FAILED or CONTAINER_CRASH status
	imagePullWorkers, err := r.workerRepo.GetWorkersByFailureType(ctx, string(interfaces.FailureTypeImagePull))
	if err != nil {
		logger.Error("Failed to get workers with image pull failure",
			zap.Error(err),
		)
		return
	}

	containerCrashWorkers, err := r.workerRepo.GetWorkersByFailureType(ctx, string(interfaces.FailureTypeContainerCrash))
	if err != nil {
		logger.Error("Failed to get workers with container crash failure",
			zap.Error(err),
		)
		return
	}

	// Combine both types of failed workers
	workers := append(imagePullWorkers, containerCrashWorkers...)

	if len(workers) == 0 {
		// Clean up tracked workers that are no longer in failed state
		r.cleanupTrackedWorkers(ctx)
		return
	}

	logger.Debug("Found workers with failures",
		zap.Int("imagePullCount", len(imagePullWorkers)),
		zap.Int("containerCrashCount", len(containerCrashWorkers)),
		zap.Int("totalCount", len(workers)),
	)

	// Track endpoints that need health status update
	affectedEndpoints := make(map[string]bool)

	// Step 2 & 3: Check timeout and terminate if needed
	for _, worker := range workers {
		if worker.FailureOccurredAt == nil {
			continue
		}

		// Track the first failure time for this worker
		info := r.getOrCreateFailedWorkerInfo(worker.PodName, *worker.FailureOccurredAt)

		// Calculate how long the worker has been in failed state
		// Use time.Now() for consistency with how GORM stores/retrieves time
		now := time.Now()
		failureDuration := now.Sub(info.firstFailureTime)

		logger.Debug("Checking worker failure duration",
			zap.String("workerID", worker.WorkerID),
			zap.String("endpoint", worker.Endpoint),
			zap.Time("firstFailureTime", info.firstFailureTime),
			zap.Time("now", now),
			zap.Duration("failureDuration", failureDuration),
			zap.Duration("timeout", r.config.ImagePullTimeout),
		)

		if failureDuration >= r.config.ImagePullTimeout {
			// Worker has exceeded timeout, attempt to terminate
			r.terminateWorker(ctx, worker, &info)
			affectedEndpoints[worker.Endpoint] = true
		} else {
			logger.Debug("Worker still within timeout period",
				zap.String("workerID", worker.WorkerID),
				zap.String("endpoint", worker.Endpoint),
				zap.Duration("failureDuration", failureDuration),
				zap.Duration("timeout", r.config.ImagePullTimeout),
			)
		}
	}

	// Step 4: Update endpoint health status for all endpoints with failed workers
	// This ensures health status is always up-to-date, not just when workers are terminated
	endpointsToUpdate := make(map[string]bool)
	for _, worker := range workers {
		endpointsToUpdate[worker.Endpoint] = true
	}
	// Also include endpoints where workers were terminated
	for endpoint := range affectedEndpoints {
		endpointsToUpdate[endpoint] = true
	}

	// Step 5: Check UNHEALTHY/DEGRADED endpoints that may have recovered
	// When all failed workers become OFFLINE, the endpoint should recover to HEALTHY
	unhealthyEndpoints, err := r.endpointRepo.GetByHealthStatus(ctx, string(model.HealthStatusUnhealthy))
	if err != nil {
		logger.Error("Failed to get unhealthy endpoints", zap.Error(err))
	} else {
		for _, ep := range unhealthyEndpoints {
			endpointsToUpdate[ep.Endpoint] = true
		}
	}

	degradedEndpoints, err := r.endpointRepo.GetByHealthStatus(ctx, string(model.HealthStatusDegraded))
	if err != nil {
		logger.Error("Failed to get degraded endpoints", zap.Error(err))
	} else {
		for _, ep := range degradedEndpoints {
			endpointsToUpdate[ep.Endpoint] = true
		}
	}

	for endpoint := range endpointsToUpdate {
		if err := r.UpdateEndpointHealthStatus(ctx, endpoint); err != nil {
			logger.Error("Failed to update endpoint health status",
				zap.String("endpoint", endpoint),
				zap.Error(err),
			)
		}
	}

	// Clean up tracked workers that are no longer in failed state
	r.cleanupTrackedWorkers(ctx)
}

// getOrCreateFailedWorkerInfo gets or creates the failure tracking info for a worker.
func (r *ResourceReleaser) getOrCreateFailedWorkerInfo(workerID string, failureTime time.Time) failedWorkerInfo {
	if existing, ok := r.failedWorkers.Load(workerID); ok {
		return existing.(failedWorkerInfo)
	}

	info := failedWorkerInfo{
		firstFailureTime: failureTime,
		retryCount:       0,
	}
	r.failedWorkers.Store(workerID, info)
	return info
}

// terminateWorker attempts to terminate a worker that has exceeded the timeout.
func (r *ResourceReleaser) terminateWorker(ctx context.Context, worker *model.Worker, info *failedWorkerInfo) {
	// Check if we've exceeded max retries
	if info.retryCount >= r.config.MaxRetries {
		logger.Warn("Max termination retries exceeded for worker",
			zap.String("workerID", worker.WorkerID),
			zap.String("endpoint", worker.Endpoint),
			zap.Int("retryCount", info.retryCount),
		)
		return
	}

	// Check if the provider supports worker termination
	terminator, ok := r.deployProvider.(interfaces.WorkerTerminator)
	if !ok {
		logger.Warn("Deploy provider does not support worker termination",
			zap.String("workerID", worker.WorkerID),
		)
		// Still update the worker status to indicate timeout
		r.updateWorkerTimeoutStatus(ctx, worker)
		return
	}

	// Determine the timeout reason based on failure type
	var reason string
	if worker.FailureType == string(interfaces.FailureTypeContainerCrash) {
		reason = "CONTAINER_CRASH_TIMEOUT: Container crash exceeded timeout of " + r.config.ImagePullTimeout.String()
	} else {
		reason = "IMAGE_PULL_TIMEOUT: Image pull exceeded timeout of " + r.config.ImagePullTimeout.String()
	}

	logger.Info("Terminating worker due to failure timeout",
		zap.String("workerID", worker.WorkerID),
		zap.String("endpoint", worker.Endpoint),
		zap.String("failureType", worker.FailureType),
		zap.Duration("failureDuration", time.Since(info.firstFailureTime)),
	)

	// Attempt to terminate the worker
	err := terminator.TerminateWorker(ctx, worker.Endpoint, worker.WorkerID, reason)
	if err != nil {
		logger.Error("Failed to terminate worker",
			zap.String("workerID", worker.WorkerID),
			zap.String("endpoint", worker.Endpoint),
			zap.Error(err),
		)

		// Increment retry count
		info.retryCount++
		r.failedWorkers.Store(worker.WorkerID, *info)
		return
	}

	logger.Info("Successfully terminated worker",
		zap.String("workerID", worker.WorkerID),
		zap.String("endpoint", worker.Endpoint),
	)

	// Update worker failure type to TIMEOUT to indicate it was terminated due to timeout
	r.updateWorkerTimeoutStatus(ctx, worker)

	// Remove from tracking since it's been terminated
	r.failedWorkers.Delete(worker.WorkerID)
}

// updateWorkerTimeoutStatus updates the worker's failure type to TIMEOUT.
func (r *ResourceReleaser) updateWorkerTimeoutStatus(ctx context.Context, worker *model.Worker) {
	// Use time.Now() for consistency with how GORM stores time
	now := time.Now()
	err := r.workerRepo.UpdateWorkerFailure(
		ctx,
		worker.PodName,
		string(interfaces.FailureTypeTimeout),
		"Image pull timeout, resources released",
		worker.FailureDetails, // Keep original details
		now,
	)
	if err != nil {
		logger.Error("Failed to update worker timeout status",
			zap.String("workerID", worker.WorkerID),
			zap.Error(err),
		)
	}
}

// UpdateEndpointHealthStatus updates the health status of an endpoint based on worker failures.
// It implements Property 7: Endpoint Health Status Derivation from the design document.
//
// The health status is determined by the ratio of failed workers to total workers:
//   - If F = 0 (no failed workers), health_status is "HEALTHY"
//   - If 0 < F < N (some workers failed), health_status is "DEGRADED"
//   - If F = N (all workers failed), health_status is "UNHEALTHY"
//
// Failed workers include both IMAGE_PULL_FAILED and CONTAINER_CRASH types.
// The health_message will use the worker's failure_reason for more intuitive error display.
//
// When status becomes UNHEALTHY due to image or container issues, this method also scales down
// the deployment to 0 replicas to prevent K8s from creating new pods that will fail.
// This implements Property 8: Failed Endpoint Prevents New Pods.
//
// Parameters:
//   - ctx: Context for database operations
//   - endpoint: The name of the endpoint to update
//
// Returns:
//   - error if the database operations fail
//
// Validates: Requirements 5.4, 5.5, 6.4
func (r *ResourceReleaser) UpdateEndpointHealthStatus(ctx context.Context, endpoint string) error {
	// Get all active workers for this endpoint (excludes OFFLINE)
	workers, err := r.workerRepo.GetByEndpoint(ctx, endpoint)
	if err != nil {
		return err
	}

	// Count failed workers and collect failure reasons
	totalWorkers := len(workers)
	failedWorkers := 0
	var firstFailureReason string
	for _, w := range workers {
		if w.FailureType == string(interfaces.FailureTypeImagePull) ||
			w.FailureType == string(interfaces.FailureTypeContainerCrash) {
			failedWorkers++
			// Use the first worker's failure reason as the health message
			if firstFailureReason == "" && w.FailureReason != "" {
				firstFailureReason = w.FailureReason
			}
		}
	}

	// Determine health status
	var healthStatus model.HealthStatus
	var healthMessage string

	if totalWorkers == 0 {
		healthStatus = model.HealthStatusHealthy
		healthMessage = ""
	} else if failedWorkers == 0 {
		healthStatus = model.HealthStatusHealthy
		healthMessage = ""
	} else if failedWorkers < totalWorkers {
		healthStatus = model.HealthStatusDegraded
		// Use worker's failure reason directly for more intuitive message
		if firstFailureReason != "" {
			healthMessage = firstFailureReason
		} else {
			healthMessage = "Some workers failed to start"
		}
	} else {
		healthStatus = model.HealthStatusUnhealthy
		// Use worker's failure reason directly for more intuitive message
		if firstFailureReason != "" {
			healthMessage = firstFailureReason
		} else {
			healthMessage = "All workers failed to start"
		}
	}

	// Update endpoint health status in database
	if err := r.endpointRepo.UpdateHealthStatus(ctx, endpoint, string(healthStatus), healthMessage); err != nil {
		return err
	}

	// Property 8: When endpoint becomes UNHEALTHY, scale down to 0 to prevent K8s from creating new pods
	// This is necessary because K8s Deployment controller will automatically create new pods
	// when existing pods are terminated, bypassing the Autoscaler's blocking logic.
	if healthStatus == model.HealthStatusUnhealthy {
		logger.Info("Endpoint is UNHEALTHY due to worker failures, scaling down to 0 to prevent new pod creation",
			zap.String("endpoint", endpoint),
			zap.Int("failedWorkers", failedWorkers),
			zap.Int("totalWorkers", totalWorkers),
		)

		// Scale down deployment to 0 replicas
		zero := 0
		req := &interfaces.UpdateDeploymentRequest{
			Endpoint: endpoint,
			Replicas: &zero,
		}
		if _, err := r.deployProvider.UpdateDeployment(ctx, req); err != nil {
			logger.Error("Failed to scale down unhealthy endpoint",
				zap.String("endpoint", endpoint),
				zap.Error(err),
			)
			// Don't return error - health status is already updated
		} else {
			logger.Info("Successfully scaled down unhealthy endpoint to 0 replicas",
				zap.String("endpoint", endpoint),
			)
		}
	}

	return nil
}

// cleanupTrackedWorkers removes workers from tracking that are no longer in failed state.
func (r *ResourceReleaser) cleanupTrackedWorkers(ctx context.Context) {
	r.failedWorkers.Range(func(key, value interface{}) bool {
		workerID := key.(string)

		// Check if worker still exists and is still in failed state
		worker, err := r.workerRepo.Get(ctx, workerID)
		if err != nil || worker == nil {
			// Worker no longer exists, remove from tracking
			r.failedWorkers.Delete(workerID)
			return true
		}

		// Worker is no longer in IMAGE_PULL_FAILED or CONTAINER_CRASH state, remove from tracking
		if worker.FailureType != string(interfaces.FailureTypeImagePull) &&
			worker.FailureType != string(interfaces.FailureTypeContainerCrash) {
			r.failedWorkers.Delete(workerID)
		}

		return true
	})
}

// IsRunning returns whether the releaser is currently running.
func (r *ResourceReleaser) IsRunning() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.running
}

// GetConfig returns the current configuration.
func (r *ResourceReleaser) GetConfig() *ResourceReleaserConfig {
	return r.config
}

// GetTrackedWorkerCount returns the number of workers currently being tracked.
func (r *ResourceReleaser) GetTrackedWorkerCount() int {
	count := 0
	r.failedWorkers.Range(func(key, value interface{}) bool {
		count++
		return true
	})
	return count
}
