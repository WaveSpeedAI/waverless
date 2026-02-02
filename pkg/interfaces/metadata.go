package interfaces

import (
	"context"
	"time"
)

// Worker Status Constants
const (
	WorkerStatusOnline   = "online"   // Worker running normally, can pull and process tasks
	WorkerStatusBusy     = "busy"     // Worker busy, processing tasks
	WorkerStatusDraining = "draining" // Worker entering graceful shutdown, not pulling new tasks, waiting for existing tasks to complete
	WorkerStatusOffline  = "offline"  // Worker offline
)

// MetadataStore Endpoint metadata storage interface
// Supports multiple storages like Redis, MySQL, PostgreSQL, etc.
type MetadataStore interface {
	// SaveEndpoint saves endpoint metadata
	SaveEndpoint(ctx context.Context, endpoint *EndpointMetadata) error

	// GetEndpoint retrieves endpoint metadata
	GetEndpoint(ctx context.Context, name string) (*EndpointMetadata, error)

	// ListEndpoints lists all endpoints
	ListEndpoints(ctx context.Context) ([]*EndpointMetadata, error)

	// UpdateEndpoint updates endpoint metadata
	UpdateEndpoint(ctx context.Context, endpoint *EndpointMetadata) error

	// DeleteEndpoint deletes endpoint metadata
	DeleteEndpoint(ctx context.Context, name string) error

	// SaveWorker saves Worker information
	SaveWorker(ctx context.Context, worker *WorkerMetadata) error

	// GetWorker retrieves Worker information
	GetWorker(ctx context.Context, workerID string) (*WorkerMetadata, error)

	// ListWorkers lists all Workers
	// endpoint: filter workers for specific endpoint, empty string means all
	ListWorkers(ctx context.Context, endpoint string) ([]*WorkerMetadata, error)

	// UpdateWorkerHeartbeat updates Worker heartbeat
	UpdateWorkerHeartbeat(ctx context.Context, workerID string, heartbeat time.Time) error

	// DeleteWorker deletes Worker
	DeleteWorker(ctx context.Context, workerID string) error

	// GetEndpointStats retrieves endpoint statistics
	GetEndpointStats(ctx context.Context, name string) (*EndpointStats, error)

	// Close closes connection
	Close() error
}

