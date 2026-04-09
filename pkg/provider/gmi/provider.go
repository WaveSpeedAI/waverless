package gmi

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-redis/redis/v8"

	"waverless/pkg/config"
	"waverless/pkg/interfaces"
	"waverless/pkg/logger"
	redisstore "waverless/pkg/store/redis"
)

// GMIDeploymentProvider implements the DeploymentProvider interface.
// It calls the ieops-v2 BFF API (/api/v1/models) for deployment operations.
type GMIDeploymentProvider struct {
	baseURL      string
	token        string
	client       *http.Client
	cfg          *config.Config
	gmiConfig    *config.GMIConfig
	pollInterval time.Duration

	// Worker state tracking for polling-based sync
	workerStates   sync.Map // workerID → *gmiWorkerState
	watcherRunning sync.Once
	watcherCtx     context.Context
	watcherCancel  context.CancelFunc

	// Worker status change callbacks
	workerStatusCallbacks     map[uint64]WorkerStatusChangeCallback
	workerStatusCallbacksLock sync.RWMutex
	workerDeleteCallbacks     map[uint64]WorkerDeleteCallback
	workerDeleteCallbacksLock sync.RWMutex
	nextCallbackID            uint64

	// Redis client for draining workers tracking (multi-replica safe)
	redisClient   *redis.Client
	drainingStore *redisstore.DrainingStore
}

// NewGMIDeploymentProvider creates a new GMI deployment provider.
func NewGMIDeploymentProvider(cfg *config.Config) (interfaces.DeploymentProvider, error) {
	if !cfg.GMI.Enabled {
		return nil, fmt.Errorf("gmi provider is not enabled in config")
	}

	baseURL := cfg.GMI.BaseURL
	if baseURL == "" {
		return nil, fmt.Errorf("gmi base_url is required")
	}
	baseURL = strings.TrimRight(baseURL, "/")

	token := cfg.GMI.APIKey

	pollInterval := 10 * time.Second
	if cfg.GMI.PollInterval > 0 {
		pollInterval = time.Duration(cfg.GMI.PollInterval) * time.Second
	}

	return &GMIDeploymentProvider{
		baseURL: baseURL,
		token:   token,
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
		cfg:                   cfg,
		gmiConfig:             &cfg.GMI,
		pollInterval:          pollInterval,
		workerStatusCallbacks: make(map[uint64]WorkerStatusChangeCallback),
		workerDeleteCallbacks: make(map[uint64]WorkerDeleteCallback),
	}, nil
}

// ========================================
// CoreDeploymentProvider (required) — BFF API
// ========================================

func (p *GMIDeploymentProvider) Deploy(ctx context.Context, req *interfaces.DeployRequest) (*interfaces.DeployResponse, error) {
	logger.Infof("GMI Deploy: endpoint=%s, image=%s, replicas=%d, spec=%s, gpuCount=%d",
		req.Endpoint, req.Image, req.Replicas, req.SpecName, req.GpuCount)

	// Build env: defaults + user env
	mergedEnv := p.mergeEnv(p.buildDefaultEnv(req.Endpoint), req.Env)

	// Build BFF create request
	createReq := map[string]any{
		"name":       req.Endpoint,
		"model":      req.Endpoint,
		"image":      req.Image,
		"replicas":   req.Replicas,
		"envVars":    mergedEnv,
		"gpuProfile": buildBFFGPUProfile(req.SpecName, req.GpuCount),
	}
	if req.ShmSize != "" {
		createReq["shmSize"] = req.ShmSize
	}
	if len(req.VolumeMounts) > 0 {
		createReq["volumeMounts"] = req.VolumeMounts
	}
	// Pass registry credential to BFF for K8s imagePullSecrets (not as env vars)
	if req.RegistryCredential != nil {
		createReq["registryCredential"] = map[string]string{
			"registry": req.RegistryCredential.Registry,
			"username": req.RegistryCredential.Username,
			"password": req.RegistryCredential.Password,
		}
	}

	if err := p.doRequestBFF(ctx, "POST", "/api/v1/models", createReq, nil); err != nil {
		return nil, fmt.Errorf("failed to deploy via BFF API: %w", err)
	}

	logger.Infof("GMI Deploy: endpoint=%s, SUCCESS", req.Endpoint)
	return &interfaces.DeployResponse{
		Endpoint:  req.Endpoint,
		Message:   "Successfully deployed via BFF API",
		CreatedAt: time.Now().Format(time.RFC3339),
	}, nil
}

