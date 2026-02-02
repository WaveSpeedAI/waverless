package k8s

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	appslisters "k8s.io/client-go/listers/apps/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/yaml"

	"waverless/pkg/constants"
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
	client        kubernetes.Interface
	dynamicClient dynamic.Interface
	config        *rest.Config
	namespace     string
	platform      Platform
	specManager   *SpecManager
	renderer      *TemplateRenderer
	globalEnv     map[string]string

	informerFactory  informers.SharedInformerFactory
	deploymentLister appslisters.DeploymentLister
	podLister        corelisters.PodLister
	informerStopCh   chan struct{}
	stopOnce         sync.Once

	callbacksMu                     sync.RWMutex
	replicaCallbacks                map[int64]interfaces.ReplicaCallback
	podTerminatingCallbacks         map[int64]PodTerminatingCallback
	podDeleteCallbacks              map[int64]PodDeleteCallback
	podStatusChangeCallbacks        map[int64]PodStatusChangeCallback
	spotInterruptionCallbacks       map[int64]SpotInterruptionCallback
	deploymentSpecChangeCallbacks   map[int64]DeploymentSpecChangeCallback
	deploymentStatusChangeCallbacks map[int64]DeploymentStatusChangeCallback
	nextCallbackID                  int64
}

// PodTerminatingCallback is called when a pod is marked for deletion (DeletionTimestamp set)
// This allows the system to drain workers before pods are actually terminated
type PodTerminatingCallback func(podName, endpoint string)

// PodDeleteCallback is called when a pod is deleted from K8s
type PodDeleteCallback func(podName, endpoint string)

// PodStatusChangeCallback is called when a pod's status changes
// This allows the system to sync pod status to worker database
type PodStatusChangeCallback func(podName, endpoint string, info *interfaces.PodInfo)

// SpotInterruptionCallback is called when a spot interruption is detected
type SpotInterruptionCallback func(podName, endpoint, reason string)

// DeploymentSpecChangeCallback is called when a deployment's spec changes (image, resources, etc.)
// This allows the system to optimize pod replacement during rolling updates
type DeploymentSpecChangeCallback func(endpoint string)

// DeploymentStatusChangeCallback is called when a deployment's status changes (replicas ready, etc.)
// This allows the system to sync deployment status to database
type DeploymentStatusChangeCallback func(endpoint string, deployment *appsv1.Deployment)

