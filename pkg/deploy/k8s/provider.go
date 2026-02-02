package k8s

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"waverless/pkg/config"
	"waverless/pkg/interfaces"
)

// K8sDeploymentProvider K8s deployment provider implementation
type K8sDeploymentProvider struct {
	manager *Manager
}

// NewK8sDeploymentProvider creates a K8s deployment provider
func NewK8sDeploymentProvider(cfg *config.Config) (interfaces.DeploymentProvider, error) {
	if !cfg.K8s.Enabled {
		return nil, fmt.Errorf("k8s is not enabled in config")
	}

	// Build globalEnv with defaults, then merge config overrides
	globalEnv := map[string]string{
		// RunPod compatible environment variables (for runpod-python SDK)
		"RUNPOD_ENDPOINT_ID":         "{{.Endpoint}}",
		"RUNPOD_PING_INTERVAL":       "10000",
		"RUNPOD_WEBHOOK_GET_JOB":     "http://waverless-svc/v2/{{.Endpoint}}/job-take/$ID?",
		"RUNPOD_WEBHOOK_PING":        "http://waverless-svc/v2/{{.Endpoint}}/ping/$RUNPOD_POD_ID",
		"RUNPOD_WEBHOOK_POST_OUTPUT": "http://waverless-svc/v2/{{.Endpoint}}/job-done/$RUNPOD_POD_ID/$ID?",
		"RUNPOD_WEBHOOK_POST_STREAM": "http://waverless-svc/v2/{{.Endpoint}}/job-stream/$RUNPOD_POD_ID/$ID?",
		// Waverless native environment variables (for wavespeed-python SDK)
		"WAVERLESS_ENDPOINT_ID":         "{{.Endpoint}}",
		"WAVERLESS_PING_INTERVAL":       "10000",
		"WAVERLESS_WEBHOOK_GET_JOB":     "http://waverless-svc/v2/{{.Endpoint}}/job-take/$ID?",
		"WAVERLESS_WEBHOOK_PING":        "http://waverless-svc/v2/{{.Endpoint}}/ping/$WAVERLESS_POD_ID",
		"WAVERLESS_WEBHOOK_POST_OUTPUT": "http://waverless-svc/v2/{{.Endpoint}}/job-done/$WAVERLESS_POD_ID/$ID?",
		"WAVERLESS_WEBHOOK_POST_STREAM": "http://waverless-svc/v2/{{.Endpoint}}/job-stream/$WAVERLESS_POD_ID/$ID?",
	}

	// Merge config overrides (config takes precedence)
	for k, v := range cfg.K8s.GlobalEnv {
		globalEnv[k] = v
	}

	// Inject server api_key
	if cfg.Server.APIKey != "" {
		globalEnv["WAVERLESS_API_KEY"] = cfg.Server.APIKey
		globalEnv["RUNPOD_AI_API_KEY"] = cfg.Server.APIKey
		globalEnv["RUNPOD_API_KEY"] = cfg.Server.APIKey
	}

	manager, err := NewManager(cfg.K8s.Namespace, cfg.K8s.Platform, cfg.K8s.ConfigDir, globalEnv)
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s manager: %w", err)
	}

	return &K8sDeploymentProvider{
		manager: manager,
	}, nil
}

// Deploy deploys an application
func (p *K8sDeploymentProvider) Deploy(ctx context.Context, req *interfaces.DeployRequest) (*interfaces.DeployResponse, error) {
	// Convert to DeployAppRequest
	k8sReq := &DeployAppRequest{
		Endpoint:     req.Endpoint,
		SpecName:     req.SpecName,
		Image:        req.Image,
		Replicas:     req.Replicas,
		GpuCount:     req.GpuCount,
		TaskTimeout:  req.TaskTimeout,
		Env:          req.Env,
		VolumeMounts: req.VolumeMounts,
		ShmSize:      req.ShmSize,
	}
	if req.RegistryCredential != nil {
		k8sReq.RegistryCredential = &RegistryCredential{
			Registry: req.RegistryCredential.Registry,
			Username: req.RegistryCredential.Username,
			Password: req.RegistryCredential.Password,
		}
	}

	if err := p.manager.DeployApp(ctx, k8sReq); err != nil {
		return nil, err
	}

	return &interfaces.DeployResponse{
		Endpoint:  req.Endpoint,
		Message:   "Application deployed successfully",
		CreatedAt: "", // TODO: Get creation time
	}, nil
}