func (p *GMIDeploymentProvider) GetApp(ctx context.Context, endpoint string) (*interfaces.AppInfo, error) {
	var resp bffModelResponse
	path := "/api/v1/models/" + endpoint
	if err := p.doRequestBFF(ctx, "GET", path, nil, &resp); err != nil {
		return nil, err
	}
	return convertBFFModelToAppInfo(&resp), nil
}

func (p *GMIDeploymentProvider) ListApps(ctx context.Context) ([]*interfaces.AppInfo, error) {
	var respList []bffModelResponse
	if err := p.doRequestBFF(ctx, "GET", "/api/v1/models", nil, &respList); err != nil {
		return nil, err
	}

	apps := make([]*interfaces.AppInfo, len(respList))
	for i := range respList {
		apps[i] = convertBFFModelToAppInfo(&respList[i])
	}
	return apps, nil
}

func (p *GMIDeploymentProvider) DeleteApp(ctx context.Context, endpoint string) error {
	logger.Infof("GMI DeleteApp: endpoint=%s", endpoint)

	path := "/api/v1/models/" + endpoint
	if err := p.doRequestBFF(ctx, "DELETE", path, nil, nil); err != nil {
		return fmt.Errorf("failed to delete via BFF API: %w", err)
	}

	logger.Infof("GMI DeleteApp: endpoint=%s, SUCCESS", endpoint)
	return nil
}

func (p *GMIDeploymentProvider) ScaleApp(ctx context.Context, endpoint string, replicas int) error {
	logger.Infof("GMI ScaleApp: endpoint=%s, target replicas=%d", endpoint, replicas)

	// Drain-first: if scaling down, select and drain workers before reducing replicas
	current, err := p.GetApp(ctx, endpoint)
	if err != nil {
		logger.Warnf("GMI ScaleApp: failed to get current app info (skipping drain-first): %v", err)
	}
	if err == nil && current != nil && int(current.Replicas) > replicas {
		excess := int(current.Replicas) - replicas
		pods, podErr := p.GetPods(ctx, endpoint)
		if podErr == nil && len(pods) > 0 {
			toDrain := p.selectWorkersToDrain(pods, excess)
			if len(toDrain) > 0 {
				logger.Infof("GMI ScaleApp: drain-first: draining %d workers before scale-down", len(toDrain))
				if drainErr := p.DrainWorkers(ctx, endpoint, toDrain); drainErr != nil {
					logger.Warnf("GMI ScaleApp: drain before scale failed (continuing): %v", drainErr)
				}
			}
		}
	}

	path := "/api/v1/models/" + endpoint + "/scale"
	scaleReq := map[string]any{
		"replicas": replicas,
	}

	if err := p.doRequestBFF(ctx, "POST", path, scaleReq, nil); err != nil {
		return fmt.Errorf("failed to scale via BFF API: %w", err)
	}

	logger.Infof("GMI ScaleApp: endpoint=%s, replicas=%d, SUCCESS", endpoint, replicas)
	return nil
}

func (p *GMIDeploymentProvider) GetAppStatus(ctx context.Context, endpoint string) (*interfaces.AppStatus, error) {
	app, err := p.GetApp(ctx, endpoint)
	if err != nil {
		return nil, err
	}

	return &interfaces.AppStatus{
		Endpoint:          app.Name,
		Status:            app.Status,
		ReadyReplicas:     app.ReadyReplicas,
		AvailableReplicas: app.AvailableReplicas,
		TotalReplicas:     app.Replicas,
	}, nil
}

