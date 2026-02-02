package autoscaler

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"

	"waverless/internal/model"
	endpointsvc "waverless/internal/service/endpoint"
	"waverless/pkg/deploy/k8s"
	"waverless/pkg/interfaces"
	"waverless/pkg/logger"
	"waverless/pkg/store/mysql"
)

// Manager 自动扩缩容管理器
type Manager struct {
	config             *Config
	enabled            bool
	running            bool
	mu                 sync.RWMutex
	stopCh             chan struct{}
	replicaWatchCancel context.CancelFunc
	triggerCh          chan struct{}
	targetQueue        chan string
	queueMu            sync.Mutex
	pendingTargets     map[string]struct{}
	deploymentProvider interfaces.DeploymentProvider
	endpointService    *endpointsvc.Service
	metricsCollector   *MetricsCollector
	resourceCalculator *ResourceCalculator
	decisionEngine     *DecisionEngine
	executor           *Executor
	scalingEventRepo   *mysql.ScalingEventRepository
	lastRunTime        time.Time
	specManager        *k8s.SpecManager
	redisClient        *redis.Client           // Redis用于全局配置存储
	configKey          string                  // 全局配置key
	distributedLock    DistributedLock         // 分布式锁，防止多副本冲突
	workerLister       interfaces.WorkerLister // For worker queries

	// 缓存集群资源状态，避免每次 API 调用都重新计算
	cachedClusterMu        sync.RWMutex
	cachedClusterResources *ClusterResources
	cachedClusterTime      time.Time
}

// NewManager 创建自动扩缩容管理器
func NewManager(
	config *Config,
	deploymentProvider interfaces.DeploymentProvider,
	endpointService *endpointsvc.Service,
	workerLister interfaces.WorkerLister,
	taskRepo *mysql.TaskRepository,
	scalingEventRepo *mysql.ScalingEventRepository,
	redisClient *redis.Client,
	specManager *k8s.SpecManager,
	endpointRepo *mysql.EndpointRepository,
) *Manager {
	resourceCalculator := NewResourceCalculator(deploymentProvider, endpointService, specManager)
	decisionEngine := NewDecisionEngine(config, resourceCalculator)
	executor := NewExecutor(deploymentProvider, endpointService, scalingEventRepo, workerLister, taskRepo, endpointRepo)
	metricsCollector := NewMetricsCollector(deploymentProvider, endpointService, workerLister, taskRepo)

	// 创建分布式锁（如果 redisClient 为 nil，锁会自动降级为单实例模式）
	distributedLock := NewRedisDistributedLock(redisClient, autoscalerLockKey)

	manager := &Manager{
		config:             config,
		enabled:            config.Enabled,
		running:            false,
		stopCh:             make(chan struct{}),
		targetQueue:        make(chan string, 100),
		pendingTargets:     make(map[string]struct{}),
		deploymentProvider: deploymentProvider,
		endpointService:    endpointService,
		metricsCollector:   metricsCollector,
		resourceCalculator: resourceCalculator,
		decisionEngine:     decisionEngine,
		executor:           executor,
		scalingEventRepo:   scalingEventRepo,
		specManager:        specManager,
		workerLister:       workerLister,
		redisClient:        redisClient,
		configKey:          "autoscaler:global-config",
		distributedLock:    distributedLock,
	}

	// 从Redis加载全局配置（如果存在）
	manager.loadPersistedConfig(context.Background())
	return manager
}

// Start 启动自动扩缩容控制循环
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return fmt.Errorf("autoscaler is already running")
	}
	m.running = true
	m.triggerCh = make(chan struct{}, 1)
	m.mu.Unlock()

	logger.InfoCtx(ctx, "starting autoscaler, interval: %d seconds", m.config.Interval)

	// 启动副本变化监听
	m.startReplicaWatcher(ctx)

	// 启动控制循环
	go m.controlLoop(ctx)

	return nil
}

