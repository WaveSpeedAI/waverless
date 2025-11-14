package k8s

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	appslisters "k8s.io/client-go/listers/apps/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/yaml"

	"waverless/pkg/interfaces"
	"waverless/pkg/logger"
)

var (
	// Kubernetes DNS-1123 label specification: lowercase letters, numbers, '-', must start and end with alphanumeric
	// Maximum length 63 characters
	dns1123LabelRegex = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
)

// validateK8sName validates and sanitizes a Kubernetes resource name
// Returns error if the name cannot be made valid
func validateK8sName(name string) error {
	// Trim whitespace
	name = strings.TrimSpace(name)

	// Check length
	if len(name) == 0 {
		return fmt.Errorf("name cannot be empty")
	}
	if len(name) > 63 {
		return fmt.Errorf("name too long (max 63 characters): %s", name)
	}

	// Convert to lowercase
	name = strings.ToLower(name)

	// Check for invalid characters (anything other than a-z, 0-9, -)
	if !dns1123LabelRegex.MatchString(name) {
		return fmt.Errorf("invalid name '%s': must consist of lowercase alphanumeric characters or '-', and must start and end with an alphanumeric character", name)
	}

	return nil
}

// Manager K8s application manager
type Manager struct {
	client      kubernetes.Interface
	config      *rest.Config
	namespace   string
	platform    Platform
	specManager *SpecManager
	renderer    *TemplateRenderer

	informerFactory  informers.SharedInformerFactory
	deploymentLister appslisters.DeploymentLister
	podLister        corelisters.PodLister
	informerStopCh   chan struct{}
	stopOnce         sync.Once

	callbacksMu                   sync.RWMutex
	replicaCallbacks              map[int64]interfaces.ReplicaCallback
	podTerminatingCallbacks       map[int64]PodTerminatingCallback
	deploymentSpecChangeCallbacks map[int64]DeploymentSpecChangeCallback
	nextCallbackID                int64
}

// PodTerminatingCallback is called when a pod is marked for deletion (DeletionTimestamp set)
// This allows the system to drain workers before pods are actually terminated
type PodTerminatingCallback func(podName, endpoint string)

// DeploymentSpecChangeCallback is called when a deployment's spec changes (image, resources, etc.)
// This allows the system to optimize pod replacement during rolling updates
type DeploymentSpecChangeCallback func(endpoint string)

// NewManager creates a K8s manager
func NewManager(namespace, platformName, configDir string) (*Manager, error) {
	// Create K8s client
	config, err := rest.InClusterConfig()
	if err != nil {
		// If not in cluster, try to use kubeconfig
		loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
		configOverrides := &clientcmd.ConfigOverrides{}
		kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
		config, err = kubeConfig.ClientConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to get kubernetes config: %v", err)
		}
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %v", err)
	}

	// Create spec manager
	specPath := fmt.Sprintf("%s/specs.yaml", configDir)
	specManager, err := NewSpecManager(specPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create spec manager: %v", err)
	}

	// Create platform instance
	platformFactory := NewPlatformFactory()
	platform := platformFactory.CreatePlatform(platformName)

	// Create template renderer
	templateDir := fmt.Sprintf("%s/templates", configDir)
	renderer := NewTemplateRenderer(templateDir)

	// Setup shared informers for Deployments/Pods in the configured namespace
	// Resync period: check for updates every 5 minutes
	stopCh := make(chan struct{})
	informerFactory := informers.NewSharedInformerFactoryWithOptions(
		client,
		5*time.Minute, // Resync period
		informers.WithNamespace(namespace),
	)
	deploymentInformer := informerFactory.Apps().V1().Deployments()
	podInformer := informerFactory.Core().V1().Pods()

	manager := &Manager{
		client:                        client,
		config:                        config,
		namespace:                     namespace,
		platform:                      platform,
		specManager:                   specManager,
		renderer:                      renderer,
		informerFactory:               informerFactory,
		deploymentLister:              deploymentInformer.Lister(),
		podLister:                     podInformer.Lister(),
		informerStopCh:                stopCh,
		replicaCallbacks:              make(map[int64]interfaces.ReplicaCallback),
		podTerminatingCallbacks:       make(map[int64]PodTerminatingCallback),
		deploymentSpecChangeCallbacks: make(map[int64]DeploymentSpecChangeCallback),
	}

	// Add event handlers to force informers to start watching
	// Without handlers, informers may not actually watch in some environments (ASK/Virtual Kubelet)
	deploymentInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    manager.handleDeploymentEvent,
		UpdateFunc: manager.handleDeploymentUpdate,
		DeleteFunc: manager.handleDeploymentDelete,
	})

	podInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			logger.DebugCtx(context.Background(), "pod added (informer cache)")
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			logger.DebugCtx(context.Background(), "pod updated (informer cache)")
			manager.handlePodUpdate(oldObj, newObj)
		},
		DeleteFunc: func(obj interface{}) {
			logger.DebugCtx(context.Background(), "pod deleted (informer cache)")
		},
	})

	// Start informers asynchronously (non-blocking mode)
	// This is critical for ASK/Virtual Kubelet environments where initial sync may be slow
	logger.InfoCtx(context.Background(), "starting k8s informers for namespace: %s (async mode)", namespace)
	go informerFactory.Start(stopCh)

	// Monitor sync status in background without blocking startup
	go func() {
		ctx := context.Background()
		logger.InfoCtx(ctx, "waiting for k8s informers to sync...")

		// Wait for initial sync with timeout
		syncDone := make(chan bool, 1)
		go func() {
			// Log sync status periodically
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()

			syncChecking := true
			go func() {
				ok := cache.WaitForCacheSync(stopCh,
					deploymentInformer.Informer().HasSynced,
					podInformer.Informer().HasSynced)
				syncChecking = false
				syncDone <- ok
			}()

			for syncChecking {
				select {
				case <-ticker.C:
					deploymentSynced := deploymentInformer.Informer().HasSynced()
					podSynced := podInformer.Informer().HasSynced()
					logger.InfoCtx(ctx, "informer sync status: deployment=%v, pod=%v", deploymentSynced, podSynced)
				case <-stopCh:
					return
				}
			}
		}()

		select {
		case ok := <-syncDone:
			if ok {
				logger.InfoCtx(ctx, "✅ k8s informers synced successfully for namespace: %s", namespace)
				logger.InfoCtx(ctx, "informer cache is now ready, queries will be fast (~100μs)")
			} else {
				logger.WarnCtx(ctx, "❌ k8s informers failed to sync for namespace: %s, will retry in background", namespace)
			}
		case <-time.After(60 * time.Second):
			deploymentSynced := deploymentInformer.Informer().HasSynced()
			podSynced := podInformer.Informer().HasSynced()
			logger.WarnCtx(ctx, "⏰ k8s informers initial sync timeout after 60s for namespace: %s", namespace)
			logger.WarnCtx(ctx, "sync status: deployment=%v, pod=%v", deploymentSynced, podSynced)
			logger.WarnCtx(ctx, "informers will continue syncing in background, queries will use live API until ready")

			// Diagnose potential issues
			if !deploymentSynced && !podSynced {
				logger.WarnCtx(ctx, "🔍 possible issues:")
				logger.WarnCtx(ctx, "  1. Network connectivity to API server")
				logger.WarnCtx(ctx, "  2. RBAC permissions (need 'watch' verb)")
				logger.WarnCtx(ctx, "  3. Virtual Kubelet environment compatibility")
				logger.WarnCtx(ctx, "  4. API server watch endpoint not working")
			}
		}
	}()

	return manager, nil
}