func (p *GMIDeploymentProvider) UpdateDeployment(ctx context.Context, req *interfaces.UpdateDeploymentRequest) (*interfaces.DeployResponse, error) {
	logger.Infof("GMI UpdateDeployment: endpoint=%s, image=%s, replicas=%v, env=%v, shmSize=%v",
		req.Endpoint, req.Image, req.Replicas, req.Env != nil, req.ShmSize)

	// Drain-first: if replicas are being reduced, drain excess workers first
	if req.Replicas != nil && *req.Replicas > 0 {
		current, err := p.GetApp(ctx, req.Endpoint)
		if err == nil && current != nil && int(current.Replicas) > *req.Replicas {
			excess := int(current.Replicas) - *req.Replicas
			pods, podErr := p.GetPods(ctx, req.Endpoint)
			if podErr == nil && len(pods) > 0 {
				toDrain := p.selectWorkersToDrain(pods, excess)
				if len(toDrain) > 0 {
					logger.Infof("GMI UpdateDeployment: drain-first: draining %d workers before replica reduction", len(toDrain))
					if drainErr := p.DrainWorkers(ctx, req.Endpoint, toDrain); drainErr != nil {
						logger.Warnf("GMI UpdateDeployment: drain before update failed (continuing): %v", drainErr)
					}
				}
			}
		}
	}

	// ShmSize not supported by BFF PATCH API
	if req.ShmSize != nil && *req.ShmSize != "" {
		logger.Warnf("GMI UpdateDeployment: shmSize update not supported by BFF PATCH API, ignoring shmSize=%s", *req.ShmSize)
	}

	// Build BFF PATCH request — only include fields that are being updated
	patch := map[string]any{}

	if req.Replicas != nil {
		patch["replicas"] = *req.Replicas
	}
	if req.Image != "" {
		patch["image"] = req.Image
	}

	// Build env using three-layer merge: defaults → existing → caller
	if req.Env != nil || req.Image != "" {
		defaultEnv := p.buildDefaultEnv(req.Endpoint)
		existingEnv := p.getExistingEnv(ctx, req.Endpoint)

		// Preserve existing non-default env vars
		for k, v := range existingEnv {
			if _, isDefault := defaultEnv[k]; !isDefault {
				defaultEnv[k] = v
			}
		}

		// Apply caller env overrides
		if req.Env != nil {
			for k, v := range *req.Env {
				defaultEnv[k] = v
			}
		}

		// Build envVars patch: use nil values to delete keys removed from merged set
		envPatch := make(map[string]any, len(defaultEnv))
		for k, v := range defaultEnv {
			envPatch[k] = v
		}
		// Mark keys present in existing but absent from merged as nil (JSON null = delete)
		for k := range existingEnv {
			if _, ok := defaultEnv[k]; !ok {
				envPatch[k] = nil
			}
		}
		patch["envVars"] = envPatch
	}

	// Pass registry credential update if provided
	if req.RegistryCredential != nil {
		patch["registryCredential"] = map[string]string{
			"registry": req.RegistryCredential.Registry,
			"username": req.RegistryCredential.Username,
			"password": req.RegistryCredential.Password,
		}
	}

	if len(patch) == 0 {
		return &interfaces.DeployResponse{
			Endpoint: req.Endpoint,
			Message:  "No fields to update",
		}, nil
	}

	path := "/api/v1/models/" + req.Endpoint
	if err := p.doRequestBFF(ctx, "PATCH", path, patch, nil); err != nil {
		return nil, fmt.Errorf("failed to update via BFF API: %w", err)
	}

	logger.Infof("GMI UpdateDeployment: endpoint=%s, SUCCESS", req.Endpoint)
	return &interfaces.DeployResponse{
		Endpoint:  req.Endpoint,
		Message:   "Successfully updated via BFF API",
		CreatedAt: time.Now().Format(time.RFC3339),
	}, nil
}

// ========================================
// SpecProvider (optional)
// ========================================

func (p *GMIDeploymentProvider) ListSpecs(ctx context.Context) ([]*interfaces.SpecInfo, error) {
	return nil, fmt.Errorf("GMI provider: ListSpecs - use database spec service instead")
}

func (p *GMIDeploymentProvider) GetSpec(ctx context.Context, specName string) (*interfaces.SpecInfo, error) {
	return nil, fmt.Errorf("GMI provider: GetSpec - use database spec service instead")
}

// ========================================
// LogProvider (optional) — BFF API
// ========================================

func (p *GMIDeploymentProvider) GetAppLogs(ctx context.Context, endpoint string, lines int, podName ...string) (string, error) {
	if len(podName) == 0 || podName[0] == "" {
		return "", fmt.Errorf("GMI provider: pod name is required for GetAppLogs via BFF")
	}

	// BFF returns SSE (text/event-stream) with log data.
	// Parse SSE events to extract logs.
	path := fmt.Sprintf("/api/v1/models/%s/workers/%s/logs?lines=%d", endpoint, podName[0], lines)
	return p.doRequestSSELogs(ctx, "GET", p.baseURL+path)
}

// ========================================
// PodProvider (optional) — BFF API
// ========================================

