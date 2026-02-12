package gmi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"waverless/pkg/config"
	"waverless/pkg/interfaces"
	"waverless/pkg/logger"
)

// GMIDeploymentProvider implements the DeploymentProvider interface for GMI.
// It calls the gmiless API (/api/v1/endpoints) for deployment operations.
type GMIDeploymentProvider struct {
	baseURL      string
	token        string
	client       *http.Client
	cfg          *config.Config
	gmiConfig    *config.GMIConfig
	pollInterval time.Duration

	// Endpoint name → ID cache
	endpointCache sync.Map

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
// CoreDeploymentProvider (required)
// ========================================

func (p *GMIDeploymentProvider) Deploy(ctx context.Context, req *interfaces.DeployRequest) (*interfaces.DeployResponse, error) {
	logger.Infof("GMI Deploy: endpoint=%s, image=%s, replicas=%d, spec=%s, gpuCount=%d",
		req.Endpoint, req.Image, req.Replicas, req.SpecName, req.GpuCount)

	computeType := "GPU"
	endpointType := "QB"

	// Build template with default env vars merged
	mergedEnv := p.mergeEnv(p.buildDefaultEnv(req.Endpoint), req.Env)

	// Pass registry credential as environment variables for private image pulling
	if req.RegistryCredential != nil {
		if req.RegistryCredential.Registry != "" {
			mergedEnv["REGISTRY_SERVER"] = req.RegistryCredential.Registry
		}
		if req.RegistryCredential.Username != "" {
			mergedEnv["REGISTRY_USERNAME"] = req.RegistryCredential.Username
		}
		if req.RegistryCredential.Password != "" {
			mergedEnv["REGISTRY_PASSWORD"] = req.RegistryCredential.Password
		}
	}

	template := &gmiTemplateData{
		ImageName: &req.Image,
		Env:       mergedEnv,
	}
	if req.ShmSize != "" {
		template.ShmSize = &req.ShmSize
	}

	// Build request matching gmiless EndpointRequest
	defaultRegions := []string{"us-west1"}
	gmiReq := &gmiEndpointRequest{
		Name:          &req.Endpoint,
		Replicas:      &req.Replicas,
		GpuCount:      &req.GpuCount,
		ComputeType:   &computeType,
		Type:          &endpointType,
		Template:      template,
		WorkersMin:    &req.Replicas,
		WorkersMax:    &req.Replicas,
		DataCenterIds: &defaultRegions,
	}

	// Map spec name to GPU type ID
	if req.SpecName != "" {
		gpuType := specNameToGPUType(req.SpecName)
		gmiReq.GpuTypeIds = &[]string{gpuType}
	}

	// Convert TaskTimeout (seconds) to ExecutionTimeoutMs (milliseconds)
	if req.TaskTimeout > 0 {
		timeoutMs := int64(req.TaskTimeout) * 1000
		gmiReq.ExecutionTimeoutMs = &timeoutMs
	}

	url := p.baseURL + "/api/v1/endpoints"
	body, err := p.doRequest(ctx, "POST", url, gmiReq)
	if err != nil {
		return nil, fmt.Errorf("failed to deploy via GMI API: %w", err)
	}

	// Parse response to get endpoint ID
	var resp gmiEndpointResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		logger.Warnf("GMI Deploy: failed to parse response: %v, body=%s", err, string(body))
	} else if resp.Id != "" {
		p.endpointCache.Store(req.Endpoint, resp.Id)
	}

	logger.Infof("GMI Deploy: endpoint=%s, id=%s, SUCCESS", req.Endpoint, resp.Id)

	return &interfaces.DeployResponse{
		Endpoint:  req.Endpoint,
		Message:   "Successfully deployed via GMI API",
		CreatedAt: time.Now().Format(time.RFC3339),
	}, nil
}

