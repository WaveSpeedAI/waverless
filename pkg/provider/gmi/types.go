package gmi

import "waverless/pkg/interfaces"

// ========================================
// Request types - matches gmiless interfaces.EndpointRequest
// ========================================

// gmiEndpointRequest matches gmiless interfaces.EndpointRequest
type gmiEndpointRequest struct {
	// Core
	Name     *string `json:"name,omitempty"`
	Replicas *int    `json:"replicas,omitempty"`

	// Hardware
	ComputeType *string   `json:"computeType,omitempty"` // GPU, CPU
	GpuCount    *int      `json:"gpuCount,omitempty"`
	GpuTypeIds  *[]string `json:"gpuTypeIds,omitempty"`
	VcpuCount   *int      `json:"vcpuCount,omitempty"`

	// Template
	Template   *gmiTemplateData `json:"template,omitempty"`
	TemplateId *string          `json:"templateId,omitempty"`

	// Networking / Storage
	DataCenterIds   *[]string `json:"dataCenterIds,omitempty"`
	NetworkVolumeId *string   `json:"networkVolumeId,omitempty"`

	// Endpoint type
	Type                 *string `json:"type,omitempty"` // LB, QB
	UseContainerResource *bool   `json:"useContainerResource,omitempty"`

	// Autoscaling
	ExecutionTimeoutMs *int64  `json:"executionTimeoutMs,omitempty"`
	IdleTimeout        *int    `json:"idleTimeout,omitempty"`
	WorkersMin         *int    `json:"workersMin,omitempty"`
	WorkersMax         *int    `json:"workersMax,omitempty"`
	ScalerType         *string `json:"scalerType,omitempty"`
	ScalerValue        *int    `json:"scalerValue,omitempty"`
	ScaleDownIdleTime  *int    `json:"scaleDownIdleTime,omitempty"`
	ScaleUpCooldown    *int    `json:"scaleUpCooldown,omitempty"`
	ScaleDownCooldown  *int    `json:"scaleDownCooldown,omitempty"`
}

// gmiTemplateData matches gmiless interfaces.TemplateData
type gmiTemplateData struct {
	ImageName        *string           `json:"imageName,omitempty"`
	Env              map[string]string `json:"env,omitempty"`
	DockerEntrypoint []string          `json:"dockerEntrypoint,omitempty"`
	DockerStartCmd   []string          `json:"dockerStartCmd,omitempty"`
	Ports            []string          `json:"ports,omitempty"`
	ShmSize          *string           `json:"shmSize,omitempty"`
	VolumeMountPath  *string           `json:"volumeMountPath,omitempty"`
}

// ========================================
// Response types - from gmiless API
// ========================================

// gmiEndpointResponse is the response from gmiless endpoint APIs
type gmiEndpointResponse struct {
	Id         string              `json:"id"`
	Name       string              `json:"name"`
	Image      string              `json:"image"`
	Replicas   int                 `json:"replicas"`
	Status     string              `json:"status"`
	GpuCount   int                 `json:"gpuCount"`
	GpuTypeIds []string            `json:"gpuTypeIds"`
	CreatedAt  string              `json:"createdAt"`
	Env        map[string]string   `json:"env"`
	Workers    []gmiWorkerResponse `json:"workers"`
	WorkersMin int                 `json:"workersMin"`
	WorkersMax int                 `json:"workersMax"`
	Template   *gmiTemplateResp    `json:"template,omitempty"`
	AccessURL  string              `json:"accessUrl,omitempty"`
}

type gmiTemplateResp struct {
	ImageName string `json:"imageName,omitempty"`
}

type gmiWorkerResponse struct {
	Id            string `json:"id"`
	Name          string `json:"name"`
	Image         string `json:"image"`
	DesiredStatus string `json:"desiredStatus"`
	LastStartedAt string `json:"lastStartedAt,omitempty"`
}

// gmiPodInfo represents pod data returned by gmiless /workers and /describe
type gmiPodInfo struct {
	Name         string            `json:"name"`
	Namespace    string            `json:"namespace"`
	UID          string            `json:"uid"`
	Phase        string            `json:"phase"`
	Status       string            `json:"status"`
	Reason       string            `json:"reason"`
	Message      string            `json:"message"`
	IP           string            `json:"ip"`
	NodeName     string            `json:"nodeName"`
	CreatedAt    string            `json:"createdAt"`
	StartedAt    string            `json:"startedAt"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	RestartCount int               `json:"restartCount"`
	Ready        bool              `json:"ready"`
	Containers   []struct {
		Name  string `json:"name"`
		Image string `json:"image"`
		Env   []struct {
			Name      string `json:"name"`
			Value     string `json:"value,omitempty"`
			ValueFrom *struct {
				FieldRef *struct {
					FieldPath string `json:"fieldPath"`
				} `json:"fieldRef,omitempty"`
			} `json:"valueFrom,omitempty"`
		} `json:"env"`
		Ready     bool `json:"ready"`
		Resources struct {
			Limits   map[string]string `json:"limits"`
			Requests map[string]string `json:"requests"`
		} `json:"resources"`
	} `json:"containers"`
	Conditions []struct {
		Type               string `json:"type"`
		Status             string `json:"status"`
		LastTransitionTime string `json:"lastTransitionTime"`
	} `json:"conditions"`
	Volumes []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"volumes"`
	DeletionTimestamp string `json:"deletionTimestamp,omitempty"`
}

// ========================================
// Internal types
// ========================================

// WorkerStatusChangeCallback is called when a worker's status changes
type WorkerStatusChangeCallback func(workerID, endpoint string, info *interfaces.PodInfo)

// WorkerDeleteCallback is called when a worker is deleted
type WorkerDeleteCallback func(workerID, endpoint string)

// gmiWorkerState tracks a worker's last known state
type gmiWorkerState struct {
	ID        string
	Endpoint  string
	Status    string
	CreatedAt string
	StartedAt string
}