func (p *GMIDeploymentProvider) GetPods(ctx context.Context, endpoint string) ([]*interfaces.PodInfo, error) {
	if endpoint == "" {
		return p.getAllPods(ctx)
	}

	// BFF GET /models/:name includes pods in the response
	var resp bffModelResponse
	path := "/api/v1/models/" + endpoint
	if err := p.doRequestBFF(ctx, "GET", path, nil, &resp); err != nil {
		return nil, err
	}

	pods := make([]*interfaces.PodInfo, len(resp.Pods))
	for i := range resp.Pods {
		pods[i] = convertBFFPodToPodInfo(&resp.Pods[i])
		if pods[i].Labels == nil {
			pods[i].Labels = make(map[string]string)
		}
		pods[i].Labels["app"] = endpoint
	}
	return pods, nil
}

func (p *GMIDeploymentProvider) getAllPods(ctx context.Context) ([]*interfaces.PodInfo, error) {
	apps, err := p.ListApps(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list apps: %w", err)
	}

	var allPods []*interfaces.PodInfo
	for _, app := range apps {
		pods, err := p.GetPods(ctx, app.Name)
		if err != nil {
			logger.Warnf("GMI: failed to get pods for %s: %v", app.Name, err)
			continue
		}
		allPods = append(allPods, pods...)
	}
	return allPods, nil
}

func (p *GMIDeploymentProvider) DescribePod(ctx context.Context, endpoint string, podName string) (*interfaces.PodDetail, error) {
	// BFF doesn't have a dedicated pod describe endpoint.
	// Build a PodDetail from the pod info in GET /models/:name.
	pods, err := p.GetPods(ctx, endpoint)
	if err != nil {
		return nil, err
	}

	for _, pod := range pods {
		if pod.Name == podName {
			return &interfaces.PodDetail{
				PodInfo:    pod,
				Containers: []interfaces.ContainerInfo{},
				Conditions: []interfaces.PodCondition{},
				Events:     []interfaces.PodEvent{},
			}, nil
		}
	}
	return nil, fmt.Errorf("pod %s not found in endpoint %s", podName, endpoint)
}

func (p *GMIDeploymentProvider) GetPodYAML(ctx context.Context, endpoint string, podName string) (string, error) {
	return "", fmt.Errorf("GMI provider: GetPodYAML not supported")
}

func (p *GMIDeploymentProvider) IsPodTerminating(ctx context.Context, podName string) (bool, error) {
	if podName == "" {
		return false, nil
	}

	// 1. Check local workerStates cache for DeletionTimestamp
	if stateInterface, ok := p.workerStates.Load(podName); ok {
		state := stateInterface.(*gmiWorkerState)
		if state.DeletionTimestamp != "" {
			return true, nil
		}
	}

	// 2. Check Redis DrainingStore
	if p.drainingStore != nil {
		draining, err := p.drainingStore.IsDraining(ctx, gmiProviderName, podName)
		if err == nil && draining {
			return true, nil
		}
	}

	// 3. Fall back to BFF API check
	apps, err := p.ListApps(ctx)
	if err != nil {
		return false, nil
	}

	for _, app := range apps {
		if !strings.HasPrefix(podName, app.Name+"-") {
			continue
		}
		pods, err := p.GetPods(ctx, app.Name)
		if err != nil {
			continue
		}
		for _, pod := range pods {
			if pod.Name == podName {
				return pod.DeletionTimestamp != "" || pod.Status == "Terminating" || pod.Status == "Draining", nil
			}
		}
	}

	return false, nil
}

// ========================================
// StorageProvider (optional)
// ========================================

func (p *GMIDeploymentProvider) ListPVCs(ctx context.Context) ([]*interfaces.PVCInfo, error) {
	return nil, nil
}

// ========================================
// ConfigProvider (optional)
// ========================================

func (p *GMIDeploymentProvider) GetDefaultEnv(ctx context.Context) (map[string]string, error) {
	return map[string]string{
		"PLATFORM": "waverless",
	}, nil
}

