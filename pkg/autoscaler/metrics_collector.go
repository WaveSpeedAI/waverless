package autoscaler

import (
	"context"
	"fmt"
	"sync"
	"time"

	endpointsvc "waverless/internal/service/endpoint"
	"waverless/pkg/constants"
	"waverless/pkg/interfaces"
	"waverless/pkg/logger"
	"waverless/pkg/store/mysql"
)

// MetricsCollector is the metrics collector
type MetricsCollector struct {
	deploymentProvider interfaces.DeploymentProvider
	endpointService    *endpointsvc.Service
	workerLister       interfaces.WorkerLister
	taskRepo           *mysql.TaskRepository

	replicaMu        sync.RWMutex
	replicaSnapshots map[string]replicaSnapshot
}

type replicaSnapshot struct {
	Desired    int
	Ready      int
	Available  int
	Conditions []interfaces.ReplicaCondition
	UpdatedAt  time.Time
}

// NewMetricsCollector creates a metrics collector
func NewMetricsCollector(
	deploymentProvider interfaces.DeploymentProvider,
	endpointService *endpointsvc.Service,
	workerLister interfaces.WorkerLister,
	taskRepo *mysql.TaskRepository,
) *MetricsCollector {
	return &MetricsCollector{
		deploymentProvider: deploymentProvider,
		endpointService:    endpointService,
		workerLister:       workerLister,
		taskRepo:           taskRepo,
		replicaSnapshots:   make(map[string]replicaSnapshot),
	}
}

// UpdateReplicaSnapshot updates replica snapshot for control loop to quickly read latest state
func (c *MetricsCollector) UpdateReplicaSnapshot(event interfaces.ReplicaEvent) bool {
	c.replicaMu.Lock()
	defer c.replicaMu.Unlock()

	prev, ok := c.replicaSnapshots[event.Name]
	c.replicaSnapshots[event.Name] = replicaSnapshot{
		Desired:    event.DesiredReplicas,
		Ready:      event.ReadyReplicas,
		Available:  event.AvailableReplicas,
		Conditions: event.Conditions,
		UpdatedAt:  time.Now(),
	}

	if !ok {
		return true
	}

	if prev.Desired != event.DesiredReplicas ||
		prev.Ready != event.ReadyReplicas ||
		prev.Available != event.AvailableReplicas {
		return true
	}

	return false
}

func (c *MetricsCollector) getReplicaSnapshot(name string) (replicaSnapshot, bool) {
	c.replicaMu.RLock()
	defer c.replicaMu.RUnlock()
	snap, ok := c.replicaSnapshots[name]
	return snap, ok
}

// CollectEndpointMetrics collects metrics for all endpoints
func (c *MetricsCollector) CollectEndpointMetrics(ctx context.Context) ([]*EndpointConfig, error) {
	// Get all endpoint metadata
	endpoints, err := c.endpointService.ListEndpoints(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list endpoints: %w", err)
	}

	configs := make([]*EndpointConfig, 0, len(endpoints))
	for _, ep := range endpoints {
		config, err := c.collectSingleEndpoint(ctx, ep)
		if err != nil {
			logger.ErrorCtx(ctx, "failed to collect metrics for endpoint %s: %v", ep.Name, err)
			continue
		}
		configs = append(configs, config)
	}

	return configs, nil
}