// Stop 停止自动扩缩容
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.running {
		return fmt.Errorf("autoscaler is not running")
	}

	close(m.stopCh)
	if m.triggerCh != nil {
		close(m.triggerCh)
		m.triggerCh = nil
	}
	m.queueMu.Lock()
	for k := range m.pendingTargets {
		delete(m.pendingTargets, k)
	}
	m.queueMu.Unlock()
	if m.replicaWatchCancel != nil {
		m.replicaWatchCancel()
		m.replicaWatchCancel = nil
	}
	m.running = false

	logger.Info("autoscaler stopped")
	return nil
}

// controlLoop 控制循环
func (m *Manager) controlLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(m.config.Interval) * time.Second)
	defer ticker.Stop()

	triggerCh := m.triggerCh

	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			if !m.IsEnabled() {
				continue
			}

			if err := m.runOnce(ctx); err != nil {
				logger.ErrorCtx(ctx, "autoscaler run failed: %v", err)
			}
		case <-triggerCh:
			if !m.IsEnabled() {
				continue
			}
			// consume current targets snapshot
			targets := m.collectTargets()
			if len(targets) == 0 {
				if err := m.runOnce(ctx); err != nil {
					logger.ErrorCtx(ctx, "autoscaler run failed (trigger): %v", err)
				}
				continue
			}
			if err := m.runForTargets(ctx, targets); err != nil {
				logger.ErrorCtx(ctx, "autoscaler partial run failed: %v", err)
			}
		}
	}
}

func (m *Manager) startReplicaWatcher(ctx context.Context) {
	if m.deploymentProvider == nil {
		return
	}

	watchCtx, cancel := context.WithCancel(ctx)
	if err := m.deploymentProvider.WatchReplicas(watchCtx, m.handleReplicaEvent); err != nil {
		cancel()
		logger.WarnCtx(ctx, "failed to start replica watcher: %v", err)
		return
	}

	m.replicaWatchCancel = cancel
}

func (m *Manager) handleReplicaEvent(event interfaces.ReplicaEvent) {
	if m.metricsCollector != nil {
		changed := m.metricsCollector.UpdateReplicaSnapshot(event)
		if changed {
			m.enqueueTarget(event.Name)
		}
	} else {
		m.enqueueTarget(event.Name)
	}
}

func (m *Manager) enqueueTarget(endpoint string) {
	if endpoint == "" {
		m.triggerAutoscaler()
		return
	}
	m.queueMu.Lock()
	if _, exists := m.pendingTargets[endpoint]; exists {
		m.queueMu.Unlock()
		return
	}
	m.pendingTargets[endpoint] = struct{}{}
	m.queueMu.Unlock()

	select {
	case m.targetQueue <- endpoint:
	default:
		logger.Warn("target queue full, falling back to full scan")
	}
	m.triggerAutoscaler()
}

func (m *Manager) collectTargets() []string {
	m.queueMu.Lock()
	defer m.queueMu.Unlock()

	targets := make([]string, 0, len(m.pendingTargets))
	for k := range m.pendingTargets {
		targets = append(targets, k)
		delete(m.pendingTargets, k)
	}
	return targets
}

func (m *Manager) triggerAutoscaler() {
	if m.triggerCh == nil {
		return
	}
	select {
	case m.triggerCh <- struct{}{}:
	default:
	}
}

