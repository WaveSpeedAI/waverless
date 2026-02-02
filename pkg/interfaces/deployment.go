package interfaces

import (
	"context"
)

// DeploymentProvider deployment provider interface
// Supports multiple deployment methods like K8s, third-party services, etc.
type DeploymentProvider interface {
	// Deploy deploys application
	// endpoint: application name/endpoint name
	// spec: specification configuration (e.g., GPU type, resource configuration, etc.)
	// image: Docker image
	// replicas: replica count
	Deploy(ctx context.Context, req *DeployRequest) (*DeployResponse, error)

	// GetApp retrieves application details
	GetApp(ctx context.Context, endpoint string) (*AppInfo, error)

	// ListApps lists all applications
	ListApps(ctx context.Context) ([]*AppInfo, error)

	// DeleteApp deletes application
	DeleteApp(ctx context.Context, endpoint string) error

	// GetAppLogs retrieves application logs
	// podName is optional - if provided, gets logs from specific pod; otherwise gets from first pod
	GetAppLogs(ctx context.Context, endpoint string, lines int, podName ...string) (string, error)

	// ScaleApp scales application
	ScaleApp(ctx context.Context, endpoint string, replicas int) error

	// GetAppStatus retrieves application status
	GetAppStatus(ctx context.Context, endpoint string) (*AppStatus, error)

	// ListSpecs lists available specifications
	ListSpecs(ctx context.Context) ([]*SpecInfo, error)

	// GetSpec retrieves specification details
	GetSpec(ctx context.Context, specName string) (*SpecInfo, error)

	// PreviewDeploymentYAML previews deployment configuration
	PreviewDeploymentYAML(ctx context.Context, req *DeployRequest) (string, error)

	// UpdateDeployment updates deployment (e.g., image, replica count, etc.)
	UpdateDeployment(ctx context.Context, req *UpdateDeploymentRequest) (*DeployResponse, error)

	// WatchReplicas watches replica count changes
	WatchReplicas(ctx context.Context, callback ReplicaCallback) error

	// GetPods retrieves all Pod information for specified endpoint (including Pending, Running, Terminating)
	GetPods(ctx context.Context, endpoint string) ([]*PodInfo, error)

	// DescribePod retrieves detailed Pod information (similar to kubectl describe)
	DescribePod(ctx context.Context, endpoint string, podName string) (*PodDetail, error)

	// GetPodYAML retrieves Pod YAML (similar to kubectl get pod -o yaml)
	GetPodYAML(ctx context.Context, endpoint string, podName string) (string, error)

	// ListPVCs lists all PersistentVolumeClaims in the namespace
	ListPVCs(ctx context.Context) ([]*PVCInfo, error)

	// GetDefaultEnv retrieves default environment variables from wavespeed-config ConfigMap
	GetDefaultEnv(ctx context.Context) (map[string]string, error)

	// IsPodTerminating checks if a worker/pod is terminating
	IsPodTerminating(ctx context.Context, podName string) (bool, error)
}

// ReplicaEvent represents Deployment replica change event
type ReplicaEvent struct {
	Name              string             `json:"name"`
	DesiredReplicas   int                `json:"desiredReplicas"`
	ReadyReplicas     int                `json:"readyReplicas"`
	AvailableReplicas int                `json:"availableReplicas"`
	Conditions        []ReplicaCondition `json:"conditions,omitempty"`
}

type ReplicaCondition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

// ReplicaCallback replica change callback
type ReplicaCallback func(event ReplicaEvent)

// VolumeMount volume mount configuration
type VolumeMount struct {
	PVCName   string `json:"pvcName"`   // PVC name
	MountPath string `json:"mountPath"` // Mount path in container
}