// DeployAppRequest deployment request (simplified version)
type DeployAppRequest struct {
	// Core variables (user input)
	Endpoint    string `json:"endpoint" binding:"required"` // Endpoint name
	SpecName    string `json:"specName" binding:"required"` // Spec name
	Image       string `json:"image" binding:"required"`    // Image
	Replicas    int    `json:"replicas,omitempty"`          // Replica count (default 1)
	TaskTimeout int    `json:"taskTimeout,omitempty"`       // Task execution timeout in seconds (0 = use global default)

	// Auto-scaling configuration (optional)
	MinReplicas       int   `json:"minReplicas,omitempty"`       // Minimum replica count (default 0)
	MaxReplicas       int   `json:"maxReplicas,omitempty"`       // Maximum replica count (default 10)
	ScaleUpThreshold  int   `json:"scaleUpThreshold,omitempty"`  // Queue threshold for scale up (default 1)
	ScaleDownIdleTime int   `json:"scaleDownIdleTime,omitempty"` // Idle time in seconds before scale down (default 300)
	ScaleUpCooldown   int   `json:"scaleUpCooldown,omitempty"`   // Scale up cooldown in seconds (default 30)
	ScaleDownCooldown int   `json:"scaleDownCooldown,omitempty"` // Scale down cooldown in seconds (default 60)
	Priority          int   `json:"priority,omitempty"`          // Priority for resource allocation (0-100, default 50)
	EnableDynamicPrio *bool `json:"enableDynamicPrio,omitempty"` // Enable dynamic priority (default true)
	HighLoadThreshold int   `json:"highLoadThreshold,omitempty"` // High load threshold for priority boost (default 10)
	PriorityBoost     int   `json:"priorityBoost,omitempty"`     // Priority boost amount when high load (default 20)
}

// DeployApp deploys an application
func (m *Manager) DeployApp(ctx context.Context, req *DeployAppRequest) error {
	// Validate endpoint name
	if err := validateK8sName(req.Endpoint); err != nil {
		return fmt.Errorf("invalid endpoint name: %w", err)
	}

	// Normalize endpoint name (trim and lowercase)
	req.Endpoint = strings.ToLower(strings.TrimSpace(req.Endpoint))

	// Get spec
	spec, err := m.specManager.GetSpec(req.SpecName)
	if err != nil {
		return err
	}

	// Build render context
	renderCtx, err := m.buildRenderContext(req, spec)
	if err != nil {
		return err
	}

	// Render Deployment template
	yamlContent, err := m.renderer.Render("deployment.yaml", renderCtx)
	if err != nil {
		return err
	}

	// Apply YAML
	return m.applyYAML(ctx, yamlContent)
}

// buildRenderContext builds render context (simplified version)
func (m *Manager) buildRenderContext(req *DeployAppRequest, spec *ResourceSpec) (*RenderContext, error) {
	// Get platform-specific configuration
	platformConfig := spec.GetPlatformConfig(m.platform.GetName())

	// Build render context
	ctx := &RenderContext{
		Endpoint:      req.Endpoint,
		Namespace:     m.namespace,
		Image:         req.Image,
		Replicas:      req.Replicas,
		ContainerName: fmt.Sprintf("%s-worker", req.Endpoint), // Or directly use endpoint name
		ContainerPort: 8000,                                   // Default container port
		ProxyPort:     8001,                                   // Default proxy port

		// Resource configuration (from Spec)
		IsGpu:         spec.Category == "gpu",
		CpuLimit:      spec.Resources.CPU,
		MemoryRequest: spec.Resources.Memory,

		// K8s scheduling configuration (from Spec)
		NodeSelector: platformConfig.NodeSelector,
		Tolerations:  platformConfig.Tolerations,
		Labels:       platformConfig.Labels,
		Annotations:  platformConfig.Annotations,

		// Graceful shutdown configuration
		TaskTimeout: req.TaskTimeout,
	}

	// Inject spec name as label for tracking
	if ctx.Labels == nil {
		ctx.Labels = make(map[string]string)
	}
	ctx.Labels["waverless.io/spec"] = req.SpecName

	// Record platform configuration (for precise deletion during future updates)
	// Filter out system labels/annotations (waverless.io/* prefix) to prevent accidental deletion of runtime-added labels
	if len(platformConfig.Labels) > 0 {
		filteredLabels := make(map[string]string)
		for k, v := range platformConfig.Labels {
			if !strings.HasPrefix(k, "waverless.io/") {
				filteredLabels[k] = v
			}
		}
		if len(filteredLabels) > 0 {
			if labelsJSON, err := json.Marshal(filteredLabels); err == nil {
				ctx.PlatformLabelsJSON = string(labelsJSON)
			}
		}
	}
	if len(platformConfig.Annotations) > 0 {
		filteredAnnotations := make(map[string]string)
		for k, v := range platformConfig.Annotations {
			if !strings.HasPrefix(k, "waverless.io/") {
				filteredAnnotations[k] = v
			}
		}
		if len(filteredAnnotations) > 0 {
			if annotationsJSON, err := json.Marshal(filteredAnnotations); err == nil {
				ctx.PlatformAnnotationsJSON = string(annotationsJSON)
			}
		}
	}

	// Replicas default value
	if ctx.Replicas == 0 {
		ctx.Replicas = 1
	}

	// GPU count
	if ctx.IsGpu {
		// Parse GPU count string (e.g., "1" -> 1)
		fmt.Sscanf(spec.Resources.GPU, "%d", &ctx.GpuCount)
	}

	// Calculate termination grace period
	// Formula: taskTimeout + 30s buffer for cleanup
	// Default: 330s (300s task timeout + 30s buffer)
	taskTimeout := ctx.TaskTimeout
	if taskTimeout == 0 {
		taskTimeout = 300 // Default 5 minutes
	}
	ctx.TerminationGracePeriodSeconds = int64(taskTimeout + 30)

	return ctx, nil
}

