package autoscaler

import (
	"context"
	"fmt"
	"sync"
	"time"

	endpointsvc "waverless/internal/service/endpoint"
	"waverless/pkg/interfaces"
	"waverless/pkg/logger"
	"waverless/pkg/store/mysql"
	redisstore "waverless/pkg/store/redis"
)

// MetricsCollector 指标收集器
type MetricsCollector struct {
	deploymentProvider interfaces.DeploymentProvider
	endpointService    *endpointsvc.Service
	workerRepo         *redisstore.WorkerRepository
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

// NewMetricsCollector 创建指标收集器
func NewMetricsCollector(
	deploymentProvider interfaces.DeploymentProvider,
	endpointService *endpointsvc.Service,
	workerRepo *redisstore.WorkerRepository,
	taskRepo *mysql.TaskRepository,
) *MetricsCollector {
	return &MetricsCollector{
		deploymentProvider: deploymentProvider,
		endpointService:    endpointService,
		workerRepo:         workerRepo,
		taskRepo:           taskRepo,
		replicaSnapshots:   make(map[string]replicaSnapshot),
	}
}

// UpdateReplicaSnapshot 更新副本快照，供控制循环快速读取最新状态
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

// CollectEndpointMetrics 收集所有 Endpoint 的指标
func (c *MetricsCollector) CollectEndpointMetrics(ctx context.Context) ([]*EndpointConfig, error) {
	// 获取所有 endpoint 元数据
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

// collectSingleEndpoint 收集单个 Endpoint 的指标
func (c *MetricsCollector) collectSingleEndpoint(ctx context.Context, ep *interfaces.EndpointMetadata) (*EndpointConfig, error) {
	config := &EndpointConfig{
		Name:        ep.Name,
		DisplayName: ep.DisplayName,
		SpecName:    ep.SpecName, // Copy SpecName to avoid re-querying metadata
		MinReplicas: ep.MinReplicas,
		MaxReplicas: ep.MaxReplicas,
		Replicas:    ep.Replicas,
		Priority:    ep.Priority,

		// 扩缩容配置（使用默认值或配置值）
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
	}

	// WARNING: Check for invalid autoscaling configuration
	if config.MaxReplicas == 0 && ep.MaxReplicas == 0 {
		logger.WarnCtx(ctx, "endpoint %s: maxReplicas is 0, autoscaling will NOT work! Please configure maxReplicas > 0", ep.Name)
	}

	// 获取最近的副本状态（优先使用 informer 快照）
	if snapshot, ok := c.getReplicaSnapshot(ep.Name); ok {
		config.ActualReplicas = snapshot.Ready
		config.AvailableReplicas = snapshot.Available
		config.Conditions = snapshot.Conditions
	} else {
		actualReplicas, availableReplicas, drainingReplicas, conditions, err := c.getReplicaStats(ctx, ep.Name)
		if err != nil {
			logger.WarnCtx(ctx, "failed to get actual replicas for %s: %v", ep.Name, err)
			actualReplicas = 0
			availableReplicas = 0
			drainingReplicas = 0
			conditions = nil
		}
		config.ActualReplicas = actualReplicas
		config.AvailableReplicas = availableReplicas
		config.DrainingReplicas = drainingReplicas
		config.Conditions = conditions
	}

	// 获取排队任务数（从MySQL统计）
	pendingCount, err := c.taskRepo.CountByEndpointAndStatus(ctx, ep.Name, "PENDING")
	if err != nil {
		logger.WarnCtx(ctx, "failed to get pending task count for %s: %v", ep.Name, err)
		pendingCount = 0
	}
	config.PendingTasks = pendingCount

	// 获取正在执行的任务数
	runningCount, err := c.getRunningTaskCount(ctx, ep.Name)
	if err != nil {
		logger.WarnCtx(ctx, "failed to get running task count for %s: %v", ep.Name, err)
		runningCount = 0
	}
	config.RunningTasks = runningCount

	// 更新 FirstPendingTime
	if pendingCount > 0 && config.FirstPendingTime.IsZero() {
		config.FirstPendingTime = time.Now()
	} else if pendingCount == 0 {
		config.FirstPendingTime = time.Time{} // 重置
	}

	return config, nil
}

// getReplicaStats 获取 K8s 中实际运行的副本数和正在排空的副本数
func (c *MetricsCollector) getReplicaStats(ctx context.Context, endpoint string) (ready int, available int, draining int, conditions []interfaces.ReplicaCondition, err error) {
	app, err := c.deploymentProvider.GetApp(ctx, endpoint)
	if err != nil {
		return 0, 0, 0, nil, err
	}

	ready = int(app.ReadyReplicas)
	available = int(app.AvailableReplicas)

	// Count draining workers - workers whose pods are marked for deletion
	// These workers are still running and completing tasks, but won't receive new ones
	drainingCount := 0
	workers, err := c.workerRepo.GetByEndpoint(ctx, endpoint)
	if err == nil {
		for _, worker := range workers {
			if worker.Status == "DRAINING" { // model.WorkerStatusDraining
				drainingCount++
			}
		}
	}
	draining = drainingCount

	return ready, available, draining, conditions, nil
}

// getRunningTaskCount 获取正在执行的任务数
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

// getOrDefault 获取值或默认值
func getOrDefault(value, defaultValue int) int {
	if value == 0 {
		return defaultValue
	}
	return value
}

// getBoolOrDefault 获取布尔值或默认值
func getBoolOrDefault(value *bool, defaultValue bool) bool {
	if value == nil {
		return defaultValue
	}
	return *value
}