// DeployRequest deployment request
type DeployRequest struct {
	Endpoint           string              `json:"endpoint"`                // Application name/endpoint
	SpecName           string              `json:"specName"`                // Spec name
	Image              string              `json:"image"`                   // Docker image
	Replicas           int                 `json:"replicas"`                // Replica count
	GpuCount           int                 `json:"gpuCount"`                // GPU count (1-N, resources = per-gpu-config * gpuCount)
	TaskTimeout        int                 `json:"taskTimeout"`             // Task execution timeout in seconds (0 = use global default)
	Env                map[string]string   `json:"env"`                     // Environment variables
	Labels             map[string]string   `json:"labels"`                  // Labels
	VolumeMounts       []VolumeMount       `json:"volumeMounts,omitempty"`  // PVC volume mounts
	ShmSize            string              `json:"shmSize,omitempty"`       // Shared memory size (e.g., "1Gi", "512Mi")
	EnablePtrace       bool                `json:"enablePtrace,omitempty"`  // Enable SYS_PTRACE capability for debugging (only for fixed resource pools)
	ValidateImage      *bool               `json:"validateImage,omitempty"` // Whether to validate image before deployment (default: use config)
	RegistryCredential *RegistryCredential `json:"registryCredential,omitempty"`
}

// RegistryCredential for private container registries
type RegistryCredential struct {
	Registry string `json:"registry"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// DeployResponse deployment response
type DeployResponse struct {
	Endpoint  string `json:"endpoint"`
	Message   string `json:"message"`
	CreatedAt string `json:"createdAt"`
}

// UpdateDeploymentRequest update deployment request (image, specification, replica count)
type UpdateDeploymentRequest struct {
	Endpoint     string             `json:"endpoint"`               // Application name (required)
	SpecName     string             `json:"specName,omitempty"`     // New spec name (optional)
	Image        string             `json:"image,omitempty"`        // New docker image (optional)
	Replicas     *int               `json:"replicas,omitempty"`     // New replica count (optional, use pointer to distinguish 0 from unset)
	VolumeMounts *[]VolumeMount     `json:"volumeMounts,omitempty"` // New volume mounts (optional, use pointer to distinguish empty from unset)
	ShmSize      *string            `json:"shmSize,omitempty"`      // New shared memory size (optional, use pointer to distinguish empty from unset)
	EnablePtrace *bool              `json:"enablePtrace,omitempty"` // Enable SYS_PTRACE capability (optional, use pointer to distinguish false from unset)
	Env          *map[string]string `json:"env,omitempty"`          // New environment variables (optional, use pointer to distinguish empty from unset)
	TaskTimeout  *int               `json:"taskTimeout,omitempty"`  // New task timeout (optional)
}

// UpdateEndpointConfigRequest update Endpoint configuration request (metadata + autoscaling configuration)
// Only contains fields that users can edit via UI
type UpdateEndpointConfigRequest struct {
	// Basic metadata
	DisplayName     *string `json:"displayName,omitempty"`     // Display name
	Description     *string `json:"description,omitempty"`     // Description
	TaskTimeout     *int    `json:"taskTimeout,omitempty"`     // Task timeout in seconds
	MaxPendingTasks *int    `json:"maxPendingTasks,omitempty"` // Maximum allowed pending tasks before warning clients
	ImagePrefix     *string `json:"imagePrefix,omitempty"`     // Image prefix for matching updates

	// Autoscaling configuration
	MinReplicas       *int    `json:"minReplicas,omitempty"`       // Minimum replicas (0 = scale-to-zero)
	MaxReplicas       *int    `json:"maxReplicas,omitempty"`       // Maximum replicas
	Priority          *int    `json:"priority,omitempty"`          // Priority (0-100, 0 = best-effort)
	ScaleUpThreshold  *int    `json:"scaleUpThreshold,omitempty"`  // Queue threshold for scale up
	ScaleDownIdleTime *int    `json:"scaleDownIdleTime,omitempty"` // Idle time before scale down (seconds)
	ScaleUpCooldown   *int    `json:"scaleUpCooldown,omitempty"`   // Scale up cooldown (seconds, 0 = no cooldown)
	ScaleDownCooldown *int    `json:"scaleDownCooldown,omitempty"` // Scale down cooldown (seconds, 0 = no cooldown)
	EnableDynamicPrio *bool   `json:"enableDynamicPrio,omitempty"` // Enable dynamic priority
	HighLoadThreshold *int    `json:"highLoadThreshold,omitempty"` // High load threshold for priority boost
	PriorityBoost     *int    `json:"priorityBoost,omitempty"`     // Priority boost amount (0 = no boost)
	AutoscalerEnabled *string `json:"autoscalerEnabled,omitempty"` // Autoscaler override: "" = default, "disabled" = off, "enabled" = on
}

// AppInfo application information
type AppInfo struct {
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace,omitempty"` // K8s specific
	Type              string            `json:"type"`                // Deployment, Service, etc.
	Status            string            `json:"status"`
	Replicas          int32             `json:"replicas,omitempty"`
	ReadyReplicas     int32             `json:"readyReplicas,omitempty"`
	AvailableReplicas int32             `json:"availableReplicas,omitempty"`
	Image             string            `json:"image"`
	Labels            map[string]string `json:"labels"`
	CreatedAt         string            `json:"createdAt"`
	ShmSize           string            `json:"shmSize,omitempty"`      // Shared memory size from deployment volumes
	VolumeMounts      []VolumeMount     `json:"volumeMounts,omitempty"` // PVC volume mounts from deployment
}

