package gmi

import (
	"encoding/json"
	"time"

	"waverless/pkg/interfaces"
)

// ========================================
// BFF response types — ieops-v2 BFF API
// ========================================

// bffResponse is the envelope for all BFF responses: {"msg":"success","data":{...}}
type bffResponse struct {
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data,omitempty"`
}

// bffModelResponse represents a model from BFF GET /models and GET /models/:name
type bffModelResponse struct {
	Name              string            `json:"name"`
	Model             string            `json:"model"`
	Status            string            `json:"status"` // ModelDeployment Phase: Running, Deploying, Failed, etc.
	Image             string            `json:"image"`
	DesiredReplicas   int32             `json:"desired_replicas"`
	CurrentReplicas   int32             `json:"current_replicas"`
	ReadyReplicas     int32             `json:"ready_replicas"`
	AvailableReplicas int32             `json:"available_replicas"`
	CreatedAt         string            `json:"created_at"`
	Regions           []string          `json:"regions"`
	Clusters          []string          `json:"clusters"`
	// Detail-only fields (from GET /models/:name):
	Pods         []bffPodStatus    `json:"pods,omitempty"`
	EnvVars      map[string]string `json:"envVars,omitempty"`
	Command      []string          `json:"command,omitempty"`
	ShmSize      string            `json:"shmSize,omitempty"`
	VolumeMounts []bffVolumeMount  `json:"volumeMounts,omitempty"`
}

// bffPodStatus represents a pod from BFF, sourced from CRD PodStatus + placement data
type bffPodStatus struct {
	PodName           string `json:"pod_name"`
	NodeID            string `json:"node_id"`
	ClusterID         string `json:"cluster_id"`
	Phase             string `json:"phase"`
	Ready             bool   `json:"ready"`
	CreatedAt         string `json:"created_at"`
	StartedAt         string `json:"started_at"`
	ReadyAt           string `json:"ready_at"`
	DeletionTimestamp string `json:"deletion_timestamp"`
	Reason            string `json:"reason"`
	Message           string `json:"message"`
	RestartCount      int32  `json:"restart_count"`
}

// bffVolumeMount represents a volume mount from BFF
type bffVolumeMount struct {
	PVCName   string `json:"pvcName"`
	MountPath string `json:"mountPath"`
}

// ========================================
// Internal types
// ========================================

// WorkerStatusChangeCallback is called when a worker's status changes
type WorkerStatusChangeCallback func(workerID, endpoint string, info *interfaces.PodInfo)

// WorkerDeleteCallback is called when a worker is deleted
type WorkerDeleteCallback func(workerID, endpoint string, deletedAt *time.Time)

// gmiWorkerState tracks a worker's last known state (used by polling watcher)
type gmiWorkerState struct {
	ID                string
	Endpoint          string
	Status            string
	CreatedAt         string
	StartedAt         string
	ReadyAt           string
	DeletionTimestamp string // Set when pod is being drained/terminated
	Reason            string
	Message           string
	RestartCount      int32
}