// runOnce 执行一次扩缩容决策
func (m *Manager) runOnce(ctx context.Context) error {
	// 🔍 DEBUG: 记录每次 runOnce 调用
	logger.InfoCtx(ctx, "autoscaler runOnce called at %s", time.Now().Format("2006-01-02 15:04:05.000"))

	// 🔒 关键改进：使用分布式锁防止多副本冲突
	// 尝试获取分布式锁
	acquired, err := m.distributedLock.TryLock(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire distributed lock: %w", err)
	}

	if !acquired {
		// 另一个副本正在执行扩缩容，跳过本次执行
		logger.DebugCtx(ctx, "autoscaler lock held by another instance, skipping this run")
		return nil
	}

	// 确保释放锁
	defer func() {
		if err := m.distributedLock.Unlock(ctx); err != nil {
			logger.ErrorCtx(ctx, "failed to release distributed lock: %v", err)
		}
	}()

	m.mu.Lock()
	m.lastRunTime = time.Now()
	m.mu.Unlock()

	logger.DebugCtx(ctx, "autoscaler running (lock acquired)...")

	// Step 1: 收集所有 endpoint 的指标
	endpoints, err := m.metricsCollector.CollectEndpointMetrics(ctx)
	if err != nil {
		return fmt.Errorf("failed to collect metrics: %w", err)
	}

	// 🔍 DEBUG: 记录收集到的 endpoint 状态
	for _, ep := range endpoints {
		logger.InfoCtx(ctx, "collected metrics for %s: replicas(desired)=%d, actualReplicas(ready)=%d, pending=%d, running=%d",
			ep.Name, ep.Replicas, ep.ActualReplicas, ep.PendingTasks, ep.RunningTasks)
	}

	if len(endpoints) == 0 {
		logger.DebugCtx(ctx, "no endpoints to scale")
		return nil
	}

	// Filter endpoints based on autoscaler override settings
	enabledEndpoints := make([]*EndpointConfig, 0, len(endpoints))
	for _, ep := range endpoints {
		if m.shouldProcessEndpoint(ep) {
			enabledEndpoints = append(enabledEndpoints, ep)
		} else {
			logger.DebugCtx(ctx, "skipping endpoint %s: autoscaler disabled for this endpoint", ep.Name)
		}
	}

	if len(enabledEndpoints) == 0 {
		logger.DebugCtx(ctx, "no enabled endpoints to scale")
		return nil
	}

	// Use filtered endpoints for resource calculation and decision making
	endpoints = enabledEndpoints

	// Step 2: 计算集群资源使用情况
	maxResources := &Resources{
		GPUCount: m.config.MaxGPUCount,
		CPUCores: float64(m.config.MaxCPUCores),
		MemoryGB: float64(m.config.MaxMemoryGB),
	}
	clusterResources, err := m.resourceCalculator.CalculateClusterResources(ctx, endpoints, maxResources)
	if err != nil {
		return fmt.Errorf("failed to calculate cluster resources: %w", err)
	}

	// 更新缓存
	m.cachedClusterMu.Lock()
	m.cachedClusterResources = clusterResources
	m.cachedClusterTime = time.Now()
	m.cachedClusterMu.Unlock()

	logger.DebugCtx(ctx, "cluster resources: total=%+v, used=%+v, available=%+v",
		clusterResources.Total, clusterResources.Used, clusterResources.Available)

	// Step 3: 做出扩缩容决策
	decisions, err := m.decisionEngine.MakeDecisions(ctx, endpoints, clusterResources)
	if err != nil {
		return fmt.Errorf("failed to make decisions: %w", err)
	}

	if len(decisions) == 0 {
		logger.DebugCtx(ctx, "no scaling decisions to execute")
		return nil
	}

	logger.InfoCtx(ctx, "autoscaler made %d decisions", len(decisions))
	for _, d := range decisions {
		if d.ScaleAmount != 0 {
			logger.InfoCtx(ctx, "decision: endpoint=%s, from=%d, to=%d, amount=%d, priority=%d, approved=%v, reason=%s",
				d.Endpoint, d.CurrentReplicas, d.DesiredReplicas, d.ScaleAmount, d.Priority, d.Approved, d.Reason)
		}
	}

	// Step 4: 执行决策
	if err := m.executor.ExecuteDecisions(ctx, decisions); err != nil {
		return fmt.Errorf("failed to execute decisions: %w", err)
	}

	// Step 4.5: 检查长时间空闲的 worker，触发主动缩容
	if err := m.checkAndScaleDownIdleWorkers(ctx, endpoints); err != nil {
		logger.WarnCtx(ctx, "failed to check idle workers: %v", err)
		// Don't fail the entire autoscaling process if idle worker check fails
	}

	// Step 5: 清理过期事件（超过7天）
	cutoffTime := time.Now().Add(-7 * 24 * time.Hour)
	if deleted, err := m.scalingEventRepo.DeleteOldEvents(ctx, cutoffTime); err != nil {
		logger.WarnCtx(ctx, "failed to cleanup old events: %v", err)
	} else if deleted > 0 {
		logger.InfoCtx(ctx, "cleaned up %d old scaling events", deleted)
	}

	return nil
}