// applyYAML applies YAML configuration
func (m *Manager) applyYAML(ctx context.Context, yamlContent string) error {
	// Split multiple YAML documents
	docs := strings.Split(yamlContent, "---")

	for _, doc := range docs {
		doc = strings.TrimSpace(doc)
		if doc == "" {
			continue
		}

		// Parse YAML to determine resource type
		var meta metav1.TypeMeta
		if err := yaml.Unmarshal([]byte(doc), &meta); err != nil {
			return fmt.Errorf("failed to parse YAML: %v", err)
		}

		switch meta.Kind {
		case "Deployment":
			var deployment appsv1.Deployment
			if err := yaml.Unmarshal([]byte(doc), &deployment); err != nil {
				return fmt.Errorf("failed to parse Deployment: %v", err)
			}
			if err := m.applyDeployment(ctx, &deployment); err != nil {
				return err
			}

		default:
			return fmt.Errorf("unsupported resource kind: %s", meta.Kind)
		}
	}

	return nil
}

// applyPV applies PersistentVolume
func (m *Manager) applyPV(ctx context.Context, pv *corev1.PersistentVolume) error {
	pvs := m.client.CoreV1().PersistentVolumes()
	existing, err := pvs.Get(ctx, pv.Name, metav1.GetOptions{})
	if err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to get PV: %v", err)
		}
		// Create new
		_, err = pvs.Create(ctx, pv, metav1.CreateOptions{})
		return err
	}

	// Update existing
	pv.ResourceVersion = existing.ResourceVersion
	_, err = pvs.Update(ctx, pv, metav1.UpdateOptions{})
	return err
}

// applyPVC applies PersistentVolumeClaim
func (m *Manager) applyPVC(ctx context.Context, pvc *corev1.PersistentVolumeClaim) error {
	pvcs := m.client.CoreV1().PersistentVolumeClaims(m.namespace)
	existing, err := pvcs.Get(ctx, pvc.Name, metav1.GetOptions{})
	if err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to get PVC: %v", err)
		}
		// Create new
		_, err = pvcs.Create(ctx, pvc, metav1.CreateOptions{})
		return err
	}

	// PVC doesn't support updating specs, skip
	_ = existing
	return nil
}

// applyPod applies Pod
func (m *Manager) applyPod(ctx context.Context, pod *corev1.Pod) error {
	pods := m.client.CoreV1().Pods(m.namespace)
	existing, err := pods.Get(ctx, pod.Name, metav1.GetOptions{})
	if err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to get Pod: %v", err)
		}
		// Create new
		_, err = pods.Create(ctx, pod, metav1.CreateOptions{})
		return err
	}

	// Pod exists, need to delete before creating
	if err := pods.Delete(ctx, pod.Name, metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("failed to delete existing pod: %v", err)
	}

	// Wait for Pod deletion
	time.Sleep(2 * time.Second)

	// Create new Pod
	_, err = pods.Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create new pod: %v", err)
	}

	_ = existing
	return nil
}

// applyDeployment applies Deployment
func (m *Manager) applyDeployment(ctx context.Context, deployment *appsv1.Deployment) error {
	deployments := m.client.AppsV1().Deployments(m.namespace)
	existing, err := deployments.Get(ctx, deployment.Name, metav1.GetOptions{})
	if err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to get Deployment: %v", err)
		}
		// Create new
		_, err = deployments.Create(ctx, deployment, metav1.CreateOptions{})
		return err
	}

	// Update existing Deployment
	deployment.ResourceVersion = existing.ResourceVersion
	_, err = deployments.Update(ctx, deployment, metav1.UpdateOptions{})
	return err
}

// PreviewYAML previews YAML
func (m *Manager) PreviewYAML(req *DeployAppRequest) (string, error) {
	// Get spec
	spec, err := m.specManager.GetSpec(req.SpecName)
	if err != nil {
		return "", err
	}

	// Build render context
	renderCtx, err := m.buildRenderContext(req, spec)
	if err != nil {
		return "", err
	}

	// Render template
	return m.renderer.Render("deployment.yaml", renderCtx)
}

// AppInfo application information
type AppInfo struct {
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace"`
	Type              string            `json:"type"` // Pod or Deployment
	Status            string            `json:"status"`
	Replicas          int32             `json:"replicas,omitempty"`          // Deployment desired replica count
	ReadyReplicas     int32             `json:"readyReplicas,omitempty"`     // Deployment ready replica count
	AvailableReplicas int32             `json:"availableReplicas,omitempty"` // Deployment available replica count
	PodIP             string            `json:"podIp,omitempty"`
	HostIP            string            `json:"hostIp,omitempty"`
	Image             string            `json:"image"`
	Labels            map[string]string `json:"labels"`
	CreatedAt         string            `json:"createdAt"`
}

// GetApp gets application details
func (m *Manager) GetApp(ctx context.Context, name string) (*AppInfo, error) {
	// Try cache (Deployment)
	if m.deploymentLister != nil {
		if deployment, err := m.deploymentLister.Deployments(m.namespace).Get(name); err == nil {
			return deploymentToAppInfo(deployment), nil
		} else if !errors.IsNotFound(err) {
			return nil, fmt.Errorf("failed to get deployment from cache: %v", err)
		}
	}

	// Fallback to Pod cache
	if m.podLister != nil {
		if pod, err := m.podLister.Pods(m.namespace).Get(name); err == nil {
			return podToAppInfo(pod), nil
		} else if !errors.IsNotFound(err) {
			return nil, fmt.Errorf("failed to get pod from cache: %v", err)
		}
	}

	// As a last resort, query API server directly (handles newly created resources before cache sync)
	if deploymentLive, err := m.client.AppsV1().Deployments(m.namespace).Get(ctx, name, metav1.GetOptions{}); err == nil {
		return deploymentToAppInfo(deploymentLive), nil
	} else if !errors.IsNotFound(err) {
		return nil, fmt.Errorf("failed to get deployment: %v", err)
	}

	podLive, podErr := m.client.CoreV1().Pods(m.namespace).Get(ctx, name, metav1.GetOptions{})
	if podErr == nil {
		return podToAppInfo(podLive), nil
	}

	return nil, fmt.Errorf("failed to get pod: %v", podErr)
}