// NewManager creates a K8s manager
func NewManager(namespace, platformName, configDir string, globalEnv map[string]string) (*Manager, error) {
	// Create K8s client
	var config *rest.Config
	var err error

	// Check if running in cluster by looking for service account token
	if _, err := os.Stat("/var/run/secrets/kubernetes.io/serviceaccount/token"); err == nil {
		// Running in cluster, use InClusterConfig
		config, err = rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to get in-cluster config: %v", err)
		}
	} else {
		// Running outside cluster, use kubeconfig
		loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
		configOverrides := &clientcmd.ConfigOverrides{}
		kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
		config, err = kubeConfig.ClientConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to get kubeconfig: %v", err)
		}
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %v", err)
	}

	// Create dynamic client for CRDs (e.g., Karpenter NodeClaim)
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %v", err)
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
		dynamicClient:                 dynamicClient,
		config:                        config,
		namespace:                     namespace,
		platform:                      platform,
		specManager:                   specManager,
		renderer:                      renderer,
		globalEnv:                     globalEnv,
		informerFactory:               informerFactory,
		deploymentLister:              deploymentInformer.Lister(),
		podLister:                     podInformer.Lister(),
		informerStopCh:                stopCh,
		replicaCallbacks:              make(map[int64]interfaces.ReplicaCallback),
		podTerminatingCallbacks:       make(map[int64]PodTerminatingCallback),
		podDeleteCallbacks:            make(map[int64]PodDeleteCallback),
		podStatusChangeCallbacks:      make(map[int64]PodStatusChangeCallback),
		spotInterruptionCallbacks:     make(map[int64]SpotInterruptionCallback),
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
			manager.handlePodAdd(obj)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			logger.DebugCtx(context.Background(), "pod updated (informer cache)")
			manager.handlePodUpdate(oldObj, newObj)
		},
		DeleteFunc: func(obj interface{}) {
			logger.DebugCtx(context.Background(), "pod deleted (informer cache)")
			manager.handlePodDelete(obj)
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
	Endpoint        string                   `json:"endpoint" binding:"required"` // Endpoint name
	SpecName        string                   `json:"specName" binding:"required"` // Spec name
	Image           string                   `json:"image" binding:"required"`    // Image
	ImagePrefix     string                   `json:"imagePrefix,omitempty"`       // Image prefix for matching updates (e.g., "wavespeed/model-deploy:wan_i2v-default-")
	Replicas        int                      `json:"replicas,omitempty"`          // Replica count (default 1)
	GpuCount        int                      `json:"gpuCount,omitempty"`          // GPU count (1-N, resources = per-gpu-config * gpuCount)
	TaskTimeout     int                      `json:"taskTimeout,omitempty"`       // Task execution timeout in seconds (0 = use global default)
	MaxPendingTasks int                      `json:"maxPendingTasks,omitempty"`   // Maximum allowed pending tasks before warning clients (default 1)
	VolumeMounts    []interfaces.VolumeMount `json:"volumeMounts,omitempty"`      // PVC volume mounts
	ShmSize         string                   `json:"shmSize,omitempty"`           // Shared memory size (e.g., "1Gi", "512Mi")
	EnablePtrace    bool                     `json:"enablePtrace,omitempty"`      // Enable SYS_PTRACE capability for debugging (only for fixed resource pools)
	ValidateImage   *bool                    `json:"validateImage,omitempty"`     // Whether to validate image before deployment (default: true)
	Env             map[string]string        `json:"env,omitempty"`               // Custom environment variables

	// Registry credential for private images
	RegistryCredential *RegistryCredential `json:"registryCredential,omitempty"`

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

// RegistryCredential for private container registries
type RegistryCredential struct {
	Registry string `json:"registry"` // e.g., "docker.io", "ghcr.io"
	Username string `json:"username"`
	Password string `json:"password"`
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

	// Create registry secret if credential provided
	var imagePullSecretName string
	if req.RegistryCredential != nil {
		imagePullSecretName = fmt.Sprintf("registry-%s", req.Endpoint)
		if err := m.createRegistrySecret(ctx, imagePullSecretName, req.RegistryCredential); err != nil {
			return fmt.Errorf("failed to create registry secret: %w", err)
		}
	}

	// Build render context
	renderCtx, err := m.buildRenderContext(req, spec)
	if err != nil {
		return err
	}
	renderCtx.ImagePullSecret = imagePullSecretName

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
	platformName := m.platform.GetName()
	platformConfig := spec.GetPlatformConfig(platformName)

	// Debug logging
	logger.InfoCtx(context.Background(), "[SPEC-CONVERT] buildRenderContext: platform=%s, spec.Platforms=%+v", platformName, spec.Platforms)
	logger.InfoCtx(context.Background(), "[SPEC-CONVERT] platformConfig: NodeSelector=%+v, Tolerations=%+v", platformConfig.NodeSelector, platformConfig.Tolerations)

	// Ensure NodeSelector is initialized (not nil) to avoid template rendering issues
	nodeSelector := platformConfig.NodeSelector
	if nodeSelector == nil {
		nodeSelector = make(map[string]string)
	}

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
		NodeSelector: nodeSelector,
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

	// GPU count: use request gpuCount if specified, otherwise use spec default
	if ctx.IsGpu {
		var maxGpu int
		fmt.Sscanf(spec.Resources.GPU, "%d", &maxGpu)

		if req.GpuCount > 0 {
			// Validate: requested gpuCount must not exceed spec's max
			if req.GpuCount > maxGpu {
				return nil, fmt.Errorf("requested gpuCount %d exceeds spec max %d", req.GpuCount, maxGpu)
			}
			ctx.GpuCount = req.GpuCount
		} else {
			// Default to 1 GPU if not specified
			ctx.GpuCount = 1
		}

		// Scale resources by gpuCount (spec defines per-GPU resources)
		ctx.CpuLimit = multiplyResource(spec.Resources.CPU, ctx.GpuCount)
		ctx.MemoryRequest = multiplyResource(spec.Resources.Memory, ctx.GpuCount)
	}

	// Calculate termination grace period
	// Formula: taskTimeout + 30s buffer for cleanup
	// Default: 330s (300s task timeout + 30s buffer)
	taskTimeout := ctx.TaskTimeout
	if taskTimeout == 0 {
		taskTimeout = 3600
	}
	ctx.TerminationGracePeriodSeconds = int64(taskTimeout + 30)

	// Process volume mounts
	if len(req.VolumeMounts) > 0 {
		ctx.Volumes = make([]VolumeInfo, len(req.VolumeMounts))
		ctx.VolumeMounts = make([]VolumeMountInfo, len(req.VolumeMounts))
		for i, vm := range req.VolumeMounts {
			// Use pvc name as volume name (replace special chars to make it k8s-safe)
			volumeName := fmt.Sprintf("pvc-%d", i)
			ctx.Volumes[i] = VolumeInfo{
				Name:    volumeName,
				PVCName: vm.PVCName,
			}
			ctx.VolumeMounts[i] = VolumeMountInfo{
				Name:      volumeName,
				MountPath: vm.MountPath,
			}
		}
	}

	// Shared memory size
	// Priority: request.ShmSize > spec.ShmSize
	if req.ShmSize != "" {
		ctx.ShmSize = req.ShmSize
	} else if spec.Resources.ShmSize != "" {
		ctx.ShmSize = spec.Resources.ShmSize
	}

	// Enable ptrace capability (only for fixed resource pools)
	ctx.EnablePtrace = req.EnablePtrace

	// Environment variables: merge globalEnv with request env (request takes precedence)
	ctx.Env = make(map[string]string)
	for k, v := range m.globalEnv {
		// Replace placeholders in globalEnv values
		v = strings.ReplaceAll(v, "{{.Endpoint}}", req.Endpoint)
		ctx.Env[k] = v
	}
	for k, v := range req.Env {
		ctx.Env[k] = v
	}

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

// createRegistrySecret creates a docker-registry type secret for private image pulls
func (m *Manager) createRegistrySecret(ctx context.Context, name string, cred *RegistryCredential) error {
	// Build docker config JSON
	registry := cred.Registry
	if registry == "" {
		registry = "https://index.docker.io/v1/"
	} else if !strings.HasPrefix(registry, "http") {
		registry = "https://" + registry
	}

	dockerConfig := map[string]interface{}{
		"auths": map[string]interface{}{
			registry: map[string]string{
				"username": cred.Username,
				"password": cred.Password,
			},
		},
	}
	configJSON, _ := json.Marshal(dockerConfig)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: m.namespace,
		},
		Type: corev1.SecretTypeDockerConfigJson,
		Data: map[string][]byte{
			corev1.DockerConfigJsonKey: configJSON,
		},
	}

	secrets := m.client.CoreV1().Secrets(m.namespace)
	existing, err := secrets.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
		_, err = secrets.Create(ctx, secret, metav1.CreateOptions{})
		return err
	}

	// Update existing
	secret.ResourceVersion = existing.ResourceVersion
	_, err = secrets.Update(ctx, secret, metav1.UpdateOptions{})
	return err
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
	Name              string                   `json:"name"`
	Namespace         string                   `json:"namespace"`
	Type              string                   `json:"type"` // Pod or Deployment
	Status            string                   `json:"status"`
	Replicas          int32                    `json:"replicas,omitempty"`          // Deployment desired replica count
	ReadyReplicas     int32                    `json:"readyReplicas,omitempty"`     // Deployment ready replica count
	AvailableReplicas int32                    `json:"availableReplicas,omitempty"` // Deployment available replica count
	PodIP             string                   `json:"podIp,omitempty"`
	HostIP            string                   `json:"hostIp,omitempty"`
	Image             string                   `json:"image"`
	Labels            map[string]string        `json:"labels"`
	CreatedAt         string                   `json:"createdAt"`
	ShmSize           string                   `json:"shmSize,omitempty"`      // Shared memory size from deployment volumes
	VolumeMounts      []interfaces.VolumeMount `json:"volumeMounts,omitempty"` // PVC volume mounts from deployment
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