func (m *Manager) runForTargets(ctx context.Context, targets []string) error {
	if len(targets) == 0 {
		return nil
	}

	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return fmt.Errorf("autoscaler not running")
	}
	m.mu.Unlock()

	logger.DebugCtx(ctx, "autoscaler running targeted evaluation for %d endpoints", len(targets))

	acquired, err := m.distributedLock.TryLock(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire distributed lock: %w", err)
	}
	if !acquired {
		logger.DebugCtx(ctx, "lock held by another instance, skipping targeted run")
		return nil
	}
	defer func() {
		if err := m.distributedLock.Unlock(ctx); err != nil {
			logger.ErrorCtx(ctx, "failed to release distributed lock: %v", err)
		}
	}()

	allEndpoints, err := m.metricsCollector.CollectEndpointMetrics(ctx)
	if err != nil {
		return fmt.Errorf("failed to collect metrics: %w", err)
	}
	if len(allEndpoints) == 0 {
		return nil
	}

	targetSet := make(map[string]struct{}, len(targets))
	for _, name := range targets {
		targetSet[name] = struct{}{}
	}

	filtered := make([]*EndpointConfig, 0, len(targets))
	for _, ep := range allEndpoints {
		if _, ok := targetSet[ep.Name]; ok {
			// Check if autoscaler is enabled for this endpoint
			if m.shouldProcessEndpoint(ep) {
				filtered = append(filtered, ep)
			} else {
				logger.DebugCtx(ctx, "skipping target endpoint %s: autoscaler disabled for this endpoint", ep.Name)
			}
		}
	}
	if len(filtered) == 0 {
		logger.DebugCtx(ctx, "no matching endpoints for targeted run")
		return nil
	}

	maxResources := &Resources{
		GPUCount: m.config.MaxGPUCount,
		CPUCores: float64(m.config.MaxCPUCores),
		MemoryGB: float64(m.config.MaxMemoryGB),
	}
	clusterResources, err := m.resourceCalculator.CalculateClusterResources(ctx, allEndpoints, maxResources)
	if err != nil {
		return fmt.Errorf("failed to calculate cluster resources: %w", err)
	}

	decisions, err := m.decisionEngine.MakeDecisions(ctx, filtered, clusterResources)
	if err != nil {
		return fmt.Errorf("failed to make decisions: %w", err)
	}
	if len(decisions) == 0 {
		logger.DebugCtx(ctx, "no targeted decisions to execute")
		return nil
	}

	if err := m.executor.ExecuteDecisions(ctx, decisions); err != nil {
		return fmt.Errorf("failed to execute targeted decisions: %w", err)
	}

	return nil
}

// TriggerScale 手动触发扩缩容
func (m *Manager) TriggerScale(ctx context.Context, endpoint string) error {
	logger.InfoCtx(ctx, "manually triggering scale for endpoint: %s", endpoint)
	return m.runOnce(ctx)
}

// GetClusterResourcesOnly 获取集群资源状态（轻量接口，优先使用缓存）
func (m *Manager) GetClusterResourcesOnly(ctx context.Context) (*ClusterResourcesStatus, error) {
	m.mu.RLock()
	enabled := m.enabled
	running := m.running
	lastRunTime := m.lastRunTime
	m.mu.RUnlock()

	m.cachedClusterMu.RLock()
	cached := m.cachedClusterResources
	cachedTime := m.cachedClusterTime
	m.cachedClusterMu.RUnlock()

	// 如果没有缓存或缓存过期（超过30秒），实时计算一次
	if cached == nil || time.Since(cachedTime) > 30*time.Second {
		endpoints, err := m.metricsCollector.CollectEndpointMetrics(ctx)
		if err != nil {
			return nil, err
		}
		maxResources := &Resources{
			GPUCount: m.config.MaxGPUCount,
			CPUCores: float64(m.config.MaxCPUCores),
			MemoryGB: float64(m.config.MaxMemoryGB),
		}
		cached, err = m.resourceCalculator.CalculateClusterResources(ctx, endpoints, maxResources)
		if err != nil {
			return nil, err
		}
		// 更新缓存
		m.cachedClusterMu.Lock()
		m.cachedClusterResources = cached
		m.cachedClusterTime = time.Now()
		m.cachedClusterMu.Unlock()
	}

	return &ClusterResourcesStatus{
		Enabled:          enabled,
		Running:          running,
		LastRunTime:      lastRunTime,
		ClusterResources: *cached,
	}, nil
}

