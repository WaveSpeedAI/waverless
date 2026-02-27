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
	"waverless/pkg/interfaces"
	"waverless/pkg/logger"
	"waverless/pkg/provider/k8s"
	"waverless/pkg/store/mysql"
	redisstore "waverless/pkg/store/redis"
)

const (
	// autoscalerLockKey is the key name for the distributed lock
	autoscalerLockKey = "autoscaler:global-lock"
)

// Manager is the autoscaler manager
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
	redisClient        *redis.Client              // Redis for global config storage
	configKey          string                     // Global config key
	distributedLock    redisstore.DistributedLock // Distributed lock to prevent multi-replica conflicts
	workerLister       interfaces.WorkerLister    // For worker queries

	// Cache cluster resource status to avoid recalculating on every API call
	cachedClusterMu        sync.RWMutex
	cachedClusterResources *ClusterResources
	cachedClusterTime      time.Time
}

// NewManager creates an autoscaler manager
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

	// Create distributed lock (if redisClient is nil, lock will automatically degrade to single-instance mode)
	lockStore := redisstore.NewLockStore(redisClient)
	distributedLock := lockStore.NewLock(autoscalerLockKey, nil)

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

	// Load global config from Redis (if exists)
	manager.loadPersistedConfig(context.Background())
	return manager
}

// Start starts the autoscaler control loop
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

	// Start replica change watcher
	m.startReplicaWatcher(ctx)

	// Start control loop
	go m.controlLoop(ctx)

	return nil
}

// Stop stops the autoscaler
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

// controlLoop is the main control loop
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

// runOnce executes one autoscaling decision cycle
func (m *Manager) runOnce(ctx context.Context) error {
	// 🔍 DEBUG: Log each runOnce call
	logger.InfoCtx(ctx, "autoscaler runOnce called at %s", time.Now().Format("2006-01-02 15:04:05.000"))

	// 🔒 Key improvement: Use distributed lock to prevent multi-replica conflicts
	// Try to acquire distributed lock
	acquired, err := m.distributedLock.TryLock(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire distributed lock: %w", err)
	}

	if !acquired {
		// Another replica is executing autoscaling, skip this run
		logger.DebugCtx(ctx, "autoscaler lock held by another instance, skipping this run")
		return nil
	}

	// Ensure lock is released
	defer func() {
		if err := m.distributedLock.Unlock(ctx); err != nil {
			logger.ErrorCtx(ctx, "failed to release distributed lock: %v", err)
		}
	}()

	m.mu.Lock()
	m.lastRunTime = time.Now()
	m.mu.Unlock()

	logger.DebugCtx(ctx, "autoscaler running (lock acquired)...")

	// Step 1: Collect metrics for all endpoints
	endpoints, err := m.metricsCollector.CollectEndpointMetrics(ctx)
	if err != nil {
		return fmt.Errorf("failed to collect metrics: %w", err)
	}

	// 🔍 DEBUG: Log collected endpoint status
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

	// Step 2: Calculate cluster resource usage
	maxResources := &Resources{
		GPUCount: m.config.MaxGPUCount,
		CPUCores: float64(m.config.MaxCPUCores),
		MemoryGB: float64(m.config.MaxMemoryGB),
	}
	clusterResources, err := m.resourceCalculator.CalculateClusterResources(ctx, endpoints, maxResources)
	if err != nil {
		return fmt.Errorf("failed to calculate cluster resources: %w", err)
	}

	// Update cache
	m.cachedClusterMu.Lock()
	m.cachedClusterResources = clusterResources
	m.cachedClusterTime = time.Now()
	m.cachedClusterMu.Unlock()

	logger.DebugCtx(ctx, "cluster resources: total=%+v, used=%+v, available=%+v",
		clusterResources.Total, clusterResources.Used, clusterResources.Available)

	// Step 3: Make scaling decisions
	decisions, err := m.decisionEngine.MakeDecisions(ctx, endpoints, clusterResources)
	if err != nil {
		return fmt.Errorf("failed to make decisions: %w", err)
	}

	// Step 4: Execute decisions (if any)
	if len(decisions) > 0 {
		logger.InfoCtx(ctx, "autoscaler made %d decisions", len(decisions))
		for _, d := range decisions {
			if d.ScaleAmount != 0 {
				logger.InfoCtx(ctx, "decision: endpoint=%s, from=%d, to=%d, amount=%d, priority=%d, approved=%v, reason=%s",
					d.Endpoint, d.CurrentReplicas, d.DesiredReplicas, d.ScaleAmount, d.Priority, d.Approved, d.Reason)
			}
		}

		if err := m.executor.ExecuteDecisions(ctx, decisions); err != nil {
			return fmt.Errorf("failed to execute decisions: %w", err)
		}
	} else {
		logger.DebugCtx(ctx, "no scaling decisions from decision engine")
	}

	// Step 4.5: Check for long-idle workers and trigger proactive scale-down
	// 🔥 CRITICAL: This must run even when there are no decisions from decision engine
	// because individual workers may be idle even if the endpoint overall is not
	if err := m.checkAndScaleDownIdleWorkers(ctx, endpoints); err != nil {
		logger.WarnCtx(ctx, "failed to check idle workers: %v", err)
		// Don't fail the entire autoscaling process if idle worker check fails
	}

	// Step 5: Clean up expired events (older than 7 days)
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

	// Execute decisions (if any)
	if len(decisions) > 0 {
		if err := m.executor.ExecuteDecisions(ctx, decisions); err != nil {
			return fmt.Errorf("failed to execute targeted decisions: %w", err)
		}
	} else {
		logger.DebugCtx(ctx, "no targeted decisions from decision engine")
	}

	// 🔥 CRITICAL: Also check for long-idle workers in targeted endpoints
	// This ensures worker-level idle check runs even when no endpoint-level decisions
	if err := m.checkAndScaleDownIdleWorkers(ctx, filtered); err != nil {
		logger.WarnCtx(ctx, "failed to check idle workers in targeted run: %v", err)
	}

	return nil
}