func (p *GMIDeploymentProvider) GetApp(ctx context.Context, endpoint string) (*interfaces.AppInfo, error) {
	endpointID, err := p.getEndpointID(ctx, endpoint)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/api/v1/endpoints/%s", p.baseURL, endpointID)
	body, err := p.doRequest(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	var resp gmiEndpointResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	p.endpointCache.Store(resp.Name, resp.Id)
	return convertToAppInfo(&resp), nil
}

func (p *GMIDeploymentProvider) ListApps(ctx context.Context) ([]*interfaces.AppInfo, error) {
	url := p.baseURL + "/api/v1/endpoints"
	body, err := p.doRequest(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	var respList []gmiEndpointResponse
	if err := json.Unmarshal(body, &respList); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	apps := make([]*interfaces.AppInfo, len(respList))
	for i, resp := range respList {
		if resp.Id != "" && resp.Name != "" {
			p.endpointCache.Store(resp.Name, resp.Id)
		}
		apps[i] = convertToAppInfo(&resp)
	}

	return apps, nil
}

func (p *GMIDeploymentProvider) DeleteApp(ctx context.Context, endpoint string) error {
	logger.Infof("GMI DeleteApp: endpoint=%s", endpoint)

	endpointID, err := p.getEndpointID(ctx, endpoint)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/api/v1/endpoints/%s", p.baseURL, endpointID)
	_, err = p.doRequest(ctx, "DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to delete via GMI API: %w", err)
	}

	p.endpointCache.Delete(endpoint)
	logger.Infof("GMI DeleteApp: endpoint=%s, SUCCESS", endpoint)
	return nil
}

func (p *GMIDeploymentProvider) ScaleApp(ctx context.Context, endpoint string, replicas int) error {
	logger.Infof("GMI ScaleApp: endpoint=%s, target replicas=%d", endpoint, replicas)

	endpointID, err := p.getEndpointID(ctx, endpoint)
	if err != nil {
		return err
	}

	// Only send replicas fields, no template or env changes
	gmiReq := &gmiEndpointRequest{
		Replicas:   &replicas,
		WorkersMin: &replicas,
		WorkersMax: &replicas,
	}

	url := fmt.Sprintf("%s/api/v1/endpoints/%s", p.baseURL, endpointID)
	_, err = p.doRequest(ctx, "PATCH", url, gmiReq)
	if err != nil {
		return fmt.Errorf("failed to scale via GMI API: %w", err)
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
	logger.Infof("GMI UpdateDeployment: endpoint=%s, image=%s, replicas=%v, env=%v, shmSize=%v, taskTimeout=%v",
		req.Endpoint, req.Image, req.Replicas, req.Env != nil, req.ShmSize, req.TaskTimeout)

	endpointID, err := p.getEndpointID(ctx, req.Endpoint)
	if err != nil {
		return nil, err
	}

	gmiReq := &gmiEndpointRequest{}

	if req.Replicas != nil {
		gmiReq.Replicas = req.Replicas
		gmiReq.WorkersMin = req.Replicas
		gmiReq.WorkersMax = req.Replicas
	}

	// Only build and send template when template-related fields are being updated
	needTemplate := req.Image != "" || req.Env != nil || (req.ShmSize != nil && *req.ShmSize != "")
	if needTemplate {
		template := &gmiTemplateData{}
		if req.Image != "" {
			template.ImageName = &req.Image
		}
		if req.ShmSize != nil && *req.ShmSize != "" {
			template.ShmSize = req.ShmSize
		}

		// Build env: start with defaults, then merge existing user env vars, then apply new env
		defaultEnv := p.buildDefaultEnv(req.Endpoint)
		existingEnv := p.getExistingEnv(ctx, endpointID)

		// Preserve existing non-default env vars (user-set vars from previous deploys)
		if existingEnv != nil {
			for k, v := range existingEnv {
				if _, isDefault := defaultEnv[k]; !isDefault {
					defaultEnv[k] = v
				}
			}
		}

		// If user explicitly provided new env, override with it
		if req.Env != nil {
			for k, v := range *req.Env {
				defaultEnv[k] = v
			}
		}

		template.Env = defaultEnv
		gmiReq.Template = template
	}

	if req.TaskTimeout != nil && *req.TaskTimeout > 0 {
		timeoutMs := int64(*req.TaskTimeout) * 1000
		gmiReq.ExecutionTimeoutMs = &timeoutMs
	}

	// Log the full request for debugging
	if reqJSON, err := json.Marshal(gmiReq); err == nil {
		logger.Infof("GMI UpdateDeployment: endpoint=%s, endpointID=%s, request=%s", req.Endpoint, endpointID, string(reqJSON))
	}

	url := fmt.Sprintf("%s/api/v1/endpoints/%s", p.baseURL, endpointID)
	body, err := p.doRequest(ctx, "PATCH", url, gmiReq)
	if err != nil {
		return nil, fmt.Errorf("failed to update via GMI API: %w", err)
	}

	logger.Infof("GMI UpdateDeployment: endpoint=%s, SUCCESS, response=%s", req.Endpoint, string(body))

	return &interfaces.DeployResponse{
		Endpoint:  req.Endpoint,
		Message:   "Successfully updated via GMI API",
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
// LogProvider (optional)
// ========================================

func (p *GMIDeploymentProvider) GetAppLogs(ctx context.Context, endpoint string, lines int, podName ...string) (string, error) {
	endpointID, err := p.getEndpointID(ctx, endpoint)
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("%s/api/v1/endpoints/%s/logs?lines=%d", p.baseURL, endpointID, lines)
	if len(podName) > 0 && podName[0] != "" {
		url += "&pod_name=" + podName[0]
	}

	body, err := p.doRequest(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}

	return string(body), nil
}

// ========================================
// PodProvider (optional)
// ========================================

func (p *GMIDeploymentProvider) GetPods(ctx context.Context, endpoint string) ([]*interfaces.PodInfo, error) {
	if endpoint == "" {
		return p.getAllPods(ctx)
	}

	endpointID, err := p.getEndpointID(ctx, endpoint)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/api/v1/endpoints/%s/workers", p.baseURL, endpointID)
	body, err := p.doRequest(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	// gmiless may return workers as direct array or as part of endpoint response
	var podList []gmiPodInfo
	if err := json.Unmarshal(body, &podList); err != nil {
		// Try parsing as endpoint response with workers
		var endpointResp gmiEndpointResponse
		if err2 := json.Unmarshal(body, &endpointResp); err2 == nil {
			pods := make([]*interfaces.PodInfo, len(endpointResp.Workers))
			for i, w := range endpointResp.Workers {
				pods[i] = convertWorkerToPodInfo(&w, endpoint)
			}
			return pods, nil
		}
		return nil, fmt.Errorf("failed to parse workers response: %w", err)
	}

	pods := make([]*interfaces.PodInfo, len(podList))
	for i := range podList {
		pods[i] = convertPodInfoFromGMI(&podList[i])
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
		for _, pod := range pods {
			if pod.Labels == nil {
				pod.Labels = make(map[string]string)
			}
			if pod.Labels["app"] == "" {
				pod.Labels["app"] = app.Name
			}
		}
		allPods = append(allPods, pods...)
	}

	return allPods, nil
}

func (p *GMIDeploymentProvider) DescribePod(ctx context.Context, endpoint string, podName string) (*interfaces.PodDetail, error) {
	endpointID, err := p.getEndpointID(ctx, endpoint)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/api/v1/endpoints/%s/workers/%s/describe", p.baseURL, endpointID, podName)
	body, err := p.doRequest(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	var pod gmiPodInfo
	if err := json.Unmarshal(body, &pod); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return convertPodDetailFromGMI(&pod), nil
}

func (p *GMIDeploymentProvider) GetPodYAML(ctx context.Context, endpoint string, podName string) (string, error) {
	return "", fmt.Errorf("GMI provider: GetPodYAML not supported")
}

func (p *GMIDeploymentProvider) IsPodTerminating(ctx context.Context, podName string) (bool, error) {
	if podName == "" {
		return false, nil
	}

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
				return pod.DeletionTimestamp != "" || pod.Status == "Terminating", nil
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

// getExistingEnv fetches the current env vars from the remote GMI endpoint.
// Returns nil if the endpoint cannot be fetched (non-fatal).
func (p *GMIDeploymentProvider) getExistingEnv(ctx context.Context, endpointID string) map[string]string {
	url := fmt.Sprintf("%s/api/v1/endpoints/%s", p.baseURL, endpointID)
	body, err := p.doRequest(ctx, "GET", url, nil)
	if err != nil {
		return nil
	}
	var resp gmiEndpointResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil
	}
	return resp.Env
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
# This endpoint will be deployed via GMI (gmiless) API at:
# %s/api/v1/endpoints
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
// Worker Status Sync (polling-based)
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

// pollWorkerStates polls gmiless for all endpoints and workers
func (p *GMIDeploymentProvider) pollWorkerStates(ctx context.Context) {
	url := p.baseURL + "/api/v1/endpoints"
	body, err := p.doRequest(ctx, "GET", url, nil)
	if err != nil {
		logger.Warnf("GMI: failed to poll endpoints: %v", err)
		return
	}

	var endpoints []gmiEndpointResponse
	if err := json.Unmarshal(body, &endpoints); err != nil {
		logger.Warnf("GMI: failed to parse endpoints response: %v", err)
		return
	}

	currentWorkerIDs := make(map[string]bool)
	totalWorkers := 0

	for _, ep := range endpoints {
		if ep.Id != "" && ep.Name != "" {
			p.endpointCache.Store(ep.Name, ep.Id)
		}

		// The list endpoints API does not include workers inline,
		// so we need to fetch workers separately for each endpoint.
		workers := ep.Workers
		if len(workers) == 0 && ep.Id != "" {
			workersURL := p.baseURL + "/api/v1/endpoints/" + ep.Id + "/workers"
			wBody, wErr := p.doRequest(ctx, "GET", workersURL, nil)
			if wErr != nil {
				logger.Warnf("GMI: failed to poll workers for endpoint %s: %v", ep.Name, wErr)
			} else {
				var fetchedWorkers []gmiWorkerResponse
				if jErr := json.Unmarshal(wBody, &fetchedWorkers); jErr != nil {
					logger.Warnf("GMI: failed to parse workers response for endpoint %s: %v", ep.Name, jErr)
				} else {
					workers = fetchedWorkers
				}
			}
		}

		totalWorkers += len(workers)
		for i := range workers {
			worker := &workers[i]
			workerID := worker.Id
			if workerID == "" {
				workerID = worker.Name
			}
			currentWorkerIDs[workerID] = true
			p.processWorkerStateChange(ep.Name, workerID, worker)
		}
	}

	if len(endpoints) > 0 || totalWorkers > 0 {
		logger.Infof("GMI poll: found %d endpoints, %d workers", len(endpoints), totalWorkers)
	} else {
		logger.Debugf("GMI poll: no endpoints or workers found")
	}

	p.detectDeletedWorkers(currentWorkerIDs)
}

// processWorkerStateChange detects worker state changes and triggers callbacks
func (p *GMIDeploymentProvider) processWorkerStateChange(endpoint, workerID string, worker *gmiWorkerResponse) {
	prevInterface, exists := p.workerStates.Load(workerID)

	podInfo := convertWorkerToPodInfo(worker, endpoint)

	currentState := &gmiWorkerState{
		ID:        workerID,
		Endpoint:  endpoint,
		Status:    worker.DesiredStatus,
		CreatedAt: worker.LastStartedAt,
		StartedAt: worker.LastStartedAt,
	}

	if !exists {
		p.workerStates.Store(workerID, currentState)
		logger.Infof("GMI: new worker detected: %s (endpoint: %s, status: %s)", workerID, endpoint, worker.DesiredStatus)
		p.notifyWorkerStatusChange(workerID, endpoint, podInfo)
		return
	}

	prev := prevInterface.(*gmiWorkerState)
	if prev.Status != currentState.Status {
		p.workerStates.Store(workerID, currentState)
		logger.Infof("GMI: worker state changed: %s (endpoint: %s, %s -> %s)", workerID, endpoint, prev.Status, currentState.Status)
		p.notifyWorkerStatusChange(workerID, endpoint, podInfo)
	}
}

// detectDeletedWorkers finds workers that disappeared and triggers delete callbacks
func (p *GMIDeploymentProvider) detectDeletedWorkers(currentWorkerIDs map[string]bool) {
	p.workerStates.Range(func(key, value interface{}) bool {
		workerID := key.(string)
		if !currentWorkerIDs[workerID] {
			state := value.(*gmiWorkerState)
			logger.Infof("GMI: worker deleted: %s (endpoint: %s)", workerID, state.Endpoint)
			p.workerStates.Delete(workerID)
			p.notifyWorkerDelete(workerID, state.Endpoint)
		}
		return true
	})
}

// notifyWorkerStatusChange triggers all registered status change callbacks
func (p *GMIDeploymentProvider) notifyWorkerStatusChange(workerID, endpoint string, info *interfaces.PodInfo) {
	p.workerStatusCallbacksLock.RLock()
	defer p.workerStatusCallbacksLock.RUnlock()

	for _, cb := range p.workerStatusCallbacks {
		cb := cb
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
func (p *GMIDeploymentProvider) notifyWorkerDelete(workerID, endpoint string) {
	p.workerDeleteCallbacksLock.RLock()
	defer p.workerDeleteCallbacksLock.RUnlock()

	for _, cb := range p.workerDeleteCallbacks {
		cb := cb
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Errorf("GMI: panic in worker delete callback: %v", r)
				}
			}()
			cb(workerID, endpoint)
		}()
	}
}

// GetLifecycle returns the GMI lifecycle manager
func (p *GMIDeploymentProvider) GetLifecycle() *GMIProviderLifecycle {
	return NewGMIProviderLifecycle(p)
}