// ListApps lists all applications (only those managed by Waverless)
func (m *Manager) ListApps(ctx context.Context) ([]*AppInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	result := make([]*AppInfo, 0)
	selector := labels.SelectorFromSet(labels.Set{"managed-by": "waverless"})

	// Try informer cache first if synced (fast path, ~100μs)
	useCache := false
	if m.deploymentLister != nil && m.informerFactory != nil {
		if m.informerFactory.Apps().V1().Deployments().Informer().HasSynced() {
			if deployments, err := m.deploymentLister.Deployments(m.namespace).List(selector); err == nil {
				for _, deployment := range deployments {
					result = append(result, deploymentToAppInfo(deployment))
				}
				useCache = true
				logger.DebugCtx(ctx, "listed deployments from informer cache")
			}
		}
	}

	// Fallback to API if cache not ready or failed (slower, ~200-500ms)
	if !useCache {
		logger.DebugCtx(ctx, "deployment informer not ready, using live API")
		result = append(result, m.listDeploymentsViaAPI(ctx)...)
	}

	// Try pods from cache
	usePodCache := false
	if m.podLister != nil && m.informerFactory != nil {
		if m.informerFactory.Core().V1().Pods().Informer().HasSynced() {
			if pods, err := m.podLister.Pods(m.namespace).List(selector); err == nil {
				for _, pod := range pods {
					if len(pod.OwnerReferences) > 0 {
						continue
					}
					result = append(result, podToAppInfo(pod))
				}
				usePodCache = true
				logger.DebugCtx(ctx, "listed pods from informer cache")
			}
		}
	}

	// Fallback to API for pods
	if !usePodCache {
		logger.DebugCtx(ctx, "pod informer not ready, using live API")
		result = append(result, m.listPodsViaAPI(ctx)...)
	}

	return result, nil
}

// Close stops internal informers
func (m *Manager) Close() {
	if m.informerStopCh == nil {
		return
	}
	m.stopOnce.Do(func() {
		close(m.informerStopCh)
		m.informerStopCh = nil
	})
}

// RegisterReplicaCallback adds a new replica change listener and returns its id.
func (m *Manager) RegisterReplicaCallback(cb interfaces.ReplicaCallback) int64 {
	if cb == nil {
		return 0
	}
	id := atomic.AddInt64(&m.nextCallbackID, 1)
	m.callbacksMu.Lock()
	if m.replicaCallbacks == nil {
		m.replicaCallbacks = make(map[int64]interfaces.ReplicaCallback)
	}
	m.replicaCallbacks[id] = cb
	m.callbacksMu.Unlock()
	return id
}

// UnregisterReplicaCallback removes a previously registered listener.
func (m *Manager) UnregisterReplicaCallback(id int64) {
	if id == 0 {
		return
	}
	m.callbacksMu.Lock()
	if m.replicaCallbacks != nil {
		delete(m.replicaCallbacks, id)
	}
	m.callbacksMu.Unlock()
}

// RegisterPodTerminatingCallback adds a new pod terminating listener and returns its id.
// The callback is invoked when a pod is marked for deletion (DeletionTimestamp set).
// This allows the system to drain workers before pods are actually terminated.
func (m *Manager) RegisterPodTerminatingCallback(cb PodTerminatingCallback) int64 {
	if cb == nil {
		return 0
	}
	id := atomic.AddInt64(&m.nextCallbackID, 1)
	m.callbacksMu.Lock()
	if m.podTerminatingCallbacks == nil {
		m.podTerminatingCallbacks = make(map[int64]PodTerminatingCallback)
	}
	m.podTerminatingCallbacks[id] = cb
	m.callbacksMu.Unlock()
	return id
}

// UnregisterPodTerminatingCallback removes a previously registered pod terminating listener.
func (m *Manager) UnregisterPodTerminatingCallback(id int64) {
	if id == 0 {
		return
	}
	m.callbacksMu.Lock()
	if m.podTerminatingCallbacks != nil {
		delete(m.podTerminatingCallbacks, id)
	}
	m.callbacksMu.Unlock()
}

// RegisterDeploymentSpecChangeCallback adds a new deployment spec change listener and returns its id.
// The callback is invoked when a deployment's spec changes (image, resources, env, etc).
// This allows the system to optimize pod replacement during rolling updates.
func (m *Manager) RegisterDeploymentSpecChangeCallback(cb DeploymentSpecChangeCallback) int64 {
	if cb == nil {
		return 0
	}
	id := atomic.AddInt64(&m.nextCallbackID, 1)
	m.callbacksMu.Lock()
	if m.deploymentSpecChangeCallbacks == nil {
		m.deploymentSpecChangeCallbacks = make(map[int64]DeploymentSpecChangeCallback)
	}
	m.deploymentSpecChangeCallbacks[id] = cb
	m.callbacksMu.Unlock()
	return id
}

// UnregisterDeploymentSpecChangeCallback removes a previously registered deployment spec change listener.
func (m *Manager) UnregisterDeploymentSpecChangeCallback(id int64) {
	if id == 0 {
		return
	}
	m.callbacksMu.Lock()
	if m.deploymentSpecChangeCallbacks != nil {
		delete(m.deploymentSpecChangeCallbacks, id)
	}
	m.callbacksMu.Unlock()
}

// notifyDeploymentSpecChange notifies all registered callbacks that a deployment spec has changed
func (m *Manager) notifyDeploymentSpecChange(endpoint string) {
	m.callbacksMu.RLock()
	callbacks := make([]DeploymentSpecChangeCallback, 0, len(m.deploymentSpecChangeCallbacks))
	for _, cb := range m.deploymentSpecChangeCallbacks {
		callbacks = append(callbacks, cb)
	}
	m.callbacksMu.RUnlock()

	// Fan out asynchronously to avoid blocking informer thread
	for _, cb := range callbacks {
		cb := cb // capture for goroutine
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.ErrorCtx(context.Background(), "Deployment spec change callback panicked: %v", r)
				}
			}()
			cb(endpoint)
		}()
	}
}

// handlePodUpdate handles pod update events and detects when pods are marked for deletion
func (m *Manager) handlePodUpdate(oldObj, newObj interface{}) {
	oldPod, oldOk := oldObj.(*corev1.Pod)
	newPod, newOk := newObj.(*corev1.Pod)

	if !oldOk || !newOk || oldPod == nil || newPod == nil {
		return
	}

	// Detect when a pod is marked for deletion (DeletionTimestamp transitions from nil to non-nil)
	if oldPod.DeletionTimestamp == nil && newPod.DeletionTimestamp != nil {
		// Pod is now terminating
		podName := newPod.Name

		// Extract endpoint from pod labels
		endpoint := ""
		managedBy := ""
		if newPod.Labels != nil {
			endpoint = newPod.Labels["app"]
			managedBy = newPod.Labels["managed-by"]
		}

		// Only handle pods managed by waverless (worker pods)
		// Skip waverless service's own pods
		if endpoint != "" && managedBy == "waverless" && endpoint != "waverless" {
			logger.InfoCtx(context.Background(), "🔔 Pod %s (endpoint: %s) marked for deletion, notifying callbacks",
				podName, endpoint)
			m.notifyPodTerminating(podName, endpoint)
		}
	}
}