// buildDefaultEnv builds the default RUNPOD/WAVERLESS environment variables for a given endpoint.
// These are automatically injected during Deploy and Update, not exposed to the frontend.
func (p *GMIDeploymentProvider) buildDefaultEnv(endpoint string) map[string]string {
	callbackURL := strings.TrimRight(p.gmiConfig.CallbackURL, "/")
	if callbackURL == "" {
		callbackURL = p.baseURL
	}

	serverAPIKey := p.cfg.Server.APIKey
	heartbeatMs := "10000"

	return map[string]string{
		"PLATFORM":                      "waverless",
		"RUNPOD_AI_API_KEY":             serverAPIKey,
		"RUNPOD_ENDPOINT_ID":            endpoint,
		"RUNPOD_PING_INTERVAL":          heartbeatMs,
		"RUNPOD_WEBHOOK_GET_JOB":        fmt.Sprintf("%s/v2/%s/job-take/$ID?", callbackURL, endpoint),
		"RUNPOD_WEBHOOK_PING":           fmt.Sprintf("%s/v2/%s/ping/$RUNPOD_POD_ID", callbackURL, endpoint),
		"RUNPOD_WEBHOOK_POST_OUTPUT":    fmt.Sprintf("%s/v2/%s/job-done/$RUNPOD_POD_ID/$ID?", callbackURL, endpoint),
		"RUNPOD_WEBHOOK_JOB_STREAM":     fmt.Sprintf("%s/v2/%s/job-stream/$RUNPOD_POD_ID/$ID?", callbackURL, endpoint),
		"WAVERLESS_ENDPOINT_ID":         endpoint,
		"WAVERLESS_PING_INTERVAL":       heartbeatMs,
		"WAVERLESS_WEBHOOK_GET_JOB":     fmt.Sprintf("%s/v2/%s/job-take/$ID?", callbackURL, endpoint),
		"WAVERLESS_WEBHOOK_PING":        fmt.Sprintf("%s/v2/%s/ping/$WAVERLESS_POD_ID", callbackURL, endpoint),
		"WAVERLESS_WEBHOOK_POST_OUTPUT": fmt.Sprintf("%s/v2/%s/job-done/$WAVERLESS_POD_ID/$ID?", callbackURL, endpoint),
		"WAVERLESS_WEBHOOK_POST_STREAM": fmt.Sprintf("%s/v2/%s/job-stream/$WAVERLESS_POD_ID/$ID?", callbackURL, endpoint),
	}
}

// getExistingEnv fetches the current env vars from the BFF.
// Returns nil if the model cannot be fetched (non-fatal).
func (p *GMIDeploymentProvider) getExistingEnv(ctx context.Context, endpoint string) map[string]string {
	var resp bffModelResponse
	path := "/api/v1/models/" + endpoint
	if err := p.doRequestBFF(ctx, "GET", path, nil, &resp); err != nil {
		return nil
	}
	return resp.EnvVars
}

// mergeEnv merges default env with user env. User-provided values take precedence.
func (p *GMIDeploymentProvider) mergeEnv(defaults map[string]string, userEnv map[string]string) map[string]string {
	merged := make(map[string]string, len(defaults)+len(userEnv))
	for k, v := range defaults {
		merged[k] = v
	}
	for k, v := range userEnv {
		merged[k] = v
	}
	return merged
}

// ========================================
// PreviewProvider (optional)
// ========================================

func (p *GMIDeploymentProvider) PreviewDeploymentYAML(ctx context.Context, req *interfaces.DeployRequest) (string, error) {
	gpuCount := req.GpuCount
	if gpuCount <= 0 {
		gpuCount = 1
	}

	yaml := fmt.Sprintf(`# GMI Deployment Preview
# Endpoint: %s
# Image: %s
# Replicas: %d
# GPU Count: %d
#
# This endpoint will be deployed via BFF API at:
# %s/api/v1/models
`,
		req.Endpoint, req.Image, req.Replicas, gpuCount, p.baseURL,
	)

	return yaml, nil
}

// ========================================
// WatchProvider (optional)
// ========================================

func (p *GMIDeploymentProvider) WatchReplicas(ctx context.Context, callback interfaces.ReplicaCallback) error {
	go func() {
		ticker := time.NewTicker(p.pollInterval)
		defer ticker.Stop()

		previousState := make(map[string]interfaces.ReplicaEvent)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				apps, err := p.ListApps(ctx)
				if err != nil {
					continue
				}

				for _, app := range apps {
					event := interfaces.ReplicaEvent{
						Name:              app.Name,
						DesiredReplicas:   int(app.Replicas),
						ReadyReplicas:     int(app.ReadyReplicas),
						AvailableReplicas: int(app.AvailableReplicas),
					}

					prev, exists := previousState[app.Name]
					if !exists ||
						prev.DesiredReplicas != event.DesiredReplicas ||
						prev.ReadyReplicas != event.ReadyReplicas ||
						prev.AvailableReplicas != event.AvailableReplicas {

						if exists {
							logger.Infof("GMI WatchReplicas: endpoint=%s, replica change: desired %d->%d, ready %d->%d",
								app.Name, prev.DesiredReplicas, event.DesiredReplicas, prev.ReadyReplicas, event.ReadyReplicas)
						}
						callback(event)
						previousState[app.Name] = event
					}
				}
			}
		}
	}()

	return nil
}