// GetApp gets application details
func (p *K8sDeploymentProvider) GetApp(ctx context.Context, endpoint string) (*interfaces.AppInfo, error) {
	app, err := p.manager.GetApp(ctx, endpoint)
	if err != nil {
		return nil, err
	}

	// Convert to interfaces.AppInfo
	return &interfaces.AppInfo{
		Name:              app.Name,
		Namespace:         app.Namespace,
		Type:              app.Type,
		Status:            app.Status,
		Replicas:          app.Replicas,
		ReadyReplicas:     app.ReadyReplicas,
		AvailableReplicas: app.AvailableReplicas,
		Image:             app.Image,
		Labels:            app.Labels,
		CreatedAt:         app.CreatedAt,
		ShmSize:           app.ShmSize,
		VolumeMounts:      app.VolumeMounts,
	}, nil
}

// ListApps lists all applications
func (p *K8sDeploymentProvider) ListApps(ctx context.Context) ([]*interfaces.AppInfo, error) {
	apps, err := p.manager.ListApps(ctx)
	if err != nil {
		return nil, err
	}

	// Convert to interfaces.AppInfo list
	result := make([]*interfaces.AppInfo, 0, len(apps))
	for _, app := range apps {
		result = append(result, &interfaces.AppInfo{
			Name:              app.Name,
			Namespace:         app.Namespace,
			Type:              app.Type,
			Status:            app.Status,
			Replicas:          app.Replicas,
			ReadyReplicas:     app.ReadyReplicas,
			AvailableReplicas: app.AvailableReplicas,
			Image:             app.Image,
			Labels:            app.Labels,
			CreatedAt:         app.CreatedAt,
			ShmSize:           app.ShmSize,
			VolumeMounts:      app.VolumeMounts,
		})
	}

	return result, nil
}

// DeleteApp deletes an application
func (p *K8sDeploymentProvider) DeleteApp(ctx context.Context, endpoint string) error {
	return p.manager.DeleteApp(ctx, endpoint)
}

// GetAppLogs gets application logs
func (p *K8sDeploymentProvider) GetAppLogs(ctx context.Context, endpoint string, lines int, podName ...string) (string, error) {
	return p.manager.GetAppLogs(ctx, endpoint, int64(lines), podName...)
}

// ScaleApp scales an application
func (p *K8sDeploymentProvider) ScaleApp(ctx context.Context, endpoint string, replicas int) error {
	return p.manager.ScaleDeployment(ctx, endpoint, replicas)
}

// GetAppStatus gets application status
func (p *K8sDeploymentProvider) GetAppStatus(ctx context.Context, endpoint string) (*interfaces.AppStatus, error) {
	app, err := p.manager.GetApp(ctx, endpoint)
	if err != nil {
		return nil, err
	}

	return &interfaces.AppStatus{
		Endpoint:          app.Name,
		Status:            app.Status,
		ReadyReplicas:     app.ReadyReplicas,
		AvailableReplicas: app.AvailableReplicas,
		TotalReplicas:     app.Replicas,
		Message:           "",
	}, nil
}