// TriggerScale manually triggers scaling
func (m *Manager) TriggerScale(ctx context.Context, endpoint string) error {
	logger.InfoCtx(ctx, "manually triggering scale for endpoint: %s", endpoint)
	return m.runOnce(ctx)
}

// GetClusterResourcesOnly gets cluster resource status (lightweight API, prefers cache)
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

	// If no cache or cache expired (over 30 seconds), calculate in real-time
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
		// Update cache
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

// GetStatus gets autoscaler status
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

	// Collect endpoint status
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

		// Calculate resource usage
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

	// Calculate cluster resources
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

	// Get recent events
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

// GetScalingHistory gets scaling history
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

// Enable enables autoscaler
func (m *Manager) Enable() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.enabled = true
	m.config.Enabled = true
	logger.Info("autoscaler enabled")

	// Persist config to avoid state loss after restart
	m.persistConfig(context.Background())
}

// Disable disables autoscaler
func (m *Manager) Disable() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.enabled = false
	m.config.Enabled = false
	logger.Info("autoscaler disabled")

	// Persist config to avoid state loss after restart
	m.persistConfig(context.Background())
}

// IsEnabled checks if autoscaler is enabled
func (m *Manager) IsEnabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.enabled
}

// shouldProcessEndpoint checks if autoscaling should be processed for this endpoint
// Priority: endpoint override config > global config
func (m *Manager) shouldProcessEndpoint(endpoint *EndpointConfig) bool {
	m.mu.RLock()
	globalEnabled := m.enabled
	m.mu.RUnlock()

	// If endpoint has explicit override config, use override config
	if endpoint.AutoscalerEnabled != nil && *endpoint.AutoscalerEnabled != "" {
		switch *endpoint.AutoscalerEnabled {
		case "enabled":
			return true
		case "disabled":
			return false
		}
	}

	// Otherwise use global config
	return globalEnabled
}

// IsRunning checks if autoscaler is running
func (m *Manager) IsRunning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.running
}

// UpdateGlobalConfig updates global config
func (m *Manager) UpdateGlobalConfig(ctx context.Context, config *Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Validate config parameters
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

	// Update config
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

// GetGlobalConfig gets global config
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

// checkAndScaleDownIdleWorkers checks for long-idle workers and triggers proactive scale-down
// Even if the Endpoint overall hasn't reached idle threshold, individual workers with excessive idle time can be scaled down
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

			// 🔥 CRITICAL: Skip STARTING workers - they haven't finished initialization yet
			// STARTING workers should not be considered for idle scale-down
			if w.Status == model.WorkerStatusStarting {
				logger.DebugCtx(ctx, "endpoint %s: skipping STARTING worker %s for idle check", ep.Name, w.ID)
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
