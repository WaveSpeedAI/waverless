package k8s

import (
	"context"
	"fmt"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"waverless/pkg/config"
	"waverless/pkg/interfaces"
)

// K8sDeploymentProvider K8s部署提供者实现
type K8sDeploymentProvider struct {
	manager *Manager
}

// NewK8sDeploymentProvider 创建K8s部署提供者
func NewK8sDeploymentProvider(cfg *config.Config) (interfaces.DeploymentProvider, error) {
	if !cfg.K8s.Enabled {
		return nil, fmt.Errorf("k8s is not enabled in config")
	}

	manager, err := NewManager(cfg.K8s.Namespace, cfg.K8s.Platform, cfg.K8s.ConfigDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s manager: %w", err)
	}

	return &K8sDeploymentProvider{
		manager: manager,
	}, nil
}

// Deploy 部署应用
func (p *K8sDeploymentProvider) Deploy(ctx context.Context, req *interfaces.DeployRequest) (*interfaces.DeployResponse, error) {
	// Convert to DeployAppRequest
	k8sReq := &DeployAppRequest{
		Endpoint: req.Endpoint,
		SpecName: req.SpecName,
		Image:    req.Image,
		Replicas: req.Replicas,
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

// GetApp 获取应用详情
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
	}, nil
}

// ListApps 列出所有应用
func (p *K8sDeploymentProvider) ListApps(ctx context.Context) ([]*interfaces.AppInfo, error) {
	apps, err := p.manager.ListApps(ctx)
	if err != nil {
		return nil, err
	}

	// Convert to interfaces.AppInfo 列表
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
		})
	}

	return result, nil
}

// DeleteApp 删除应用
func (p *K8sDeploymentProvider) DeleteApp(ctx context.Context, endpoint string) error {
	return p.manager.DeleteApp(ctx, endpoint)
}

// GetAppLogs 获取应用日志
func (p *K8sDeploymentProvider) GetAppLogs(ctx context.Context, endpoint string, lines int, podName ...string) (string, error) {
	return p.manager.GetAppLogs(ctx, endpoint, int64(lines), podName...)
}

// ScaleApp 扩缩容应用
func (p *K8sDeploymentProvider) ScaleApp(ctx context.Context, endpoint string, replicas int) error {
	return p.manager.ScaleDeployment(ctx, endpoint, replicas)
}

// GetAppStatus 获取应用状态
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

// ListSpecs 列出可用规格
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
				Disk:             spec.Resources.Disk,
				EphemeralStorage: spec.Resources.EphemeralStorage,
			},
			Platforms: platforms,
		}
	}

	return result, nil
}

// GetSpec 获取规格详情
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
			Disk:             spec.Resources.Disk,
			EphemeralStorage: spec.Resources.EphemeralStorage,
		},
		Platforms: platforms,
	}, nil
}

// PreviewDeploymentYAML 预览部署配置
func (p *K8sDeploymentProvider) PreviewDeploymentYAML(ctx context.Context, req *interfaces.DeployRequest) (string, error) {
	// Convert to DeployAppRequest
	k8sReq := &DeployAppRequest{
		Endpoint: req.Endpoint,
		SpecName: req.SpecName,
		Image:    req.Image,
		Replicas: req.Replicas,
	}

	return p.manager.PreviewYAML(k8sReq)
}

// UpdateDeployment 更新部署
func (p *K8sDeploymentProvider) UpdateDeployment(ctx context.Context, req *interfaces.UpdateDeploymentRequest) (*interfaces.DeployResponse, error) {
	if err := p.manager.UpdateDeployment(ctx, req.Endpoint, req.SpecName, req.Image, req.Replicas); err != nil {
		return nil, err
	}

	return &interfaces.DeployResponse{
		Endpoint:  req.Endpoint,
		Message:   "Deployment updated successfully",
		CreatedAt: "",
	}, nil
}

// GetSpecManager 获取规格管理器（用于自动扩缩容）
func (p *K8sDeploymentProvider) GetSpecManager() *SpecManager {
	return p.manager.specManager
}

// IsPodDraining 检查Pod是否正在排空
func (p *K8sDeploymentProvider) IsPodDraining(ctx context.Context, podName string) (bool, error) {
	return p.manager.IsPodDraining(ctx, podName)
}

// IsPodTerminating checks if a pod is marked for deletion (DeletionTimestamp set)
// This is used as a safety net to prevent Terminating pods from pulling tasks
func (p *K8sDeploymentProvider) IsPodTerminating(ctx context.Context, podName string) (bool, error) {
	return p.manager.IsPodTerminating(ctx, podName)
}

// MarkPodsAsDraining 标记Pod为排空状态
func (p *K8sDeploymentProvider) MarkPodsAsDraining(ctx context.Context, endpoint string, count int) error {
	return p.manager.MarkPodsAsDraining(ctx, endpoint, count)
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

// Close releases underlying informers/resources
func (p *K8sDeploymentProvider) Close() {
	if p.manager != nil {
		p.manager.Close()
	}
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

// GetPods 获取指定 endpoint 的所有 Pod 信息（包括 Pending、Running、Terminating）
func (p *K8sDeploymentProvider) GetPods(ctx context.Context, endpoint string) ([]*interfaces.PodInfo, error) {
	if p.manager == nil {
		return nil, fmt.Errorf("k8s manager not initialized")
	}
	return p.manager.GetPods(ctx, endpoint)
}

// DescribePod 获取 Pod 的详细信息（类似 kubectl describe）
func (p *K8sDeploymentProvider) DescribePod(ctx context.Context, endpoint string, podName string) (*interfaces.PodDetail, error) {
	if p.manager == nil {
		return nil, fmt.Errorf("k8s manager not initialized")
	}
	return p.manager.DescribePod(ctx, endpoint, podName)
}