// EndpointMetadata Endpoint metadata
type EndpointMetadata struct {
	// Basic information
	Name        string `json:"name"`                // Endpoint name
	Namespace   string `json:"namespace,omitempty"` // K8s namespace
	DisplayName string `json:"displayName"`         // Display name
	Description string `json:"description"`         // Description

	// Deployment information
	SpecName         string     `json:"specName"`         // Spec name
	Image            string     `json:"image"`            // Docker image
	ImagePrefix      string     `json:"imagePrefix"`      // Image prefix for matching updates (e.g., "wavespeed/model-deploy:wan_i2v-default-")
	ImageDigest      string     `json:"imageDigest"`      // Current image digest from DockerHub
	ImageLastChecked *time.Time `json:"imageLastChecked"` // Last time image was checked for updates
	LatestImage      string     `json:"latestImage"`      // Latest available image if update is available
	Replicas         int        `json:"replicas"`         // Replica count
	GpuCount         int        `json:"gpuCount"`         // GPU count per replica (resources = per-gpu-config * gpuCount)

	// Auto-scaling configuration
	MinReplicas       int     `json:"minReplicas"`                 // Minimum replica count (default 0)
	MaxReplicas       int     `json:"maxReplicas"`                 // Maximum replica count
	ScaleUpThreshold  int     `json:"scaleUpThreshold"`            // Queue threshold for scale up (default 1)
	ScaleDownIdleTime int     `json:"scaleDownIdleTime"`           // Idle time in seconds before scale down (default 300)
	ScaleUpCooldown   int     `json:"scaleUpCooldown"`             // Scale up cooldown in seconds (default 30)
	ScaleDownCooldown int     `json:"scaleDownCooldown"`           // Scale down cooldown in seconds (default 60)
	Priority          int     `json:"priority"`                    // Priority for resource allocation (0-100, default 50)
	EnableDynamicPrio *bool   `json:"enableDynamicPrio"`           // Enable dynamic priority (default true)
	HighLoadThreshold int     `json:"highLoadThreshold"`           // High load threshold for priority boost (default 10)
	PriorityBoost     int     `json:"priorityBoost"`               // Priority boost amount when high load (default 20)
	AutoscalerEnabled *string `json:"autoscalerEnabled,omitempty"` // Autoscaler override: nil/"" = follow global, "disabled" = force off, "enabled" = force on

	// Auto-scaling runtime state
	LastScaleTime    time.Time `json:"lastScaleTime,omitempty"`    // Last scaling time
	LastTaskTime     time.Time `json:"lastTaskTime,omitempty"`     // Last task processing time
	FirstPendingTime time.Time `json:"firstPendingTime,omitempty"` // First pending task time (for starvation detection)

	// Real-time metrics (populated dynamically by /api/v1/k8s/apps, not persisted)
	PendingTasks int64 `json:"pendingTasks,omitempty"` // Current pending tasks in queue
	RunningTasks int64 `json:"runningTasks,omitempty"` // Current running tasks

	// Configuration information
	Env             map[string]string `json:"env"`             // Environment variables
	Labels          map[string]string `json:"labels"`          // Labels
	TaskTimeout     int               `json:"taskTimeout"`     // Task execution timeout in seconds (0 = use global default)
	EnablePtrace    bool              `json:"enablePtrace"`    // Enable SYS_PTRACE capability for debugging (only for fixed resource pools)
	MaxPendingTasks int               `json:"maxPendingTasks"` // Maximum allowed pending tasks before warning clients (default 1)

	// Status information
	Status            string `json:"status"`            // Running, Stopped, Failed
	ReadyReplicas     int    `json:"readyReplicas"`     // Ready replicas
	AvailableReplicas int    `json:"availableReplicas"` // Available replicas

	// Health status (for image validation and status transparency feature)
	HealthStatus      string     `json:"healthStatus"`                // HEALTHY, DEGRADED, UNHEALTHY
	HealthMessage     string     `json:"healthMessage,omitempty"`     // User-friendly health message
	LastHealthCheckAt *time.Time `json:"lastHealthCheckAt,omitempty"` // Last health check timestamp

	// Worker information
	WorkerCount       int `json:"workerCount"`       // Worker count
	ActiveWorkerCount int `json:"activeWorkerCount"` // Active Worker count

	// Task statistics
	TotalTasks     int64 `json:"totalTasks"`     // Total tasks
	CompletedTasks int64 `json:"completedTasks"` // Completed tasks
	FailedTasks    int64 `json:"failedTasks"`    // Failed tasks

	// Storage configuration (backfilled from K8s deployment)
	ShmSize      string        `json:"shmSize,omitempty"`      // Shared memory size from deployment
	VolumeMounts []VolumeMount `json:"volumeMounts,omitempty"` // PVC volume mounts from deployment

	// Timestamps
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// WorkerMetadata Worker metadata
type WorkerMetadata struct {
	ID             string    `json:"id"`
	Endpoint       string    `json:"endpoint"`
	Status         string    `json:"status"` // online, busy, draining, offline (use WorkerStatus* constants)
	Concurrency    int       `json:"concurrency"`
	CurrentJobs    int       `json:"currentJobs"`
	JobsInProgress []string  `json:"jobsInProgress"`
	RegisteredAt   time.Time `json:"registeredAt"`
	LastHeartbeat  time.Time `json:"lastHeartbeat"`

	// Additional information
	HostIP    string            `json:"hostIp,omitempty"`
	PodName   string            `json:"podName,omitempty"`   // K8s specific
	Namespace string            `json:"namespace,omitempty"` // K8s specific
	Metadata  map[string]string `json:"metadata,omitempty"`  // Other metadata
}

// EndpointStats Endpoint statistics
type EndpointStats struct {
	Endpoint string `json:"endpoint"`

	// Worker statistics
	TotalWorkers  int `json:"totalWorkers"`
	OnlineWorkers int `json:"onlineWorkers"`
	BusyWorkers   int `json:"busyWorkers"`

	// Task statistics
	PendingTasks   int `json:"pendingTasks"`
	RunningTasks   int `json:"runningTasks"`
	CompletedTasks int `json:"completedTasks"`
	FailedTasks    int `json:"failedTasks"`

	// Performance statistics
	AvgTaskDuration float64 `json:"avgTaskDuration"` // Average task execution time (seconds)
	TasksPerMinute  float64 `json:"tasksPerMinute"`  // Tasks per minute

	// Time range
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}