// notifyPodTerminating notifies all registered callbacks that a pod is terminating
func (m *Manager) notifyPodTerminating(podName, endpoint string) {
	m.callbacksMu.RLock()
	callbacks := make([]PodTerminatingCallback, 0, len(m.podTerminatingCallbacks))
	for _, cb := range m.podTerminatingCallbacks {
		callbacks = append(callbacks, cb)
	}
	m.callbacksMu.RUnlock()

	// Fan out asynchronously to avoid blocking informer thread
	for _, cb := range callbacks {
		cb := cb // capture for goroutine
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.ErrorCtx(context.Background(), "Pod terminating callback panicked: %v", r)
				}
			}()
			cb(podName, endpoint)
		}()
	}
}

func (m *Manager) handleDeploymentEvent(obj interface{}) {
	deployment, ok := obj.(*appsv1.Deployment)
	if !ok || deployment == nil {
		return
	}
	m.emitReplicaChange(buildReplicaEvent(deployment))
}

func (m *Manager) handleDeploymentUpdate(oldObj, newObj interface{}) {
	oldDep, oldOk := oldObj.(*appsv1.Deployment)
	newDep, newOk := newObj.(*appsv1.Deployment)

	if !oldOk || !newOk || oldDep == nil || newDep == nil {
		return
	}

	// Always emit replica change event
	m.emitReplicaChange(buildReplicaEvent(newDep))

	// Detect spec changes that trigger pod recreation (image, resources, env, etc.)
	// Ignore replica count changes as those are handled separately
	if m.hasSpecChanged(oldDep, newDep) {
		// Extract endpoint name from deployment labels
		endpoint := ""
		managedBy := ""
		if newDep.Labels != nil {
			endpoint = newDep.Labels["app"]
			managedBy = newDep.Labels["managed-by"]
		}

		// Only handle deployments managed by waverless (worker deployments)
		if endpoint != "" && managedBy == "waverless" && endpoint != "waverless" {
			logger.InfoCtx(context.Background(), "🔄 Deployment %s (endpoint: %s) spec changed, triggering optimized rolling update",
				newDep.Name, endpoint)
			m.notifyDeploymentSpecChange(endpoint)
		}
	}
}

func (m *Manager) handleDeploymentDelete(obj interface{}) {
	switch v := obj.(type) {
	case *appsv1.Deployment:
		if v != nil {
			m.emitReplicaChange(interfaces.ReplicaEvent{
				Name:              v.Name,
				DesiredReplicas:   0,
				ReadyReplicas:     0,
				AvailableReplicas: 0,
				Conditions:        deletedCondition("Deleted"),
			})
		}
	case cache.DeletedFinalStateUnknown:
		if dep, ok := v.Obj.(*appsv1.Deployment); ok && dep != nil {
			m.emitReplicaChange(interfaces.ReplicaEvent{
				Name:              dep.Name,
				DesiredReplicas:   0,
				ReadyReplicas:     0,
				AvailableReplicas: 0,
				Conditions:        deletedCondition("Deleted"),
			})
		}
	}
}

// hasSpecChanged checks if deployment spec has changed in ways that trigger pod recreation
// Returns true if image, resources, env, volumes, etc. changed
// Ignores replica count changes as those don't trigger pod recreation
func (m *Manager) hasSpecChanged(oldDep, newDep *appsv1.Deployment) bool {
	// IMPORTANT: We cannot use Generation field because it increments for ANY spec change,
	// including replicas (which doesn't recreate pods). We must compare the pod template.

	// Method 1: Check if pod-template-hash changed
	// This label is automatically added by Deployment controller when template changes
	oldHash := ""
	newHash := ""
	if oldDep.Spec.Template.Labels != nil {
		oldHash = oldDep.Spec.Template.Labels["pod-template-hash"]
	}
	if newDep.Spec.Template.Labels != nil {
		newHash = newDep.Spec.Template.Labels["pod-template-hash"]
	}

	// If both have pod-template-hash and they differ, template changed
	if oldHash != "" && newHash != "" && oldHash != newHash {
		return true
	}

	// Method 2: Serialize and compare pod template spec
	// This catches all template changes (image, resources, env, volumes, etc.)
	oldTemplate, err1 := json.Marshal(oldDep.Spec.Template.Spec)
	newTemplate, err2 := json.Marshal(newDep.Spec.Template.Spec)

	if err1 != nil || err2 != nil {
		// If serialization fails, be conservative and assume change
		logger.WarnCtx(context.Background(), "failed to serialize pod template for comparison: %v, %v", err1, err2)
		return false // Don't trigger on serialization error
	}

	// Template spec changed = need to recreate pods
	return !bytes.Equal(oldTemplate, newTemplate)
}

func buildReplicaEvent(dep *appsv1.Deployment) interfaces.ReplicaEvent {
	event := interfaces.ReplicaEvent{
		Name:              dep.Name,
		DesiredReplicas:   int(getDesiredReplicas(dep)),
		ReadyReplicas:     int(dep.Status.ReadyReplicas),
		AvailableReplicas: int(dep.Status.AvailableReplicas),
		Conditions:        extractDeploymentConditions(dep.Status.Conditions),
	}
	return event
}

func getDesiredReplicas(dep *appsv1.Deployment) int32 {
	if dep.Spec.Replicas == nil {
		return 0
	}
	return *dep.Spec.Replicas
}

func extractDeploymentConditions(conditions []appsv1.DeploymentCondition) []interfaces.ReplicaCondition {
	if len(conditions) == 0 {
		return nil
	}
	result := make([]interfaces.ReplicaCondition, 0, len(conditions))
	for _, cond := range conditions {
		result = append(result, interfaces.ReplicaCondition{
			Type:    string(cond.Type),
			Status:  string(cond.Status),
			Reason:  cond.Reason,
			Message: cond.Message,
		})
	}
	return result
}

func deletedCondition(reason string) []interfaces.ReplicaCondition {
	return []interfaces.ReplicaCondition{
		{
			Type:    "Deleted",
			Status:  "True",
			Reason:  reason,
			Message: "Deployment deleted",
		},
	}
}

func (m *Manager) emitReplicaChange(event interfaces.ReplicaEvent) {
	m.callbacksMu.RLock()
	if len(m.replicaCallbacks) == 0 {
		m.callbacksMu.RUnlock()
		return
	}
	callbacks := make([]interfaces.ReplicaCallback, 0, len(m.replicaCallbacks))
	for _, cb := range m.replicaCallbacks {
		callbacks = append(callbacks, cb)
	}
	m.callbacksMu.RUnlock()

	for _, cb := range callbacks {
		// fan out asynchronously to avoid blocking informer thread
		go cb(event)
	}
}