// ListPVCs lists all PersistentVolumeClaims in the namespace
func (m *Manager) ListPVCs(ctx context.Context) ([]*interfaces.PVCInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	pvcList, err := m.client.CoreV1().PersistentVolumeClaims(m.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list PVCs: %w", err)
	}

	result := make([]*interfaces.PVCInfo, 0, len(pvcList.Items))
	for _, pvc := range pvcList.Items {
		// Get capacity
		capacity := ""
		if storage, ok := pvc.Status.Capacity[corev1.ResourceStorage]; ok {
			capacity = storage.String()
		}

		// Get access modes
		accessModes := ""
		if len(pvc.Status.AccessModes) > 0 {
			accessModes = string(pvc.Status.AccessModes[0])
		}

		// Get storage class
		storageClass := ""
		if pvc.Spec.StorageClassName != nil {
			storageClass = *pvc.Spec.StorageClassName
		}

		result = append(result, &interfaces.PVCInfo{
			Name:         pvc.Name,
			Namespace:    pvc.Namespace,
			Status:       string(pvc.Status.Phase),
			Volume:       pvc.Spec.VolumeName,
			Capacity:     capacity,
			AccessModes:  accessModes,
			StorageClass: storageClass,
			CreatedAt:    pvc.CreationTimestamp.Format(time.RFC3339),
		})
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

// RegisterPodDeleteCallback adds a new pod delete listener and returns its id.
func (m *Manager) RegisterPodDeleteCallback(cb PodDeleteCallback) int64 {
	if cb == nil {
		return 0
	}
	id := atomic.AddInt64(&m.nextCallbackID, 1)
	m.callbacksMu.Lock()
	if m.podDeleteCallbacks == nil {
		m.podDeleteCallbacks = make(map[int64]PodDeleteCallback)
	}
	m.podDeleteCallbacks[id] = cb
	m.callbacksMu.Unlock()
	return id
}

// UnregisterPodDeleteCallback removes a previously registered pod delete listener.
func (m *Manager) UnregisterPodDeleteCallback(id int64) {
	if id == 0 {
		return
	}
	m.callbacksMu.Lock()
	if m.podDeleteCallbacks != nil {
		delete(m.podDeleteCallbacks, id)
	}
	m.callbacksMu.Unlock()
}

// RegisterPodStatusChangeCallback adds a new pod status change listener and returns its id.
func (m *Manager) RegisterPodStatusChangeCallback(cb PodStatusChangeCallback) int64 {
	if cb == nil {
		return 0
	}
	id := atomic.AddInt64(&m.nextCallbackID, 1)
	m.callbacksMu.Lock()
	if m.podStatusChangeCallbacks == nil {
		m.podStatusChangeCallbacks = make(map[int64]PodStatusChangeCallback)
	}
	m.podStatusChangeCallbacks[id] = cb
	m.callbacksMu.Unlock()
	return id
}

// UnregisterPodStatusChangeCallback removes a previously registered pod status change listener.
func (m *Manager) UnregisterPodStatusChangeCallback(id int64) {
	if id == 0 {
		return
	}
	m.callbacksMu.Lock()
	if m.podStatusChangeCallbacks != nil {
		delete(m.podStatusChangeCallbacks, id)
	}
	m.callbacksMu.Unlock()
}

// RegisterSpotInterruptionCallback adds a new spot interruption listener and returns its id.
func (m *Manager) RegisterSpotInterruptionCallback(cb SpotInterruptionCallback) int64 {
	if cb == nil {
		return 0
	}
	id := atomic.AddInt64(&m.nextCallbackID, 1)
	m.callbacksMu.Lock()
	if m.spotInterruptionCallbacks == nil {
		m.spotInterruptionCallbacks = make(map[int64]SpotInterruptionCallback)
	}
	m.spotInterruptionCallbacks[id] = cb
	m.callbacksMu.Unlock()
	return id
}

// UnregisterSpotInterruptionCallback removes a previously registered spot interruption listener.
func (m *Manager) UnregisterSpotInterruptionCallback(id int64) {
	if id == 0 {
		return
	}
	m.callbacksMu.Lock()
	if m.spotInterruptionCallbacks != nil {
		delete(m.spotInterruptionCallbacks, id)
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

// RegisterDeploymentStatusChangeCallback adds a new deployment status change listener and returns its id.
func (m *Manager) RegisterDeploymentStatusChangeCallback(cb DeploymentStatusChangeCallback) int64 {
	if cb == nil {
		return 0
	}
	id := atomic.AddInt64(&m.nextCallbackID, 1)
	m.callbacksMu.Lock()
	if m.deploymentStatusChangeCallbacks == nil {
		m.deploymentStatusChangeCallbacks = make(map[int64]DeploymentStatusChangeCallback)
	}
	m.deploymentStatusChangeCallbacks[id] = cb
	m.callbacksMu.Unlock()
	return id
}

// UnregisterDeploymentStatusChangeCallback removes a previously registered deployment status change listener.
func (m *Manager) UnregisterDeploymentStatusChangeCallback(id int64) {
	if id == 0 {
		return
	}
	m.callbacksMu.Lock()
	if m.deploymentStatusChangeCallbacks != nil {
		delete(m.deploymentStatusChangeCallbacks, id)
	}
	m.callbacksMu.Unlock()
}

// notifyDeploymentStatusChange notifies all registered callbacks that a deployment status has changed
func (m *Manager) notifyDeploymentStatusChange(endpoint string, deployment *appsv1.Deployment) {
	m.callbacksMu.RLock()
	callbacks := make([]DeploymentStatusChangeCallback, 0, len(m.deploymentStatusChangeCallbacks))
	for _, cb := range m.deploymentStatusChangeCallbacks {
		callbacks = append(callbacks, cb)
	}
	m.callbacksMu.RUnlock()

	for _, cb := range callbacks {
		cb := cb
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.ErrorCtx(context.Background(), "Deployment status change callback panicked: %v", r)
				}
			}()
			cb(endpoint, deployment)
		}()
	}
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

// handlePodAdd handles pod add events and creates worker record
func (m *Manager) handlePodAdd(obj interface{}) {
	pod, ok := obj.(*corev1.Pod)
	if !ok || pod == nil {
		return
	}

	endpoint := GetPodEndpoint(pod)
	if endpoint == "" || !m.isManagedWorkerPod(pod) {
		return
	}

	podInfo := m.podToPodInfo(pod)
	m.notifyPodStatusChange(pod.Name, endpoint, podInfo)
}

// isManagedWorkerPod checks if pod is a managed worker pod (not the waverless service itself)
func (m *Manager) isManagedWorkerPod(pod *corev1.Pod) bool {
	if pod == nil || pod.Labels == nil {
		return false
	}
	endpoint := pod.Labels[constants.LabelApp]
	managedBy := pod.Labels[constants.LabelManagedBy]
	return endpoint != "" && managedBy == constants.ManagedByWaverless && endpoint != "waverless"
}

// handlePodDelete handles pod delete events
func (m *Manager) handlePodDelete(obj interface{}) {
	pod, ok := obj.(*corev1.Pod)
	if !ok || pod == nil {
		return
	}

	endpoint := GetPodEndpoint(pod)
	if endpoint == "" || !m.isManagedWorkerPod(pod) {
		return
	}

	logger.InfoCtx(context.Background(), "🗑️ Pod %s (endpoint: %s) deleted", pod.Name, endpoint)
	m.notifyPodDelete(pod.Name, endpoint)
}

// notifyPodDelete notifies all registered callbacks about pod deletion
func (m *Manager) notifyPodDelete(podName, endpoint string) {
	m.callbacksMu.RLock()
	callbacks := make([]PodDeleteCallback, 0, len(m.podDeleteCallbacks))
	for _, cb := range m.podDeleteCallbacks {
		callbacks = append(callbacks, cb)
	}
	m.callbacksMu.RUnlock()

	for _, cb := range callbacks {
		cb := cb
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.ErrorCtx(context.Background(), "Pod delete callback panicked: %v", r)
				}
			}()
			cb(podName, endpoint)
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

	// Extract pod info
	podName := newPod.Name
	endpoint := GetPodEndpoint(newPod)

	// Only handle pods managed by waverless (worker pods)
	if endpoint == "" || !m.isManagedWorkerPod(newPod) {
		return
	}

	// 1. Check for Spot interruption FIRST (before pod termination)
	if detected, reason := m.detectSpotInterruption(newPod); detected {
		logger.WarnCtx(context.Background(), "🚨 Spot interruption detected for pod %s (endpoint: %s): %s",
			podName, endpoint, reason)
		m.notifySpotInterruption(podName, endpoint, reason)
	}

	// 2. Detect when a pod is marked for deletion
	if oldPod.DeletionTimestamp == nil && newPod.DeletionTimestamp != nil {
		logger.InfoCtx(context.Background(), "🔔 Pod %s (endpoint: %s) marked for deletion, notifying callbacks",
			podName, endpoint)
		m.notifyPodTerminating(podName, endpoint)
	}

	// 3. Notify pod status change (for worker runtime state sync)
	podInfo := m.podToPodInfo(newPod)
	m.notifyPodStatusChange(podName, endpoint, podInfo)
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

// notifyPodStatusChange notifies all registered callbacks about pod status change
func (m *Manager) notifyPodStatusChange(podName, endpoint string, info *interfaces.PodInfo) {
	m.callbacksMu.RLock()
	callbacks := make([]PodStatusChangeCallback, 0, len(m.podStatusChangeCallbacks))
	for _, cb := range m.podStatusChangeCallbacks {
		callbacks = append(callbacks, cb)
	}
	m.callbacksMu.RUnlock()

	for _, cb := range callbacks {
		cb := cb
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.ErrorCtx(context.Background(), "Pod status change callback panicked: %v", r)
				}
			}()
			cb(podName, endpoint, info)
		}()
	}
}