// ========================================
// Worker Status Sync (polling-based via BFF)
// ========================================

// WatchPodStatusChange registers a callback for worker status changes.
func (p *GMIDeploymentProvider) WatchPodStatusChange(ctx context.Context, callback WorkerStatusChangeCallback) error {
	if callback == nil {
		return fmt.Errorf("worker status change callback is nil")
	}

	p.workerStatusCallbacksLock.Lock()
	callbackID := atomic.AddUint64(&p.nextCallbackID, 1)
	p.workerStatusCallbacks[callbackID] = callback
	p.workerStatusCallbacksLock.Unlock()

	logger.Infof("GMI: registered worker status change callback (ID: %d)", callbackID)

	p.watcherRunning.Do(func() {
		p.watcherCtx, p.watcherCancel = context.WithCancel(ctx)
		logger.Infof("GMI: starting worker status watcher (poll interval: %v)", p.pollInterval)
		go p.runWorkerStatusWatcher(p.watcherCtx)
	})

	go func() {
		<-ctx.Done()
		p.workerStatusCallbacksLock.Lock()
		delete(p.workerStatusCallbacks, callbackID)
		p.workerStatusCallbacksLock.Unlock()
	}()

	return nil
}

// WatchPodDelete registers a callback for worker deletions.
func (p *GMIDeploymentProvider) WatchPodDelete(ctx context.Context, callback WorkerDeleteCallback) error {
	if callback == nil {
		return fmt.Errorf("worker delete callback is nil")
	}

	p.workerDeleteCallbacksLock.Lock()
	callbackID := atomic.AddUint64(&p.nextCallbackID, 1)
	p.workerDeleteCallbacks[callbackID] = callback
	p.workerDeleteCallbacksLock.Unlock()

	logger.Infof("GMI: registered worker delete callback (ID: %d)", callbackID)

	p.watcherRunning.Do(func() {
		p.watcherCtx, p.watcherCancel = context.WithCancel(ctx)
		logger.Infof("GMI: starting worker status watcher (poll interval: %v)", p.pollInterval)
		go p.runWorkerStatusWatcher(p.watcherCtx)
	})

	go func() {
		<-ctx.Done()
		p.workerDeleteCallbacksLock.Lock()
		delete(p.workerDeleteCallbacks, callbackID)
		p.workerDeleteCallbacksLock.Unlock()
	}()

	return nil
}

// StopWatcher stops the worker status watcher
func (p *GMIDeploymentProvider) StopWatcher() {
	if p.watcherCancel != nil {
		p.watcherCancel()
	}
}

// runWorkerStatusWatcher runs the polling loop
func (p *GMIDeploymentProvider) runWorkerStatusWatcher(ctx context.Context) {
	ticker := time.NewTicker(p.pollInterval)
	defer ticker.Stop()

	logger.Infof("GMI: worker status watcher started")

	for {
		select {
		case <-ctx.Done():
			logger.Infof("GMI: worker status watcher stopped")
			return
		case <-ticker.C:
			p.pollWorkerStates(ctx)
		}
	}
}

// pollWorkerStates polls BFF for all models and their pods.
// BFF List response includes pods, so a single API call is sufficient.
func (p *GMIDeploymentProvider) pollWorkerStates(ctx context.Context) {
	var models []bffModelResponse
	if err := p.doRequestBFF(ctx, "GET", "/api/v1/models", nil, &models); err != nil {
		logger.Warnf("GMI: failed to poll models: %v", err)
		return
	}

	currentWorkerIDs := make(map[string]bool)
	totalPods := 0

	for _, model := range models {
		totalPods += len(model.Pods)
		for i := range model.Pods {
			pod := &model.Pods[i]
			workerID := pod.PodName
			currentWorkerIDs[workerID] = true
			p.processPodStateChange(model.Name, workerID, pod)
		}
	}

	if len(models) > 0 || totalPods > 0 {
		logger.Infof("GMI poll: found %d models, %d pods", len(models), totalPods)
	} else {
		logger.Debugf("GMI poll: no models or pods found")
	}

	p.detectDeletedWorkers(currentWorkerIDs)
}