// GetStatus 获取自动扩缩容状态
func (m *Manager) GetStatus(ctx context.Context) (*AutoScalerStatus, error) {
	m.mu.RLock()
	enabled := m.enabled
	running := m.running
	lastRunTime := m.lastRunTime
	m.mu.RUnlock()

	status := &AutoScalerStatus{
		Enabled:     enabled,
		Running:     running,
		LastRunTime: lastRunTime,
	}

	// 收集 endpoint 状态
	endpoints, err := m.metricsCollector.CollectEndpointMetrics(ctx)
	if err != nil {
		return nil, err
	}

	endpointStatuses := make([]EndpointStatus, 0, len(endpoints))
	for _, ep := range endpoints {
		effectivePrio := ep.EffectivePriority(m.config.StarvationTime)
		idleTime := 0.0
		if !ep.LastTaskTime.IsZero() {
			idleTime = time.Since(ep.LastTaskTime).Seconds()
		}
		waitingTime := 0.0
		if !ep.FirstPendingTime.IsZero() {
			waitingTime = time.Since(ep.FirstPendingTime).Seconds()
		}

		// 计算资源使用
		resourceUsage, _ := m.resourceCalculator.CalculateEndpointResource(ctx, ep, ep.ActualReplicas)
		if resourceUsage == nil {
			resourceUsage = &Resources{}
		}

		endpointStatuses = append(endpointStatuses, EndpointStatus{
			Name:             ep.Name,
			Enabled:          enabled,
			CurrentReplicas:  ep.ActualReplicas,
			DesiredReplicas:  ep.Replicas,
			MinReplicas:      ep.MinReplicas,
			MaxReplicas:      ep.MaxReplicas,
			DrainingReplicas: ep.DrainingReplicas,
			PendingTasks:     ep.PendingTasks,
			RunningTasks:     ep.RunningTasks,
			Priority:         ep.Priority,
			EffectivePrio:    effectivePrio,
			LastScaleTime:    ep.LastScaleTime,
			LastTaskTime:     ep.LastTaskTime,
			IdleTime:         idleTime,
			WaitingTime:      waitingTime,
			ResourceUsage:    *resourceUsage,
		})
	}
	status.Endpoints = endpointStatuses

	// 计算集群资源
	maxResources := &Resources{
		GPUCount: m.config.MaxGPUCount,
		CPUCores: float64(m.config.MaxCPUCores),
		MemoryGB: float64(m.config.MaxMemoryGB),
	}
	clusterResources, err := m.resourceCalculator.CalculateClusterResources(ctx, endpoints, maxResources)
	if err != nil {
		return nil, err
	}
	status.ClusterResources = *clusterResources

	// 获取最近的事件
	recentEvents, err := m.scalingEventRepo.ListRecent(ctx, 20)
	if err != nil {
		logger.WarnCtx(ctx, "failed to list recent events: %v", err)
	} else {
		status.RecentEvents = make([]ScalingEvent, len(recentEvents))
		for i, e := range recentEvents {
			status.RecentEvents[i] = ScalingEvent{
				ID:            e.EventID,
				Endpoint:      e.Endpoint,
				Timestamp:     e.Timestamp,
				Action:        e.Action,
				FromReplicas:  e.FromReplicas,
				ToReplicas:    e.ToReplicas,
				Reason:        e.Reason,
				QueueLength:   e.QueueLength,
				Priority:      e.Priority,
				PreemptedFrom: []string(e.PreemptedFrom),
			}
		}
	}

	return status, nil
}