// ListSpecs lists available specs
func (p *K8sDeploymentProvider) ListSpecs(ctx context.Context) ([]*interfaces.SpecInfo, error) {
	specs := p.manager.ListSpecs()

	// OPTIMIZATION: Preallocate exact size and use indexed loop for better performance
	result := make([]*interfaces.SpecInfo, len(specs))
	for i, spec := range specs {
		// OPTIMIZATION: Convert PlatformConfig map to interface{} map only when needed
		// Most clients don't need platform details, so we can keep the map small
		platforms := make(map[string]interface{}, len(spec.Platforms))
		for k, v := range spec.Platforms {
			platforms[k] = v
		}

		result[i] = &interfaces.SpecInfo{
			Name:        spec.Name,
			DisplayName: spec.DisplayName,
			Category:    spec.Category,
			Resources: interfaces.ResourceRequirements{
				GPU:              spec.Resources.GPU,
				GPUType:          spec.Resources.GpuType,
				CPU:              spec.Resources.CPU,
				Memory:           spec.Resources.Memory,
				EphemeralStorage: spec.Resources.EphemeralStorage,
			},
			Platforms: platforms,
		}
	}

	return result, nil
}

// ListSpecsWithCapacity lists available specs with capacity status
func (p *K8sDeploymentProvider) ListSpecsWithCapacity(ctx context.Context) ([]*interfaces.SpecWithCapacity, error) {
	specs := p.manager.ListSpecs()
	result := make([]*interfaces.SpecWithCapacity, len(specs))
	for i, spec := range specs {
		platforms := make(map[string]interface{}, len(spec.Platforms))
		for k, v := range spec.Platforms {
			platforms[k] = v
		}
		result[i] = &interfaces.SpecWithCapacity{
			SpecInfo: &interfaces.SpecInfo{
				Name:        spec.Name,
				DisplayName: spec.DisplayName,
				Category:    spec.Category,
				Resources: interfaces.ResourceRequirements{
					GPU:              spec.Resources.GPU,
					GPUType:          spec.Resources.GpuType,
					CPU:              spec.Resources.CPU,
					Memory:           spec.Resources.Memory,
					EphemeralStorage: spec.Resources.EphemeralStorage,
				},
				Platforms: platforms,
			},
			Capacity: p.manager.GetCapacityStatus(spec.Name),
		}
	}
	return result, nil
}

// GetSpec gets spec details
func (p *K8sDeploymentProvider) GetSpec(ctx context.Context, specName string) (*interfaces.SpecInfo, error) {
	spec, err := p.manager.GetSpec(specName)
	if err != nil {
		return nil, err
	}

	// Convert PlatformConfig map to interface{} map
	platforms := make(map[string]interface{})
	for k, v := range spec.Platforms {
		platforms[k] = v
	}

	return &interfaces.SpecInfo{
		Name:        spec.Name,
		DisplayName: spec.DisplayName,
		Category:    spec.Category,
		Resources: interfaces.ResourceRequirements{
			GPU:              spec.Resources.GPU,
			GPUType:          spec.Resources.GpuType,
			CPU:              spec.Resources.CPU,
			Memory:           spec.Resources.Memory,
			EphemeralStorage: spec.Resources.EphemeralStorage,
		},
		Platforms: platforms,
	}, nil
}

// PreviewDeploymentYAML previews deployment configuration
func (p *K8sDeploymentProvider) PreviewDeploymentYAML(ctx context.Context, req *interfaces.DeployRequest) (string, error) {
	// Convert to DeployAppRequest
	k8sReq := &DeployAppRequest{
		Endpoint:     req.Endpoint,
		SpecName:     req.SpecName,
		Image:        req.Image,
		Replicas:     req.Replicas,
		GpuCount:     req.GpuCount,
		TaskTimeout:  req.TaskTimeout,
		Env:          req.Env,
		VolumeMounts: req.VolumeMounts,
		ShmSize:      req.ShmSize,
	}

	return p.manager.PreviewYAML(k8sReq)
}