// podToPodInfo converts a K8s Pod to PodInfo
func (m *Manager) podToPodInfo(pod *corev1.Pod) *interfaces.PodInfo {
	info := &interfaces.PodInfo{
		Name:      pod.Name,
		Phase:     string(pod.Status.Phase),
		IP:        pod.Status.PodIP,
		NodeName:  pod.Spec.NodeName,
		CreatedAt: pod.CreationTimestamp.Format(time.RFC3339),
	}
	if pod.DeletionTimestamp != nil {
		info.DeletionTimestamp = pod.DeletionTimestamp.Format(time.RFC3339)
	}
	// Get container status
	for _, cs := range pod.Status.ContainerStatuses {
		info.RestartCount += cs.RestartCount
		if cs.State.Running != nil {
			info.Status = "Running"
			info.StartedAt = cs.State.Running.StartedAt.Format(time.RFC3339)
		} else if cs.State.Waiting != nil {
			info.Status = cs.State.Waiting.Reason
			info.Reason = cs.State.Waiting.Reason
			info.Message = cs.State.Waiting.Message
		} else if cs.State.Terminated != nil {
			info.Status = "Terminated"
			info.Reason = cs.State.Terminated.Reason
			info.Message = cs.State.Terminated.Message
		}
	}
	// Check conditions for Ready
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
			info.Reason = "Ready"
			info.Message = "All containers are ready"
		}
	}
	return info
}