// GetScalingHistory 获取扩缩容历史
func (m *Manager) GetScalingHistory(ctx context.Context, endpoint string, limit int) ([]*ScalingEvent, error) {
	mysqlEvents, err := m.scalingEventRepo.ListByEndpoint(ctx, endpoint, limit)
	if err != nil {
		return nil, err
	}

	// Convert MySQL events to autoscaler events
	events := make([]*ScalingEvent, len(mysqlEvents))
	for i, e := range mysqlEvents {
		events[i] = &ScalingEvent{
			ID:            e.EventID,
			Endpoint:      e.Endpoint,
			Timestamp:     e.Timestamp,
			Action:        e.Action,
			FromReplicas:  e.FromReplicas,
			ToReplicas:    e.ToReplicas,
			Reason:        e.Reason,
			QueueLength:   e.QueueLength,
			Priority:      e.Priority,
			PreemptedFrom: []string(e.PreemptedFrom),
		}
	}
	return events, nil
}

// Enable 启用自动扩缩容
func (m *Manager) Enable() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.enabled = true
	m.config.Enabled = true
	logger.Info("autoscaler enabled")

	// 持久化配置，避免重启后状态丢失
	m.persistConfig(context.Background())
}

// Disable 禁用自动扩缩容
func (m *Manager) Disable() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.enabled = false
	m.config.Enabled = false
	logger.Info("autoscaler disabled")

	// 持久化配置，避免重启后状态丢失
	m.persistConfig(context.Background())
}

// IsEnabled 检查是否启用
func (m *Manager) IsEnabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.enabled
}

// shouldProcessEndpoint 检查是否应该处理该endpoint的自动扩缩容
// 优先级：endpoint覆盖配置 > 全局配置
func (m *Manager) shouldProcessEndpoint(endpoint *EndpointConfig) bool {
	m.mu.RLock()
	globalEnabled := m.enabled
	m.mu.RUnlock()

	// 如果endpoint有明确的覆盖配置，使用覆盖配置
	if endpoint.AutoscalerEnabled != nil && *endpoint.AutoscalerEnabled != "" {
		switch *endpoint.AutoscalerEnabled {
		case "enabled":
			return true
		case "disabled":
			return false
		}
	}

	// 否则使用全局配置
	return globalEnabled
}

// IsRunning 检查是否正在运行
func (m *Manager) IsRunning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.running
}

// UpdateGlobalConfig 更新全局配置
func (m *Manager) UpdateGlobalConfig(ctx context.Context, config *Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 验证配置参数
	if config.Interval <= 0 {
		return fmt.Errorf("interval must be greater than 0")
	}
	if config.MaxGPUCount < 0 {
		return fmt.Errorf("max_gpu_count must be >= 0")
	}
	if config.MaxCPUCores < 0 {
		return fmt.Errorf("max_cpu_cores must be >= 0")
	}
	if config.MaxMemoryGB < 0 {
		return fmt.Errorf("max_memory_gb must be >= 0")
	}
	if config.StarvationTime < 0 {
		return fmt.Errorf("starvation_time must be >= 0")
	}

	// 更新配置
	m.config.Enabled = config.Enabled
	m.config.Interval = config.Interval
	m.config.MaxGPUCount = config.MaxGPUCount
	m.config.MaxCPUCores = config.MaxCPUCores
	m.config.MaxMemoryGB = config.MaxMemoryGB
	m.config.StarvationTime = config.StarvationTime

	m.enabled = config.Enabled

	logger.InfoCtx(ctx, "autoscaler global config updated: enabled=%v, interval=%d, max_gpu=%d, max_cpu=%d, max_mem=%d, starvation_time=%d",
		config.Enabled, config.Interval, config.MaxGPUCount, config.MaxCPUCores, config.MaxMemoryGB, config.StarvationTime)

	m.persistConfig(ctx)

	return nil
}

// GetGlobalConfig 获取全局配置
func (m *Manager) GetGlobalConfig() *Config {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return &Config{
		Enabled:        m.config.Enabled,
		Interval:       m.config.Interval,
		MaxGPUCount:    m.config.MaxGPUCount,
		MaxCPUCores:    m.config.MaxCPUCores,
		MaxMemoryGB:    m.config.MaxMemoryGB,
		StarvationTime: m.config.StarvationTime,
	}
}