// processPodStateChange detects pod state changes and triggers callbacks
func (p *GMIDeploymentProvider) processPodStateChange(endpoint, workerID string, pod *bffPodStatus) {
	prevInterface, exists := p.workerStates.Load(workerID)

	podInfo := convertBFFPodToPodInfo(pod)
	podInfo.Labels = map[string]string{"app": endpoint}

	currentState := &gmiWorkerState{
		ID:                workerID,
		Endpoint:          endpoint,
		Status:            pod.Phase,
		CreatedAt:         pod.CreatedAt,
		StartedAt:         pod.StartedAt,
		ReadyAt:           pod.ReadyAt,
		DeletionTimestamp: pod.DeletionTimestamp,
		Reason:            pod.Reason,
		Message:           pod.Message,
		RestartCount:      pod.RestartCount,
	}

	if !exists {
		p.workerStates.Store(workerID, currentState)
		logger.Infof("GMI: new pod detected: %s (endpoint: %s, phase: %s)", workerID, endpoint, pod.Phase)
		p.notifyWorkerStatusChange(workerID, endpoint, podInfo)
		return
	}

	prev := prevInterface.(*gmiWorkerState)
	if prev.Status != currentState.Status || prev.DeletionTimestamp != currentState.DeletionTimestamp {
		p.workerStates.Store(workerID, currentState)
		logger.Infof("GMI: pod state changed: %s (endpoint: %s, %s -> %s, deletion: %s -> %s)",
			workerID, endpoint, prev.Status, currentState.Status, prev.DeletionTimestamp, currentState.DeletionTimestamp)
		p.notifyWorkerStatusChange(workerID, endpoint, podInfo)
	}
}

// detectDeletedWorkers finds workers that disappeared and triggers delete callbacks
func (p *GMIDeploymentProvider) detectDeletedWorkers(currentWorkerIDs map[string]bool) {
	p.workerStates.Range(func(key, value any) bool {
		workerID := key.(string)
		if !currentWorkerIDs[workerID] {
			state := value.(*gmiWorkerState)
			logger.Infof("GMI: worker deleted: %s (endpoint: %s)", workerID, state.Endpoint)
			p.workerStates.Delete(workerID)

			var deletedAt *time.Time
			if state.DeletionTimestamp != "" {
				if t, err := time.Parse(time.RFC3339, state.DeletionTimestamp); err == nil {
					deletedAt = &t
				}
			}
			p.notifyWorkerDelete(workerID, state.Endpoint, deletedAt)
		}
		return true
	})
}

// notifyWorkerStatusChange triggers all registered status change callbacks
func (p *GMIDeploymentProvider) notifyWorkerStatusChange(workerID, endpoint string, info *interfaces.PodInfo) {
	p.workerStatusCallbacksLock.RLock()
	defer p.workerStatusCallbacksLock.RUnlock()

	for _, cb := range p.workerStatusCallbacks {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Errorf("GMI: panic in worker status callback: %v", r)
				}
			}()
			cb(workerID, endpoint, info)
		}()
	}
}

// notifyWorkerDelete triggers all registered delete callbacks
func (p *GMIDeploymentProvider) notifyWorkerDelete(workerID, endpoint string, deletedAt *time.Time) {
	p.workerDeleteCallbacksLock.RLock()
	defer p.workerDeleteCallbacksLock.RUnlock()

	for _, cb := range p.workerDeleteCallbacks {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Errorf("GMI: panic in worker delete callback: %v", r)
				}
			}()
			cb(workerID, endpoint, deletedAt)
		}()
	}
}

// GetLifecycle returns the GMI lifecycle manager
func (p *GMIDeploymentProvider) GetLifecycle() *GMIProviderLifecycle {
	return NewGMIProviderLifecycle(p)
}

// ========================================
// Drain + DrainingStore (Novita parity)
// ========================================

// gmiProviderName is used as the provider identifier in draining store
const gmiProviderName = "gmi"