// detectSpotInterruption checks if a pod is experiencing spot interruption
func (m *Manager) detectSpotInterruption(pod *corev1.Pod) (bool, string) {
	if m.platform == nil {
		return false, ""
	}
	return m.platform.DetectSpotInterruption(pod)
}

// notifySpotInterruption notifies all registered callbacks about spot interruption
func (m *Manager) notifySpotInterruption(podName, endpoint, reason string) {
	m.callbacksMu.RLock()
	callbacks := make([]SpotInterruptionCallback, 0, len(m.spotInterruptionCallbacks))
	for _, cb := range m.spotInterruptionCallbacks {
		callbacks = append(callbacks, cb)
	}
	m.callbacksMu.RUnlock()

	// Fan out asynchronously
	for _, cb := range callbacks {
		cb := cb
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.ErrorCtx(context.Background(), "spot interruption callback panic: %v", r)
				}
			}()
			cb(podName, endpoint, reason)
		}()
	}
}

func (m *Manager) handleDeploymentEvent(obj interface{}) {
	deployment, ok := obj.(*appsv1.Deployment)
	if !ok || deployment == nil {
		return
	}
	m.emitReplicaChange(buildReplicaEvent(deployment))
	m.syncDeploymentStatus(deployment)
}

func (m *Manager) handleDeploymentUpdate(oldObj, newObj interface{}) {
	oldDep, oldOk := oldObj.(*appsv1.Deployment)
	newDep, newOk := newObj.(*appsv1.Deployment)

	if !oldOk || !newOk || oldDep == nil || newDep == nil {
		return
	}

	// Always emit replica change event
	m.emitReplicaChange(buildReplicaEvent(newDep))

	// Extract endpoint name from deployment labels
	endpoint := ""
	managedBy := ""
	if newDep.Labels != nil {
		endpoint = newDep.Labels["app"]
		managedBy = newDep.Labels["managed-by"]
	}

	// Only handle deployments managed by waverless (worker deployments)
	if endpoint == "" || managedBy != "waverless" || endpoint == "waverless" {
		return
	}

	// Notify status change if replicas changed
	if oldDep.Status.ReadyReplicas != newDep.Status.ReadyReplicas ||
		oldDep.Status.AvailableReplicas != newDep.Status.AvailableReplicas ||
		*oldDep.Spec.Replicas != *newDep.Spec.Replicas {
		m.syncDeploymentStatus(newDep)
	}

	// Detect spec changes that trigger pod recreation (image, resources, env, etc.)
	if m.hasSpecChanged(oldDep, newDep) {
		logger.InfoCtx(context.Background(), "🔄 Deployment %s (endpoint: %s) spec changed, triggering optimized rolling update",
			newDep.Name, endpoint)
		m.notifyDeploymentSpecChange(endpoint)
	}
}