func (m *Manager) listDeploymentsViaAPI(ctx context.Context) []*AppInfo {
	result := make([]*AppInfo, 0)
	deployments, err := m.client.AppsV1().Deployments(m.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "managed-by=waverless",
	})
	if err != nil {
		logger.WarnCtx(ctx, "failed to list deployments via API: %v", err)
		return result
	}
	for i := range deployments.Items {
		deployment := &deployments.Items[i]
		result = append(result, deploymentToAppInfo(deployment))
	}
	return result
}

func (m *Manager) listPodsViaAPI(ctx context.Context) []*AppInfo {
	result := make([]*AppInfo, 0)
	pods, err := m.client.CoreV1().Pods(m.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "managed-by=waverless",
	})
	if err != nil {
		logger.WarnCtx(ctx, "failed to list pods via API: %v", err)
		return result
	}
	for i := range pods.Items {
		pod := &pods.Items[i]
		if len(pod.OwnerReferences) > 0 {
			continue
		}
		result = append(result, podToAppInfo(pod))
	}
	return result
}

func deploymentToAppInfo(deployment *appsv1.Deployment) *AppInfo {
	info := &AppInfo{
		Name:              deployment.Name,
		Namespace:         deployment.Namespace,
		Type:              "Deployment",
		Replicas:          *deployment.Spec.Replicas,
		ReadyReplicas:     deployment.Status.ReadyReplicas,
		AvailableReplicas: deployment.Status.AvailableReplicas,
		Labels:            deployment.Labels,
		CreatedAt:         deployment.CreationTimestamp.Format(time.RFC3339),
	}

	if len(deployment.Spec.Template.Spec.Containers) > 0 {
		info.Image = deployment.Spec.Template.Spec.Containers[0].Image
	}

	if deployment.Status.AvailableReplicas == *deployment.Spec.Replicas {
		info.Status = "Running"
	} else {
		info.Status = "Pending"
	}

	return info
}

func podToAppInfo(pod *corev1.Pod) *AppInfo {
	info := &AppInfo{
		Name:      pod.Name,
		Namespace: pod.Namespace,
		Type:      "Pod",
		Status:    string(pod.Status.Phase),
		PodIP:     pod.Status.PodIP,
		HostIP:    pod.Status.HostIP,
		Labels:    pod.Labels,
		CreatedAt: pod.CreationTimestamp.Format(time.RFC3339),
	}

	if len(pod.Spec.Containers) > 0 {
		info.Image = pod.Spec.Containers[0].Image
	}

	return info
}

// DeleteApp deletes an application
func (m *Manager) DeleteApp(ctx context.Context, name string) error {
	// Delete Deployment (which will automatically delete managed Pods)
	err := m.client.AppsV1().Deployments(m.namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete deployment: %v", err)
	}

	// Try to delete Service (if exists)
	err = m.client.CoreV1().Services(m.namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		// Log warning but don't fail - service might not exist
		fmt.Printf("Warning: failed to delete service %s: %v\n", name, err)
	}

	// Try to delete PVC (if exists) - best effort, don't fail on permission errors
	pvcName := fmt.Sprintf("%s-pvc", name)
	err = m.client.CoreV1().PersistentVolumeClaims(m.namespace).Delete(ctx, pvcName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		// Log warning but don't fail - PVC might not exist or we may not have permission
		fmt.Printf("Warning: failed to delete PVC %s: %v\n", pvcName, err)
	}

	// Try to delete PV (if exists) - best effort, don't fail on permission errors
	pvName := fmt.Sprintf("%s-pv", name)
	err = m.client.CoreV1().PersistentVolumes().Delete(ctx, pvName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		// Log warning but don't fail - PV might not exist or we may not have permission
		fmt.Printf("Warning: failed to delete PV %s: %v\n", pvName, err)
	}

	return nil
}

// GetAppLogs gets application logs
func (m *Manager) GetAppLogs(ctx context.Context, name string, tailLines int64, specificPodName ...string) (string, error) {
	var podName string

	// If specific pod name is provided, use it directly
	if len(specificPodName) > 0 && specificPodName[0] != "" {
		podName = specificPodName[0]
	} else {
		// Check if it's a Deployment first (use Informer cache)
		deployment, err := m.deploymentLister.Deployments(m.namespace).Get(name)
		if err == nil {
			// Is Deployment, find a Pod (use Informer cache)
			selector := labels.SelectorFromSet(labels.Set{"app": deployment.Name})
			pods, err := m.podLister.Pods(m.namespace).List(selector)
			if err != nil {
				return "", fmt.Errorf("failed to list pods for deployment: %v", err)
			}
			if len(pods) == 0 {
				return "", fmt.Errorf("no pods found for deployment %s", name)
			}
			podName = pods[0].Name
		} else if !errors.IsNotFound(err) {
			return "", fmt.Errorf("failed to get deployment: %v", err)
		} else {
			// Not a Deployment, use name directly as Pod name
			podName = name
		}
	}

	// Get Pod logs - specify container name as {endpoint}-worker
	containerName := fmt.Sprintf("%s-worker", name)
	logReq := m.client.CoreV1().Pods(m.namespace).GetLogs(podName, &corev1.PodLogOptions{
		Container: containerName,
		TailLines: &tailLines,
	})

	logs, err := logReq.Stream(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get pod logs: %v", err)
	}
	defer logs.Close()

	buf := make([]byte, 1024*1024) // 1MB buffer
	n, err := io.ReadFull(logs, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return "", fmt.Errorf("failed to read pod logs: %v", err)
	}

	return string(buf[:n]), nil
}

// ListSpecs lists all specs
func (m *Manager) ListSpecs() []*ResourceSpec {
	return m.specManager.ListSpecs()
}

// GetSpec gets spec details
func (m *Manager) GetSpec(name string) (*ResourceSpec, error) {
	return m.specManager.GetSpec(name)
}