// AppStatus application status
type AppStatus struct {
	Endpoint          string `json:"endpoint"`
	Status            string `json:"status"` // Running, Pending, Failed, etc.
	ReadyReplicas     int32  `json:"readyReplicas"`
	AvailableReplicas int32  `json:"availableReplicas"`
	TotalReplicas     int32  `json:"totalReplicas"`
	Message           string `json:"message,omitempty"`
}

// SpecInfo specification information
type SpecInfo struct {
	Name         string                 `json:"name"`
	DisplayName  string                 `json:"displayName"`
	Category     string                 `json:"category"`
	ResourceType string                 `json:"resourceType"` // fixed, serverless
	Resources    ResourceRequirements   `json:"resources"`
	Platforms    map[string]interface{} `json:"platforms"`
}

// ResourceRequirements resource requirements
type ResourceRequirements struct {
	GPU              string `json:"gpu"`
	GPUType          string `json:"gpuType"`
	CPU              string `json:"cpu"`
	Memory           string `json:"memory"`
	EphemeralStorage string `json:"ephemeralStorage,omitempty"`
	ShmSize          string `json:"shmSize,omitempty"` // Shared memory size (e.g., "1Gi", "512Mi")
}

// CreateSpecRequest create spec request
type CreateSpecRequest struct {
	Name         string                 `json:"name" binding:"required"`
	DisplayName  string                 `json:"displayName" binding:"required"`
	Category     string                 `json:"category" binding:"required"`     // cpu, gpu
	ResourceType string                 `json:"resourceType" binding:"required"` // fixed, serverless
	Resources    ResourceRequirements   `json:"resources" binding:"required"`
	Platforms    map[string]interface{} `json:"platforms,omitempty"`
}

// UpdateSpecRequest update spec request
type UpdateSpecRequest struct {
	DisplayName  *string                `json:"displayName,omitempty"`
	Category     *string                `json:"category,omitempty"`
	ResourceType *string                `json:"resourceType,omitempty"` // fixed, serverless
	Resources    *ResourceRequirements  `json:"resources,omitempty"`
	Platforms    map[string]interface{} `json:"platforms,omitempty"`
	Status       *string                `json:"status,omitempty"` // active, inactive, deprecated
}