func (m *Manager) syncDeploymentStatus(deployment *appsv1.Deployment) {
	endpoint := ""
	managedBy := ""
	if deployment.Labels != nil {
		endpoint = deployment.Labels["app"]
		managedBy = deployment.Labels["managed-by"]
	}
	if endpoint == "" || managedBy != "waverless" || endpoint == "waverless" {
		return
	}
	m.notifyDeploymentStatusChange(endpoint, deployment)
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

// DeploymentToAppInfo converts a K8s Deployment to AppInfo (exported for use in callbacks)
func DeploymentToAppInfo(deployment *appsv1.Deployment) *AppInfo {
	return deploymentToAppInfo(deployment)
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

	if *deployment.Spec.Replicas == 0 {
		info.Status = "Stopped"
	} else if deployment.Status.AvailableReplicas == *deployment.Spec.Replicas {
		info.Status = "Running"
	} else {
		info.Status = "Pending"
	}

	// Extract shmSize and volumeMounts from deployment
	if len(deployment.Spec.Template.Spec.Volumes) > 0 {
		// Extract ShmSize from dshm volume
		for _, vol := range deployment.Spec.Template.Spec.Volumes {
			if vol.Name == "dshm" && vol.EmptyDir != nil && vol.EmptyDir.Medium == corev1.StorageMediumMemory {
				if vol.EmptyDir.SizeLimit != nil {
					info.ShmSize = vol.EmptyDir.SizeLimit.String()
				}
				break
			}
		}

		// Extract PVC volume mounts
		volumeMounts := make([]interfaces.VolumeMount, 0)
		for _, vol := range deployment.Spec.Template.Spec.Volumes {
			if vol.PersistentVolumeClaim != nil {
				// Find corresponding mount path from container volumeMounts
				if len(deployment.Spec.Template.Spec.Containers) > 0 {
					for _, mount := range deployment.Spec.Template.Spec.Containers[0].VolumeMounts {
						if mount.Name == vol.Name {
							volumeMounts = append(volumeMounts, interfaces.VolumeMount{
								PVCName:   vol.PersistentVolumeClaim.ClaimName,
								MountPath: mount.MountPath,
							})
							break
						}
					}
				}
			}
		}
		if len(volumeMounts) > 0 {
			info.VolumeMounts = volumeMounts
		}
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

	// Try to delete registry secret (if exists)
	secretName := fmt.Sprintf("registry-%s", name)
	err = m.client.CoreV1().Secrets(m.namespace).Delete(ctx, secretName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		fmt.Printf("Warning: failed to delete registry secret %s: %v\n", secretName, err)
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

// GetSpecManager returns the spec manager
func (m *Manager) GetSpecManager() *SpecManager {
	return m.specManager
}

// GetDynamicClient returns the dynamic client for CRDs
func (m *Manager) GetDynamicClient() dynamic.Interface {
	return m.dynamicClient
}

// GetCapacityStatus gets capacity status for a spec
func (m *Manager) GetCapacityStatus(specName string) interfaces.CapacityStatus {
	return m.specManager.GetCapacityStatus(specName)
}

// PodCounts Pod 数量统计
type PodCounts struct {
	Running int
	Pending int
}

// GetPodCountsBySpec 按 spec 统计 Pod 数量 (实现 capacity.PodCountProvider 接口)
func (m *Manager) GetPodCountsBySpec(ctx context.Context) (map[string]PodCounts, error) {
	result := make(map[string]PodCounts)

	pods, err := m.podLister.Pods(m.namespace).List(labels.Everything())
	if err != nil {
		return nil, err
	}

	for _, pod := range pods {
		// 从 Pod 的 nodeSelector 获取 spec
		specName := m.getSpecFromPod(pod)
		if specName == "" {
			continue
		}

		counts := result[specName]
		switch pod.Status.Phase {
		case corev1.PodRunning:
			counts.Running++
		case corev1.PodPending:
			counts.Pending++
		}
		result[specName] = counts
	}

	return result, nil
}

// getSpecFromPod 从 Pod 获取对应的 spec 名称
func (m *Manager) getSpecFromPod(pod *corev1.Pod) string {
	if pod.Spec.NodeSelector == nil {
		return ""
	}

	// 通过 nodepool label 反查 spec
	nodePool := pod.Spec.NodeSelector["karpenter.sh/nodepool"]
	if nodePool == "" {
		return ""
	}

	// 遍历所有 spec 找到匹配的
	specs := m.specManager.ListSpecs()
	for _, spec := range specs {
		for _, platformConfig := range spec.Platforms {
			if platformConfig.NodeSelector["karpenter.sh/nodepool"] == nodePool {
				return spec.Name
			}
		}
	}

	return ""
}

// GetInstanceTypesFromNodePool 从 Karpenter NodePool 获取 instance types
func (m *Manager) GetInstanceTypesFromNodePool(ctx context.Context, nodePoolName string) ([]string, error) {
	nodePoolGVR := schema.GroupVersionResource{
		Group:    "karpenter.sh",
		Version:  "v1",
		Resource: "nodepools",
	}

	nodePool, err := m.dynamicClient.Resource(nodePoolGVR).Get(ctx, nodePoolName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	// 解析 spec.template.spec.requirements
	requirements, found, err := unstructured.NestedSlice(nodePool.Object, "spec", "template", "spec", "requirements")
	if err != nil || !found {
		return nil, fmt.Errorf("requirements not found in nodepool %s", nodePoolName)
	}

	for _, req := range requirements {
		reqMap, ok := req.(map[string]interface{})
		if !ok {
			continue
		}

		key, _ := reqMap["key"].(string)
		if key == "node.kubernetes.io/instance-type" {
			values, ok := reqMap["values"].([]interface{})
			if ok {
				var instanceTypes []string
				for _, v := range values {
					if s, ok := v.(string); ok {
						instanceTypes = append(instanceTypes, s)
					}
				}
				return instanceTypes, nil
			}
		}
	}

	return nil, nil
}

// UpdateDeployment updates deployment
func (m *Manager) UpdateDeployment(ctx context.Context, endpoint string, specName string, image string, replicas *int, volumeMounts *[]interfaces.VolumeMount, shmSize *string, enablePtrace *bool, env *map[string]string) error {
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
			// Ensure NodeSelector is initialized (not nil) to avoid nil map assignment
			nodeSelector := platformConfig.NodeSelector
			if nodeSelector == nil {
				nodeSelector = make(map[string]string)
			}
			deployment.Spec.Template.Spec.NodeSelector = nodeSelector

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

	// Update volume mounts if provided
	if volumeMounts != nil && len(deployment.Spec.Template.Spec.Containers) > 0 {
		// Build volumes and volumeMounts
		var volumes []corev1.Volume
		var mounts []corev1.VolumeMount

		// Keep non-PVC volumes (e.g., dshm for shared memory)
		for _, vol := range deployment.Spec.Template.Spec.Volumes {
			if vol.PersistentVolumeClaim == nil {
				volumes = append(volumes, vol)
			}
		}

		// Keep non-PVC volume mounts
		for _, mount := range deployment.Spec.Template.Spec.Containers[0].VolumeMounts {
			// Check if this mount corresponds to a PVC volume
			isPVC := false
			for _, vol := range deployment.Spec.Template.Spec.Volumes {
				if vol.Name == mount.Name && vol.PersistentVolumeClaim != nil {
					isPVC = true
					break
				}
			}
			if !isPVC {
				mounts = append(mounts, mount)
			}
		}

		// Add new PVC volumes and mounts
		for i, vm := range *volumeMounts {
			volumeName := fmt.Sprintf("pvc-%d", i)
			volumes = append(volumes, corev1.Volume{
				Name: volumeName,
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: vm.PVCName,
					},
				},
			})
			mounts = append(mounts, corev1.VolumeMount{
				Name:      volumeName,
				MountPath: vm.MountPath,
			})
		}

		deployment.Spec.Template.Spec.Volumes = volumes
		deployment.Spec.Template.Spec.Containers[0].VolumeMounts = mounts
	}

	// Update shared memory size if provided
	if shmSize != nil && len(deployment.Spec.Template.Spec.Containers) > 0 {
		// Find or create dshm volume
		dshmVolumeIndex := -1
		for i, vol := range deployment.Spec.Template.Spec.Volumes {
			if vol.Name == "dshm" {
				dshmVolumeIndex = i
				break
			}
		}

		if *shmSize != "" {
			// Add or update dshm volume
			shmSizeQuantity := resource.MustParse(*shmSize)
			dshmVolume := corev1.Volume{
				Name: "dshm",
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{
						Medium:    corev1.StorageMediumMemory,
						SizeLimit: &shmSizeQuantity,
					},
				},
			}

			if dshmVolumeIndex >= 0 {
				// Update existing volume
				deployment.Spec.Template.Spec.Volumes[dshmVolumeIndex] = dshmVolume
			} else {
				// Add new volume
				deployment.Spec.Template.Spec.Volumes = append(deployment.Spec.Template.Spec.Volumes, dshmVolume)
			}

			// Ensure volumeMount exists for /dev/shm
			dshmMountExists := false
			for i, mount := range deployment.Spec.Template.Spec.Containers[0].VolumeMounts {
				if mount.Name == "dshm" {
					deployment.Spec.Template.Spec.Containers[0].VolumeMounts[i].MountPath = "/dev/shm"
					dshmMountExists = true
					break
				}
			}
			if !dshmMountExists {
				deployment.Spec.Template.Spec.Containers[0].VolumeMounts = append(
					deployment.Spec.Template.Spec.Containers[0].VolumeMounts,
					corev1.VolumeMount{
						Name:      "dshm",
						MountPath: "/dev/shm",
					},
				)
			}
		} else {
			// Remove dshm volume if shmSize is empty
			if dshmVolumeIndex >= 0 {
				deployment.Spec.Template.Spec.Volumes = append(
					deployment.Spec.Template.Spec.Volumes[:dshmVolumeIndex],
					deployment.Spec.Template.Spec.Volumes[dshmVolumeIndex+1:]...,
				)
			}

			// Remove dshm volume mount
			for i, mount := range deployment.Spec.Template.Spec.Containers[0].VolumeMounts {
				if mount.Name == "dshm" {
					deployment.Spec.Template.Spec.Containers[0].VolumeMounts = append(
						deployment.Spec.Template.Spec.Containers[0].VolumeMounts[:i],
						deployment.Spec.Template.Spec.Containers[0].VolumeMounts[i+1:]...,
					)
					break
				}
			}
		}
	}

	// Update ptrace capability if provided
	if enablePtrace != nil && len(deployment.Spec.Template.Spec.Containers) > 0 {
		container := &deployment.Spec.Template.Spec.Containers[0]

		if *enablePtrace {
			// Add SYS_PTRACE capability
			if container.SecurityContext == nil {
				container.SecurityContext = &corev1.SecurityContext{}
			}
			if container.SecurityContext.Capabilities == nil {
				container.SecurityContext.Capabilities = &corev1.Capabilities{}
			}

			// Check if SYS_PTRACE already exists
			hasptrace := false
			for _, cap := range container.SecurityContext.Capabilities.Add {
				if cap == "SYS_PTRACE" {
					hasptrace = true
					break
				}
			}
			if !hasptrace {
				container.SecurityContext.Capabilities.Add = append(
					container.SecurityContext.Capabilities.Add,
					"SYS_PTRACE",
				)
			}
		} else {
			// Remove SYS_PTRACE capability
			if container.SecurityContext != nil && container.SecurityContext.Capabilities != nil {
				var newCaps []corev1.Capability
				for _, cap := range container.SecurityContext.Capabilities.Add {
					if cap != "SYS_PTRACE" {
						newCaps = append(newCaps, cap)
					}
				}
				container.SecurityContext.Capabilities.Add = newCaps

				// Clean up empty SecurityContext if no capabilities left
				if len(container.SecurityContext.Capabilities.Add) == 0 && len(container.SecurityContext.Capabilities.Drop) == 0 {
					container.SecurityContext.Capabilities = nil
				}
				if container.SecurityContext.Capabilities == nil &&
					container.SecurityContext.RunAsUser == nil &&
					container.SecurityContext.RunAsGroup == nil &&
					container.SecurityContext.Privileged == nil {
					container.SecurityContext = nil
				}
			}
		}
	}

	// Update environment variables if provided
	if env != nil && len(deployment.Spec.Template.Spec.Containers) > 0 {
		container := &deployment.Spec.Template.Spec.Containers[0]

		// Separate system env vars from custom env vars
		var systemEnvVars []corev1.EnvVar
		for _, envVar := range container.Env {
			// Keep system env vars (RUNPOD_* and WAVERLESS_* prefixes, or those with valueFrom)
			if strings.HasPrefix(envVar.Name, "RUNPOD_") || strings.HasPrefix(envVar.Name, "WAVERLESS_") || envVar.ValueFrom != nil {
				systemEnvVars = append(systemEnvVars, envVar)
			}
		}

		// Start with system env vars
		newEnvVars := systemEnvVars

		// Add custom env vars from the provided map
		for key, value := range *env {
			newEnvVars = append(newEnvVars, corev1.EnvVar{
				Name:  key,
				Value: value,
			})
		}

		// Update container env vars
		container.Env = newEnvVars
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

// ForceDeletePod forcefully deletes a stuck pod using a multi-stage approach
// This should only be used as a safety net when:
// 1. Pod is in Terminating state for too long
// 2. Database confirms the worker has no running tasks
// 3. Worker is not responding to SIGTERM
//
// Multi-stage deletion to avoid containerd/zombie process issues:
// Stage 1: Remove finalizers (common blocker for deletion)
// Stage 2: Delete with short grace period (1s) to give container time to cleanup
// Stage 3: If still stuck, delete with grace period 0 (SIGKILL)
func (m *Manager) ForceDeletePod(ctx context.Context, podName string) error {
	if podName == "" {
		return fmt.Errorf("pod name cannot be empty")
	}

	pods := m.client.CoreV1().Pods(m.namespace)

	// Stage 1: Remove finalizers (if any)
	// Finalizers can block pod deletion even with grace period 0
	pod, err := pods.Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			logger.InfoCtx(ctx, "Pod %s already deleted", podName)
			return nil
		}
		return fmt.Errorf("failed to get pod %s: %w", podName, err)
	}

	// Remove finalizers if present
	if len(pod.Finalizers) > 0 {
		logger.InfoCtx(ctx, "🔧 Removing %d finalizers from pod %s to unblock deletion", len(pod.Finalizers), podName)
		pod.Finalizers = []string{}
		_, err = pods.Update(ctx, pod, metav1.UpdateOptions{})
		if err != nil {
			logger.WarnCtx(ctx, "Failed to remove finalizers from pod %s: %v (continuing anyway)", podName, err)
		} else {
			logger.InfoCtx(ctx, "✅ Finalizers removed from pod %s", podName)
		}
	}

	// Stage 2: Try deletion with short grace period (2 second)
	// This gives containerd/container runtime a chance to cleanup gracefully
	// Avoids zombie processes and resource leaks
	shortGracePeriod := int64(2)
	deleteOptions := metav1.DeleteOptions{
		GracePeriodSeconds: &shortGracePeriod,
	}

	logger.InfoCtx(ctx, "🔄 Attempting to delete pod %s with 2s grace period", podName)
	err = pods.Delete(ctx, podName, deleteOptions)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.InfoCtx(ctx, "Pod %s already deleted", podName)
			return nil
		}
		logger.WarnCtx(ctx, "Failed to delete pod %s with 1s grace period: %v", podName, err)
	} else {
		// Wait 2 seconds to see if pod gets deleted
		time.Sleep(2 * time.Second)

		// Check if pod is gone
		_, err = pods.Get(ctx, podName, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			logger.InfoCtx(ctx, "✅ Pod %s deleted successfully with 1s grace period", podName)
			return nil
		}
	}

	// Stage 3: Last resort - force delete with grace period 0 (SIGKILL)
	// This can cause containerd cleanup issues, but sometimes necessary
	logger.WarnCtx(ctx, "⚠️ Pod %s still stuck, forcing deletion with 0 grace period (SIGKILL)", podName)

	zeroGracePeriod := int64(0)
	forceDeleteOptions := metav1.DeleteOptions{
		GracePeriodSeconds: &zeroGracePeriod,
	}

	err = pods.Delete(ctx, podName, forceDeleteOptions)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.InfoCtx(ctx, "Pod %s already deleted", podName)
			return nil
		}
		return fmt.Errorf("failed to force delete pod %s with grace period 0: %w", podName, err)
	}

	logger.WarnCtx(ctx, "🔴 Force deleted pod %s with grace period 0 (SIGKILL) - may cause containerd cleanup issues", podName)
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