// UpdateDeployment updates deployment
func (m *Manager) UpdateDeployment(ctx context.Context, endpoint string, specName string, image string, replicas *int) error {
	deployments := m.client.AppsV1().Deployments(m.namespace)

	// Get existing deployment
	deployment, err := deployments.Get(ctx, endpoint, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get deployment: %v", err)
	}

	// Update spec if provided
	if specName != "" {
		spec, err := m.specManager.GetSpec(specName)
		if err != nil {
			return fmt.Errorf("failed to get spec %s: %v", specName, err)
		}

		if len(deployment.Spec.Template.Spec.Containers) > 0 {
			// Build ResourceRequirements from SpecResources
			resources := corev1.ResourceRequirements{
				Requests: corev1.ResourceList{},
				Limits:   corev1.ResourceList{},
			}

			// Memory is always set
			if spec.Resources.Memory != "" {
				memoryQuantity := resource.MustParse(spec.Resources.Memory)
				resources.Requests[corev1.ResourceMemory] = memoryQuantity
				resources.Limits[corev1.ResourceMemory] = memoryQuantity
			}

			// CPU is optional (empty means unlimited)
			if spec.Resources.CPU != "" {
				cpuQuantity := resource.MustParse(spec.Resources.CPU)
				resources.Requests[corev1.ResourceCPU] = cpuQuantity
				resources.Limits[corev1.ResourceCPU] = cpuQuantity
			}

			// GPU is optional
			if spec.Resources.GPU != "" && spec.Category == "gpu" {
				gpuQuantity := resource.MustParse(spec.Resources.GPU)
				resources.Requests["nvidia.com/gpu"] = gpuQuantity
				resources.Limits["nvidia.com/gpu"] = gpuQuantity
			}

			// Update container resources
			deployment.Spec.Template.Spec.Containers[0].Resources = resources

			// Update spec label
			if deployment.Spec.Template.Labels == nil {
				deployment.Spec.Template.Labels = make(map[string]string)
			}
			deployment.Spec.Template.Labels["waverless.io/spec"] = specName

			// Apply platform-specific configuration (Tolerations, NodeSelector, Labels, Annotations)
			platformConfig := spec.GetPlatformConfig(m.platform.GetName())

			// 1. Update Tolerations (replace entirely to remove old tolerations)
			// Convert from spec.Toleration to corev1.Toleration
			tolerations := make([]corev1.Toleration, len(platformConfig.Tolerations))
			for i, t := range platformConfig.Tolerations {
				tolerations[i] = corev1.Toleration{
					Key:      t.Key,
					Operator: corev1.TolerationOperator(t.Operator),
					Value:    t.Value,
					Effect:   corev1.TaintEffect(t.Effect),
				}
			}
			deployment.Spec.Template.Spec.Tolerations = tolerations

			// 2. Update NodeSelector (replace entirely)
			deployment.Spec.Template.Spec.NodeSelector = platformConfig.NodeSelector

			// 3. Update Pod Labels (smart merge: remove old platform labels, keep system labels, apply new platform labels)
			if deployment.Spec.Template.Labels == nil {
				deployment.Spec.Template.Labels = make(map[string]string)
			}

			// Read previous platform labels record
			var oldPlatformLabels map[string]string
			if oldLabelsJSON, exists := deployment.Annotations["waverless.io/platform-labels"]; exists {
				json.Unmarshal([]byte(oldLabelsJSON), &oldPlatformLabels)
			}

			// Remove old platform labels (precise deletion)
			for key := range oldPlatformLabels {
				delete(deployment.Spec.Template.Labels, key)
			}

			// Apply new platform labels (filter out waverless.io/* system labels)
			filteredNewLabels := make(map[string]string)
			for k, v := range platformConfig.Labels {
				if !strings.HasPrefix(k, "waverless.io/") {
					deployment.Spec.Template.Labels[k] = v
					filteredNewLabels[k] = v
				}
			}

			// Ensure system labels exist
			deployment.Spec.Template.Labels["waverless.io/spec"] = specName

			// Record current platform labels (for deletion on next update)
			if len(filteredNewLabels) > 0 {
				newLabelsJSON, _ := json.Marshal(filteredNewLabels)
				if deployment.Annotations == nil {
					deployment.Annotations = make(map[string]string)
				}
				deployment.Annotations["waverless.io/platform-labels"] = string(newLabelsJSON)
			} else {
				// Clear record if new spec has no platform labels
				delete(deployment.Annotations, "waverless.io/platform-labels")
			}

			// 4. Update Pod Annotations (smart merge: remove old platform annotations, keep system annotations, apply new platform annotations)
			if deployment.Spec.Template.Annotations == nil {
				deployment.Spec.Template.Annotations = make(map[string]string)
			}

			// Read previous platform annotations record
			var oldPlatformAnnotations map[string]string
			if oldAnnosJSON, exists := deployment.Annotations["waverless.io/platform-annotations"]; exists {
				json.Unmarshal([]byte(oldAnnosJSON), &oldPlatformAnnotations)
			}

			// Remove old platform annotations (precise deletion)
			for key := range oldPlatformAnnotations {
				delete(deployment.Spec.Template.Annotations, key)
			}

			// Apply new platform annotations (filter out waverless.io/* system annotations)
			filteredNewAnnotations := make(map[string]string)
			for k, v := range platformConfig.Annotations {
				if !strings.HasPrefix(k, "waverless.io/") {
					deployment.Spec.Template.Annotations[k] = v
					filteredNewAnnotations[k] = v
				}
			}

			// Record current platform annotations (for deletion on next update)
			if len(filteredNewAnnotations) > 0 {
				newAnnosJSON, _ := json.Marshal(filteredNewAnnotations)
				deployment.Annotations["waverless.io/platform-annotations"] = string(newAnnosJSON)
			} else {
				// Clear record if new spec has no platform annotations
				delete(deployment.Annotations, "waverless.io/platform-annotations")
			}
		}
	}

	// Update image if provided
	if image != "" {
		if len(deployment.Spec.Template.Spec.Containers) > 0 {
			deployment.Spec.Template.Spec.Containers[0].Image = image
		}
	}

	// Update replicas if provided
	if replicas != nil {
		r := int32(*replicas)
		deployment.Spec.Replicas = &r
	}

	// Update deployment
	_, err = deployments.Update(ctx, deployment, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update deployment: %v", err)
	}

	return nil
}

// ScaleDeployment updates the desired replica count for an endpoint.
func (m *Manager) ScaleDeployment(ctx context.Context, endpoint string, replicas int) error {
	if replicas < 0 {
		return fmt.Errorf("replicas cannot be negative")
	}

	deployments := m.client.AppsV1().Deployments(m.namespace)
	deployment, err := deployments.Get(ctx, endpoint, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get deployment: %v", err)
	}

	r := int32(replicas)
	deployment.Spec.Replicas = &r

	if _, err := deployments.Update(ctx, deployment, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("failed to scale deployment: %v", err)
	}
	return nil
}