// UpdateDeployment updates deployment
func (p *K8sDeploymentProvider) UpdateDeployment(ctx context.Context, req *interfaces.UpdateDeploymentRequest) (*interfaces.DeployResponse, error) {
	if err := p.manager.UpdateDeployment(ctx, req.Endpoint, req.SpecName, req.Image, req.Replicas, req.VolumeMounts, req.ShmSize, req.EnablePtrace, req.Env); err != nil {
		return nil, err
	}

	return &interfaces.DeployResponse{
		Endpoint:  req.Endpoint,
		Message:   "Deployment updated successfully",
		CreatedAt: "",
	}, nil
}

// GetSpecManager gets spec manager (for autoscaling)
func (p *K8sDeploymentProvider) GetSpecManager() *SpecManager {
	return p.manager.specManager
}

// GetDynamicClient gets dynamic client (for CRD operations)
func (p *K8sDeploymentProvider) GetDynamicClient() dynamic.Interface {
	return p.manager.GetDynamicClient()
}

// GetPodCountsBySpec counts Pods by spec
func (p *K8sDeploymentProvider) GetPodCountsBySpec(ctx context.Context) (map[string]PodCounts, error) {
	return p.manager.GetPodCountsBySpec(ctx)
}

// GetInstanceTypesFromNodePool gets instance types from NodePool
func (p *K8sDeploymentProvider) GetInstanceTypesFromNodePool(ctx context.Context, nodePoolName string) ([]string, error) {
	return p.manager.GetInstanceTypesFromNodePool(ctx, nodePoolName)
}

// GetDefaultEnv gets default environment variables (from wavespeed-config ConfigMap)
func (p *K8sDeploymentProvider) GetDefaultEnv(ctx context.Context) (map[string]string, error) {
	return p.manager.GetDefaultEnvFromConfigMap(ctx)
}

// IsPodDraining checks if Pod is draining
func (p *K8sDeploymentProvider) IsPodDraining(ctx context.Context, podName string) (bool, error) {
	return p.manager.IsPodDraining(ctx, podName)
}

// IsPodTerminating checks if a pod is marked for deletion (DeletionTimestamp set)
// This is used as a safety net to prevent Terminating pods from pulling tasks
func (p *K8sDeploymentProvider) IsPodTerminating(ctx context.Context, podName string) (bool, error) {
	return p.manager.IsPodTerminating(ctx, podName)
}

// MarkPodDraining marks a specific pod as draining (for smart scale-down)
func (p *K8sDeploymentProvider) MarkPodDraining(ctx context.Context, podName string) error {
	return p.manager.MarkPodDraining(ctx, podName)
}

// SetPodDeletionCost sets the pod deletion cost annotation to influence K8s deletion priority
func (p *K8sDeploymentProvider) SetPodDeletionCost(ctx context.Context, podName string, cost int) error {
	return p.manager.SetPodDeletionCost(ctx, podName, cost)
}

// DeletePod gracefully deletes a specific pod
func (p *K8sDeploymentProvider) DeletePod(ctx context.Context, podName string) error {
	return p.manager.DeletePod(ctx, podName)
}

// ForceDeletePod immediately deletes a pod with zero grace period (SIGKILL)
// This is a safety net for stuck pods that don't respond to SIGTERM
func (p *K8sDeploymentProvider) ForceDeletePod(ctx context.Context, podName string) error {
	return p.manager.ForceDeletePod(ctx, podName)
}

// ExecPodCommand executes a command in a pod's container
// Returns stdout, stderr, and error
func (p *K8sDeploymentProvider) ExecPodCommand(ctx context.Context, podName, endpoint string, command []string) (string, string, error) {
	return p.manager.ExecPodCommand(ctx, podName, endpoint, command)
}

// WatchReplicas registers a callback to observe replica changes.
func (p *K8sDeploymentProvider) WatchReplicas(ctx context.Context, callback interfaces.ReplicaCallback) error {
	if p.manager == nil {
		return fmt.Errorf("k8s manager not initialized")
	}
	if callback == nil {
		return fmt.Errorf("replica callback is nil")
	}

	id := p.manager.RegisterReplicaCallback(func(event interfaces.ReplicaEvent) {
		select {
		case <-ctx.Done():
			return
		default:
			callback(event)
		}
	})

	go func() {
		<-ctx.Done()
		p.manager.UnregisterReplicaCallback(id)
	}()

	return nil
}