// SetRedisClient sets the Redis client for draining workers tracking
func (p *GMIDeploymentProvider) SetRedisClient(client *redis.Client) {
	p.redisClient = client
	if client != nil {
		p.drainingStore = redisstore.NewDrainingStore(client)
		logger.Infof("GMI: Redis draining store initialized")
	}
}

// DrainWorkers drains specified workers via BFF drain API and marks them in Redis
func (p *GMIDeploymentProvider) DrainWorkers(ctx context.Context, endpoint string, workerIDs []string) error {
	if len(workerIDs) == 0 {
		return nil
	}

	logger.Infof("GMI DrainWorkers: endpoint=%s, workers=%v", endpoint, workerIDs)

	// 1. Call BFF POST /models/:name/drain
	path := "/api/v1/models/" + endpoint + "/drain"
	if err := p.doRequestBFF(ctx, "POST", path, map[string]any{"workerIds": workerIDs}, nil); err != nil {
		return fmt.Errorf("failed to drain workers via BFF API: %w", err)
	}

	// 2. Mark each worker as draining in Redis
	for _, wid := range workerIDs {
		if err := p.MarkWorkerDraining(ctx, wid); err != nil {
			logger.Warnf("GMI DrainWorkers: failed to mark worker %s as draining in Redis: %v", wid, err)
		}
	}

	logger.Infof("GMI DrainWorkers: endpoint=%s, drained %d workers", endpoint, len(workerIDs))
	return nil
}

// TerminateWorker terminates a specific worker by draining it.
// Implements interfaces.WorkerTerminator.
func (p *GMIDeploymentProvider) TerminateWorker(ctx context.Context, endpoint, workerID, reason string) error {
	logger.Infof("GMI TerminateWorker: endpoint=%s, worker=%s, reason=%s", endpoint, workerID, reason)
	return p.DrainWorkers(ctx, endpoint, []string{workerID})
}

// selectWorkersToDrain selects workers to drain for scale-down.
// Priority: skip already-terminating → non-Ready → non-Running → healthy Running.
func (p *GMIDeploymentProvider) selectWorkersToDrain(pods []*interfaces.PodInfo, count int) []string {
	if count <= 0 || len(pods) == 0 {
		return nil
	}

	// Filter out already-terminating pods
	type candidate struct {
		name    string
		ready   bool
		running bool
	}
	var candidates []candidate
	for _, pod := range pods {
		if pod.DeletionTimestamp != "" || pod.Status == "Terminating" || pod.Status == "Draining" {
			continue
		}
		candidates = append(candidates, candidate{
			name:    pod.Name,
			ready:   pod.Phase == "Running" && pod.Status == "Running",
			running: pod.Phase == "Running",
		})
	}

	if count >= len(candidates) {
		result := make([]string, len(candidates))
		for i, c := range candidates {
			result[i] = c.name
		}
		return result
	}

	// Separate into priority categories
	var nonReady, notRunning, healthyRunning []string
	for _, c := range candidates {
		if !c.ready {
			nonReady = append(nonReady, c.name)
		} else if !c.running {
			notRunning = append(notRunning, c.name)
		} else {
			healthyRunning = append(healthyRunning, c.name)
		}
	}

	// Select in priority order
	selected := make([]string, 0, count)
	for _, group := range [][]string{nonReady, notRunning, healthyRunning} {
		for _, name := range group {
			if len(selected) >= count {
				return selected
			}
			selected = append(selected, name)
		}
	}
	return selected
}

// MarkWorkerDraining marks a worker as draining in Redis with TTL
func (p *GMIDeploymentProvider) MarkWorkerDraining(ctx context.Context, workerID string) error {
	if p.drainingStore == nil {
		logger.Warnf("GMI: Redis not configured, cannot mark worker %s as draining", workerID)
		return nil
	}
	return p.drainingStore.MarkDraining(ctx, gmiProviderName, workerID)
}

// ClearDrainingWorker removes a worker from the draining list in Redis
func (p *GMIDeploymentProvider) ClearDrainingWorker(ctx context.Context, workerID string) error {
	if p.drainingStore == nil {
		return nil
	}
	return p.drainingStore.ClearDraining(ctx, gmiProviderName, workerID)
}

// GetDrainingWorkers returns currently draining worker IDs from Redis
func (p *GMIDeploymentProvider) GetDrainingWorkers(ctx context.Context) []string {
	if p.drainingStore == nil {
		return nil
	}
	return p.drainingStore.GetDrainingWorkers(ctx, gmiProviderName)
}