func (m *Manager) loadPersistedConfig(ctx context.Context) {
	if m.redisClient == nil {
		return
	}
	data, err := m.redisClient.Get(ctx, m.configKey).Bytes()
	if err != nil {
		if err != redis.Nil {
			logger.WarnCtx(ctx, "failed to load autoscaler config from redis: %v", err)
		}
		return
	}

	var persisted Config
	if err := json.Unmarshal(data, &persisted); err != nil {
		logger.WarnCtx(ctx, "failed to decode autoscaler config from redis: %v", err)
		return
	}

	m.mu.Lock()
	*m.config = persisted
	m.enabled = persisted.Enabled
	m.mu.Unlock()

	logger.InfoCtx(ctx, "loaded autoscaler config from redis")
}

func (m *Manager) persistConfig(ctx context.Context) {
	if m.redisClient == nil {
		return
	}
	data, err := json.Marshal(m.config)
	if err != nil {
		logger.WarnCtx(ctx, "failed to encode autoscaler config: %v", err)
		return
	}
	if err := m.redisClient.Set(ctx, m.configKey, data, 0).Err(); err != nil {
		logger.WarnCtx(ctx, "failed to persist autoscaler config: %v", err)
	}
}

// checkAndScaleDownIdleWorkers 检查长时间空闲的 worker，触发主动缩容
// 即使 Endpoint 整体未达到空闲阈值，如果有个别 worker 空闲时间过长，也可以缩容
func (m *Manager) checkAndScaleDownIdleWorkers(ctx context.Context, endpoints []*EndpointConfig) error {
	// Check each endpoint for long-idle workers
	for _, ep := range endpoints {
		// Get workers for this endpoint only (optimized query)
		workers, err := m.workerLister.ListWorkers(ctx, ep.Name)
		if err != nil {
			logger.ErrorCtx(ctx, "failed to get workers for endpoint %s: %v", ep.Name, err)
			continue
		}

		if len(workers) == 0 {
			continue
		}

		// Only check if not already at minimum replicas
		if ep.Replicas <= ep.MinReplicas {
			continue
		}

		// 🔒 Critical: Check if there is already a pod being deleted (DRAINING status)
		// If so, skip this endpoint to ensure only one pod is deleted at a time
		hasDrainingWorker := false
		for _, w := range workers {
			if w.Status == model.WorkerStatusDraining {
				hasDrainingWorker = true
				logger.DebugCtx(ctx, "endpoint %s already has draining worker %s, skipping scale-down this cycle", ep.Name, w.ID)
				break
			}
		}
		if hasDrainingWorker {
			continue
		}

		// Find workers idle longer than ScaleDownIdleTime
		scaleDownThreshold := time.Duration(ep.ScaleDownIdleTime) * time.Second
		now := time.Now()

		for _, w := range workers {
			// Skip workers with current jobs
			if w.CurrentJobs > 0 {
				continue
			}

			// Check idle time
			var idleTime time.Duration
			if w.LastTaskTime.IsZero() {
				// Worker never processed tasks, check registration time
				idleTime = now.Sub(w.RegisteredAt)
			} else {
				// Worker processed tasks, check time since last task
				idleTime = now.Sub(w.LastTaskTime)
			}

			if idleTime < scaleDownThreshold {
				continue
			}

			// Found a long-idle worker, trigger scale-down
			logger.InfoCtx(ctx, "found long-idle worker %s for endpoint %s (idle %.0fs >= %ds), triggering proactive scale-down",
				w.ID, ep.Name, idleTime.Seconds(), ep.ScaleDownIdleTime)

			// Create a scale-down decision for this endpoint
			decision := &ScaleDecision{
				Endpoint:        ep.Name,
				CurrentReplicas: ep.Replicas,
				DesiredReplicas: ep.Replicas - 1, // Scale down by 1
				ScaleAmount:     -1,
				Priority:        ep.Priority,
				QueueLength:     ep.PendingTasks,
				Reason:          fmt.Sprintf("Worker-based idle scale-down (worker %s idle %.0fs)", w.ID, idleTime.Seconds()),
				Approved:        true,
			}

			// Execute the scale-down decision immediately
			if err := m.executor.ExecuteDecisions(ctx, []*ScaleDecision{decision}); err != nil {
				logger.WarnCtx(ctx, "failed to execute worker-based scale-down for %s: %v", ep.Name, err)
			}

			// Only scale down one worker per endpoint per cycle
			break
		}
	}

	return nil
}