// WatchPodTerminating registers a callback to observe when pods are marked for deletion.
// This is used to implement graceful shutdown by draining workers before pod termination.
func (p *K8sDeploymentProvider) WatchPodTerminating(ctx context.Context, callback PodTerminatingCallback) error {
	if p.manager == nil {
		return fmt.Errorf("k8s manager not initialized")
	}
	if callback == nil {
		return fmt.Errorf("pod terminating callback is nil")
	}

	id := p.manager.RegisterPodTerminatingCallback(func(podName, endpoint string) {
		select {
		case <-ctx.Done():
			return
		default:
			callback(podName, endpoint)
		}
	})

	go func() {
		<-ctx.Done()
		p.manager.UnregisterPodTerminatingCallback(id)
	}()

	return nil
}

// WatchPodStatusChange registers a callback to observe pod status changes
func (p *K8sDeploymentProvider) WatchPodStatusChange(ctx context.Context, callback PodStatusChangeCallback) error {
	if p.manager == nil {
		return fmt.Errorf("k8s manager not initialized")
	}
	if callback == nil {
		return fmt.Errorf("pod status change callback is nil")
	}

	id := p.manager.RegisterPodStatusChangeCallback(func(podName, endpoint string, info *interfaces.PodInfo) {
		select {
		case <-ctx.Done():
			return
		default:
			callback(podName, endpoint, info)
		}
	})

	go func() {
		<-ctx.Done()
		p.manager.UnregisterPodStatusChangeCallback(id)
	}()

	return nil
}

// WatchPodDelete registers a callback to observe pod deletions
func (p *K8sDeploymentProvider) WatchPodDelete(ctx context.Context, callback PodDeleteCallback) error {
	if p.manager == nil {
		return fmt.Errorf("k8s manager not initialized")
	}
	if callback == nil {
		return fmt.Errorf("pod delete callback is nil")
	}

	id := p.manager.RegisterPodDeleteCallback(func(podName, endpoint string) {
		select {
		case <-ctx.Done():
			return
		default:
			callback(podName, endpoint)
		}
	})

	go func() {
		<-ctx.Done()
		p.manager.UnregisterPodDeleteCallback(id)
	}()

	return nil
}

// WatchSpotInterruption registers a callback to observe spot interruptions
func (p *K8sDeploymentProvider) WatchSpotInterruption(ctx context.Context, callback func(podName, endpoint, reason string)) error {
	if p.manager == nil {
		return fmt.Errorf("k8s manager not initialized")
	}
	if callback == nil {
		return fmt.Errorf("spot interruption callback is nil")
	}

	id := p.manager.RegisterSpotInterruptionCallback(callback)

	go func() {
		<-ctx.Done()
		p.manager.UnregisterSpotInterruptionCallback(id)
	}()

	return nil
}

// WatchDeploymentSpecChange registers a callback to observe when deployment spec changes.
// This is used to optimize pod replacement during rolling updates by prioritizing idle workers.
func (p *K8sDeploymentProvider) WatchDeploymentSpecChange(ctx context.Context, callback DeploymentSpecChangeCallback) error {
	if p.manager == nil {
		return fmt.Errorf("k8s manager not initialized")
	}
	if callback == nil {
		return fmt.Errorf("deployment spec change callback is nil")
	}

	id := p.manager.RegisterDeploymentSpecChangeCallback(func(endpoint string) {
		select {
		case <-ctx.Done():
			return
		default:
			callback(endpoint)
		}
	})

	go func() {
		<-ctx.Done()
		p.manager.UnregisterDeploymentSpecChangeCallback(id)
	}()

	return nil
}