// GetDefaultEnvFromConfigMap reads the wavespeed-config ConfigMap and returns its data as environment variables
// Returns nil if the ConfigMap doesn't exist (not an error, just no defaults)
func (m *Manager) GetDefaultEnvFromConfigMap(ctx context.Context) (map[string]string, error) {
	configMaps := m.client.CoreV1().ConfigMaps(m.namespace)

	cm, err := configMaps.Get(ctx, "wavespeed-config", metav1.GetOptions{})
	if err != nil {
		// ConfigMap doesn't exist - return empty map, not an error
		if strings.Contains(err.Error(), "not found") {
			return make(map[string]string), nil
		}
		return nil, fmt.Errorf("failed to get wavespeed-config ConfigMap: %v", err)
	}

	if cm.Data == nil {
		return make(map[string]string), nil
	}

	return cm.Data, nil
}

// ExecPodCommand executes a command in a pod's container
// Returns stdout, stderr, and error
func (m *Manager) ExecPodCommand(ctx context.Context, podName, endpoint string, command []string) (string, string, error) {
	if podName == "" {
		return "", "", fmt.Errorf("pod name cannot be empty")
	}
	if endpoint == "" {
		return "", "", fmt.Errorf("endpoint cannot be empty")
	}
	if len(command) == 0 {
		return "", "", fmt.Errorf("command cannot be empty")
	}

	// Container name follows the pattern: {endpoint}-worker
	containerName := endpoint + "-worker"

	// Create exec request
	req := m.client.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(m.namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: containerName,
			Command:   command,
			Stdin:     false,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, scheme.ParameterCodec)

	// Create executor
	executor, err := remotecommand.NewSPDYExecutor(m.config, "POST", req.URL())
	if err != nil {
		return "", "", fmt.Errorf("failed to create executor: %w", err)
	}

	// Capture stdout and stderr
	var stdout, stderr bytes.Buffer

	// Execute the command
	err = executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:  nil,
		Stdout: &stdout,
		Stderr: &stderr,
		Tty:    false,
	})

	return stdout.String(), stderr.String(), err
}

// multiplyResource multiplies a K8s resource string by a factor
// Supports formats: "4" (cores), "8Gi", "16G", "1024Mi", "1024M"
func multiplyResource(resource string, factor int) string {
	if factor <= 1 || resource == "" {
		return resource
	}

	// Try to parse as number with unit suffix
	var value int
	var unit string

	n, _ := fmt.Sscanf(resource, "%d%s", &value, &unit)
	if n >= 1 {
		return fmt.Sprintf("%d%s", value*factor, unit)
	}

	return resource
}