// collectSingleEndpoint collects metrics for a single endpoint
func (c *MetricsCollector) collectSingleEndpoint(ctx context.Context, ep *interfaces.EndpointMetadata) (*EndpointConfig, error) {
	config := &EndpointConfig{
		Name:        ep.Name,
		DisplayName: ep.DisplayName,
		SpecName:    ep.SpecName, // Copy SpecName to avoid re-querying metadata
		MinReplicas: ep.MinReplicas,
		MaxReplicas: ep.MaxReplicas,
		Replicas:    ep.Replicas,
		Priority:    ep.Priority,

		// Autoscaling config (use default or configured values)
		ScaleUpThreshold:  getOrDefault(ep.ScaleUpThreshold, 1),
		ScaleDownIdleTime: getOrDefault(ep.ScaleDownIdleTime, 300),
		ScaleUpCooldown:   getOrDefault(ep.ScaleUpCooldown, 30),
		ScaleDownCooldown: getOrDefault(ep.ScaleDownCooldown, 60),
		EnableDynamicPrio: getBoolOrDefault(ep.EnableDynamicPrio, true),
		HighLoadThreshold: getOrDefault(ep.HighLoadThreshold, 10),
		PriorityBoost:     getOrDefault(ep.PriorityBoost, 20),
		AutoscalerEnabled: ep.AutoscalerEnabled,
		LastScaleTime:     ep.LastScaleTime,
		LastTaskTime:      ep.LastTaskTime,
		FirstPendingTime:  ep.FirstPendingTime,

		// Use replica status from database directly, no longer calling K8s API
		ActualReplicas:    ep.ReadyReplicas,
		AvailableReplicas: ep.AvailableReplicas,
	}

	// WARNING: Check for invalid autoscaling configuration
	if config.MaxReplicas == 0 && ep.MaxReplicas == 0 {
		logger.WarnCtx(ctx, "endpoint %s: maxReplicas is 0, autoscaling will NOT work! Please configure maxReplicas > 0", ep.Name)
	}

	// Prefer informer snapshot (if more recent data available)
	if snapshot, ok := c.getReplicaSnapshot(ep.Name); ok {
		config.ActualReplicas = snapshot.Ready
		config.AvailableReplicas = snapshot.Available
		config.Conditions = snapshot.Conditions
	}

	// Get pending task count (from MySQL)
	pendingCount, err := c.taskRepo.CountByEndpointAndStatus(ctx, ep.Name, constants.TaskStatusPending.String())
	if err != nil {
		logger.WarnCtx(ctx, "failed to get pending task count for %s: %v", ep.Name, err)
		pendingCount = 0
	}
	config.PendingTasks = pendingCount

	// Get running task count
	runningCount, err := c.getRunningTaskCount(ctx, ep.Name)
	if err != nil {
		logger.WarnCtx(ctx, "failed to get running task count for %s: %v", ep.Name, err)
		runningCount = 0
	}
	config.RunningTasks = runningCount

	// Update FirstPendingTime
	if pendingCount > 0 && config.FirstPendingTime.IsZero() {
		config.FirstPendingTime = time.Now()
	} else if pendingCount == 0 {
		config.FirstPendingTime = time.Time{} // Reset
	}

	return config, nil
}

// getRunningTaskCount gets the count of running tasks
func (c *MetricsCollector) getRunningTaskCount(ctx context.Context, endpoint string) (int64, error) {
	// OPTIMIZATION: Use in-progress index instead of scanning all tasks
	// Get all in-progress task IDs from Redis SET
	taskIDs, err := c.taskRepo.GetInProgressTasks(ctx)
	if err != nil {
		return 0, err
	}

	// If no endpoint filter, return total count
	if endpoint == "" {
		return int64(len(taskIDs)), nil
	}

	// If endpoint filter is specified, count matching tasks
	// TODO: Add per-endpoint in-progress index for O(1) lookup
	count := int64(0)
	for _, taskID := range taskIDs {
		task, err := c.taskRepo.Get(ctx, taskID)
		if err != nil || task == nil {
			continue
		}
		if task.Endpoint == endpoint {
			count++
		}
	}

	return count, nil
}

// getOrDefault gets value or default value
func getOrDefault(value, defaultValue int) int {
	if value == 0 {
		return defaultValue
	}
	return value
}

// getBoolOrDefault gets boolean value or default value
func getBoolOrDefault(value *bool, defaultValue bool) bool {
	if value == nil {
		return defaultValue
	}
	return *value
}