// PodInfo Pod basic information
type PodInfo struct {
	Name              string            `json:"name"`
	Phase             string            `json:"phase"`             // Pending, Running, Succeeded, Failed, Unknown
	Status            string            `json:"status"`            // Creating, Running, Terminating, Failed, etc.
	Reason            string            `json:"reason,omitempty"`  // Why in this status
	Message           string            `json:"message,omitempty"` // Detailed status message
	IP                string            `json:"ip,omitempty"`
	NodeName          string            `json:"nodeName,omitempty"`
	CreatedAt         string            `json:"createdAt"`
	StartedAt         string            `json:"startedAt,omitempty"`
	DeletionTimestamp string            `json:"deletionTimestamp,omitempty"` // Set when pod is terminating
	Labels            map[string]string `json:"labels,omitempty"`
	RestartCount      int32             `json:"restartCount"`
	WorkerID          string            `json:"workerID,omitempty"` // Matched worker ID from Redis
}

// PodDetail Pod detailed information (similar to kubectl describe)
type PodDetail struct {
	*PodInfo
	Namespace       string                 `json:"namespace"`
	UID             string                 `json:"uid"`
	Annotations     map[string]string      `json:"annotations,omitempty"`
	Containers      []ContainerInfo        `json:"containers"`
	InitContainers  []ContainerInfo        `json:"initContainers,omitempty"`
	Conditions      []PodCondition         `json:"conditions"`
	Events          []PodEvent             `json:"events"`
	OwnerReferences []OwnerReference       `json:"ownerReferences,omitempty"`
	Tolerations     []map[string]string    `json:"tolerations,omitempty"`
	Affinity        map[string]interface{} `json:"affinity,omitempty"`
	Volumes         []VolumeInfo           `json:"volumes,omitempty"`
}

// ContainerInfo container information
type ContainerInfo struct {
	Name         string                 `json:"name"`
	Image        string                 `json:"image"`
	State        string                 `json:"state"` // Waiting, Running, Terminated
	Ready        bool                   `json:"ready"`
	RestartCount int32                  `json:"restartCount"`
	Reason       string                 `json:"reason,omitempty"`
	Message      string                 `json:"message,omitempty"`
	StartedAt    string                 `json:"startedAt,omitempty"`
	FinishedAt   string                 `json:"finishedAt,omitempty"`
	ExitCode     int32                  `json:"exitCode,omitempty"`
	Resources    map[string]interface{} `json:"resources,omitempty"`
	Ports        []ContainerPort        `json:"ports,omitempty"`
	Env          []EnvVar               `json:"env,omitempty"`
}

// ContainerPort container port
type ContainerPort struct {
	Name          string `json:"name,omitempty"`
	ContainerPort int32  `json:"containerPort"`
	Protocol      string `json:"protocol"`
}

// EnvVar environment variable
type EnvVar struct {
	Name  string `json:"name"`
	Value string `json:"value,omitempty"`
}

// PVCInfo PersistentVolumeClaim information
type PVCInfo struct {
	Name         string `json:"name"`
	Namespace    string `json:"namespace"`
	Status       string `json:"status"`       // Bound, Pending, Lost
	Volume       string `json:"volume"`       // Bound volume name
	Capacity     string `json:"capacity"`     // Storage capacity (e.g., "100Gi")
	AccessModes  string `json:"accessModes"`  // e.g., "ReadWriteOnce"
	StorageClass string `json:"storageClass"` // Storage class name
	CreatedAt    string `json:"createdAt"`
}

// PodCondition Pod condition
type PodCondition struct {
	Type               string `json:"type"`
	Status             string `json:"status"`
	Reason             string `json:"reason,omitempty"`
	Message            string `json:"message,omitempty"`
	LastTransitionTime string `json:"lastTransitionTime,omitempty"`
}

// PodEvent Pod event
type PodEvent struct {
	Type      string `json:"type"` // Normal, Warning
	Reason    string `json:"reason"`
	Message   string `json:"message"`
	Count     int32  `json:"count"`
	FirstSeen string `json:"firstSeen"`
	LastSeen  string `json:"lastSeen"`
}

// OwnerReference owner reference
type OwnerReference struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	UID  string `json:"uid"`
}

// VolumeInfo volume information
type VolumeInfo struct {
	Name   string                 `json:"name"`
	Type   string                 `json:"type"`
	Source map[string]interface{} `json:"source,omitempty"`
}