// WatchDeploymentStatusChange registers a callback to observe when deployment status changes.
// This is used to sync deployment status to database.
func (p *K8sDeploymentProvider) WatchDeploymentStatusChange(ctx context.Context, callback DeploymentStatusChangeCallback) error {
	if p.manager == nil {
		return fmt.Errorf("k8s manager not initialized")
	}
	if callback == nil {
		return fmt.Errorf("deployment status change callback is nil")
	}

	id := p.manager.RegisterDeploymentStatusChangeCallback(func(endpoint string, deployment *appsv1.Deployment) {
		select {
		case <-ctx.Done():
			return
		default:
			callback(endpoint, deployment)
		}
	})

	go func() {
		<-ctx.Done()
		p.manager.UnregisterDeploymentStatusChangeCallback(id)
	}()

	return nil
}

// Close releases underlying informers/resources
func (p *K8sDeploymentProvider) Close() {
	if p.manager != nil {
		p.manager.Close()
	}
}

// GetManager returns the underlying K8s manager.
// This is used by the worker status monitor to access pod watching capabilities.
func (p *K8sDeploymentProvider) GetManager() *Manager {
	return p.manager
}

// GetRestConfig returns the Kubernetes REST config for exec/attach operations
func (p *K8sDeploymentProvider) GetRestConfig() *rest.Config {
	if p.manager == nil {
		return nil
	}
	return p.manager.GetRestConfig()
}

// GetNamespace returns the namespace this provider operates in
func (p *K8sDeploymentProvider) GetNamespace() string {
	if p.manager == nil {
		return ""
	}
	return p.manager.GetNamespace()
}

// GetClientset returns the Kubernetes clientset
func (p *K8sDeploymentProvider) GetClientset() kubernetes.Interface {
	if p.manager == nil {
		return nil
	}
	return p.manager.client
}

// GetPods gets all Pod info for specified endpoint (including Pending, Running, Terminating)
func (p *K8sDeploymentProvider) GetPods(ctx context.Context, endpoint string) ([]*interfaces.PodInfo, error) {
	if p.manager == nil {
		return nil, fmt.Errorf("k8s manager not initialized")
	}
	return p.manager.GetPods(ctx, endpoint)
}

// DescribePod gets detailed Pod info (similar to kubectl describe)
func (p *K8sDeploymentProvider) DescribePod(ctx context.Context, endpoint string, podName string) (*interfaces.PodDetail, error) {
	if p.manager == nil {
		return nil, fmt.Errorf("k8s manager not initialized")
	}
	return p.manager.DescribePod(ctx, endpoint, podName)
}

// GetPodYAML gets Pod YAML (similar to kubectl get pod -o yaml)
func (p *K8sDeploymentProvider) GetPodYAML(ctx context.Context, endpoint string, podName string) (string, error) {
	if p.manager == nil {
		return "", fmt.Errorf("k8s manager not initialized")
	}
	return p.manager.GetPodYAML(ctx, endpoint, podName)
}

// ListPVCs lists all PersistentVolumeClaims in the namespace
func (p *K8sDeploymentProvider) ListPVCs(ctx context.Context) ([]*interfaces.PVCInfo, error) {
	if p.manager == nil {
		return nil, fmt.Errorf("k8s manager not initialized")
	}
	return p.manager.ListPVCs(ctx)
}

// TerminateWorker terminates a specific worker (pod) due to failure.
// This implements the WorkerTerminator interface for resource release.
// It is called by ResourceReleaser when a worker exceeds the image pull timeout.
func (p *K8sDeploymentProvider) TerminateWorker(ctx context.Context, endpoint, workerID, reason string) error {
	if p.manager == nil {
		return fmt.Errorf("k8s manager not initialized")
	}

	// workerID is typically the pod name
	// First try graceful deletion, then force delete if needed
	err := p.manager.DeletePod(ctx, workerID)
	if err != nil {
		// If graceful delete fails, try force delete
		return p.manager.ForceDeletePod(ctx, workerID)
	}
	return nil
}