// IsPodDraining checks if a pod is marked as draining
// podName: name of the pod to check (e.g., from HOSTNAME env var)
// Uses Informer cache to reduce API server pressure
func (m *Manager) IsPodDraining(ctx context.Context, podName string) (bool, error) {
	if podName == "" {
		return false, nil
	}

	// Get pod from Informer cache
	pod, err := m.podLister.Pods(m.namespace).Get(podName)
	if err != nil {
		if errors.IsNotFound(err) {
			// Pod not found, not draining
			return false, nil
		}
		return false, fmt.Errorf("failed to get pod from cache: %v", err)
	}

	// Check if pod has draining label
	if pod.Labels != nil {
		if draining, exists := pod.Labels["waverless.io/draining"]; exists && draining == "true" {
			return true, nil
		}
	}

	return false, nil
}

// IsPodTerminating checks if a pod is marked for deletion (DeletionTimestamp set)
// This is used as a safety net to prevent Terminating pods from pulling tasks
// even if the Pod Watcher callback hasn't fired yet
// Uses Informer cache to reduce API server pressure
func (m *Manager) IsPodTerminating(ctx context.Context, podName string) (bool, error) {
	if podName == "" {
		return false, nil
	}

	// Get pod from Informer cache
	pod, err := m.podLister.Pods(m.namespace).Get(podName)
	if err != nil {
		if errors.IsNotFound(err) {
			// Pod not found, consider it as terminating (already deleted)
			return true, nil
		}
		return false, fmt.Errorf("failed to get pod from cache: %v", err)
	}

	// Check if pod has DeletionTimestamp set (K8s marks pod for deletion)
	return pod.DeletionTimestamp != nil, nil
}

// MarkPodsAsDraining marks the oldest N pods of an endpoint as draining
// Uses Informer cache for listing pods to reduce API server pressure
func (m *Manager) MarkPodsAsDraining(ctx context.Context, endpoint string, count int) error {
	if count <= 0 {
		return nil
	}

	// List pods from Informer cache
	labelSelector := labels.SelectorFromSet(labels.Set{"waverless.io/endpoint": endpoint})
	cachedPods, err := m.podLister.Pods(m.namespace).List(labelSelector)
	if err != nil {
		return fmt.Errorf("failed to list pods from cache: %v", err)
	}

	if len(cachedPods) == 0 {
		return nil
	}

	// Sort by creation timestamp (oldest first)
	// We'll select the oldest pods to drain
	oldestPods := make([]*corev1.Pod, 0, count)
	for i := 0; i < len(cachedPods) && i < count; i++ {
		pod := cachedPods[i]

		// Skip pods already draining
		if pod.Labels != nil {
			if draining, exists := pod.Labels["waverless.io/draining"]; exists && draining == "true" {
				continue
			}
		}

		oldestPods = append(oldestPods, pod)
	}

	// Mark pods as draining (must use API for updates)
	pods := m.client.CoreV1().Pods(m.namespace)
	for _, pod := range oldestPods {
		if pod.Labels == nil {
			pod.Labels = make(map[string]string)
		}
		pod.Labels["waverless.io/draining"] = "true"

		_, err := pods.Update(ctx, pod, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("failed to update pod %s: %v", pod.Name, err)
		}
	}

	return nil
}

// MarkPodDraining marks a specific pod as draining (for smart scale-down)
// This prevents the pod from pulling new tasks while waiting for current tasks to finish
func (m *Manager) MarkPodDraining(ctx context.Context, podName string) error {
	if podName == "" {
		return fmt.Errorf("pod name cannot be empty")
	}

	pods := m.client.CoreV1().Pods(m.namespace)
	pod, err := pods.Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get pod %s: %w", podName, err)
	}

	// Add draining label
	if pod.Labels == nil {
		pod.Labels = make(map[string]string)
	}
	pod.Labels["waverless.io/draining"] = "true"

	_, err = pods.Update(ctx, pod, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update pod %s: %w", podName, err)
	}

	return nil
}

// SetPodDeletionCost sets the pod deletion cost annotation
// This tells Kubernetes Deployment controller to preferentially delete this pod during scale-down
// Lower cost = higher priority for deletion (default is 0, we use -1000 for draining pods)
func (m *Manager) SetPodDeletionCost(ctx context.Context, podName string, cost int) error {
	if podName == "" {
		return fmt.Errorf("pod name cannot be empty")
	}

	pods := m.client.CoreV1().Pods(m.namespace)
	pod, err := pods.Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get pod %s: %w", podName, err)
	}

	// Set pod deletion cost annotation
	if pod.Annotations == nil {
		pod.Annotations = make(map[string]string)
	}
	pod.Annotations["controller.kubernetes.io/pod-deletion-cost"] = fmt.Sprintf("%d", cost)

	_, err = pods.Update(ctx, pod, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update pod %s annotations: %w", podName, err)
	}

	return nil
}

// DeletePod gracefully deletes a specific pod
// Used after draining is complete to remove the pod from the cluster
func (m *Manager) DeletePod(ctx context.Context, podName string) error {
	if podName == "" {
		return fmt.Errorf("pod name cannot be empty")
	}

	pods := m.client.CoreV1().Pods(m.namespace)

	// Use grace period of 30 seconds
	gracePeriodSeconds := int64(30)
	deleteOptions := metav1.DeleteOptions{
		GracePeriodSeconds: &gracePeriodSeconds,
	}

	err := pods.Delete(ctx, podName, deleteOptions)
	if err != nil {
		if errors.IsNotFound(err) {
			// Pod already deleted, not an error
			return nil
		}
		return fmt.Errorf("failed to delete pod %s: %w", podName, err)
	}

	return nil
}

// ForceDeletePod immediately deletes a pod with zero grace period (SIGKILL)
// This should only be used as a safety net when:
// 1. Pod is in Terminating state for too long
// 2. Database confirms the worker has no running tasks
// 3. Worker is not responding to SIGTERM
func (m *Manager) ForceDeletePod(ctx context.Context, podName string) error {
	if podName == "" {
		return fmt.Errorf("pod name cannot be empty")
	}

	pods := m.client.CoreV1().Pods(m.namespace)

	// Force delete with grace period of 0 (immediate SIGKILL)
	gracePeriodSeconds := int64(0)
	deleteOptions := metav1.DeleteOptions{
		GracePeriodSeconds: &gracePeriodSeconds,
	}

	err := pods.Delete(ctx, podName, deleteOptions)
	if err != nil {
		if errors.IsNotFound(err) {
			// Pod already deleted, not an error
			return nil
		}
		return fmt.Errorf("failed to force delete pod %s: %w", podName, err)
	}

	logger.WarnCtx(ctx, "⚠️ Force deleted pod %s with grace period 0 (SIGKILL)", podName)
	return nil
}

// GetRestConfig returns the Kubernetes REST config for exec/attach operations
func (m *Manager) GetRestConfig() *rest.Config {
	return m.config
}

// GetNamespace returns the namespace this manager operates in
func (m *Manager) GetNamespace() string {
	return m.namespace
}
