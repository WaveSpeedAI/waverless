package vo

import (
	"waverless/pkg/interfaces"
)

// EndpointResponse represents the endpoint response structure
// Used for /api/v1/endpoints/{name} and similar endpoints
type EndpointResponse struct {
	Name              string                   `json:"name"`
	DisplayName       string                   `json:"displayName,omitempty"`
	Description       string                   `json:"description,omitempty"`
	Namespace         string                   `json:"namespace,omitempty"`
	SpecName          string                   `json:"specName,omitempty"`
	Image             string                   `json:"image,omitempty"`
	ImagePrefix       string                   `json:"imagePrefix,omitempty"`
	Replicas          int                      `json:"replicas"`
	GpuCount          int                      `json:"gpuCount,omitempty"`
	TaskTimeout       int                      `json:"taskTimeout,omitempty"`
	MaxPendingTasks   int                      `json:"maxPendingTasks,omitempty"`
	Env               map[string]string        `json:"env,omitempty"`
	EnablePtrace      bool                     `json:"enablePtrace,omitempty"`
	Status            string                   `json:"status"`
	ReadyReplicas     int                      `json:"readyReplicas,omitempty"`
	AvailableReplicas int                      `json:"availableReplicas,omitempty"`
	ShmSize           string                   `json:"shmSize,omitempty"`
	VolumeMounts      []interfaces.VolumeMount `json:"volumeMounts,omitempty"`
	CreatedAt         string                   `json:"createdAt,omitempty"`
	UpdatedAt         string                   `json:"updatedAt,omitempty"`

	// Health status
	HealthStatus  string `json:"healthStatus,omitempty"`
	HealthMessage string `json:"healthMessage,omitempty"`

	// Autoscaler configuration
	MinReplicas       int     `json:"minReplicas,omitempty"`
	MaxReplicas       int     `json:"maxReplicas,omitempty"`
	Priority          int     `json:"priority,omitempty"`
	ScaleUpThreshold  int     `json:"scaleUpThreshold,omitempty"`
	ScaleDownIdleTime int     `json:"scaleDownIdleTime,omitempty"`
	ScaleUpCooldown   int     `json:"scaleUpCooldown,omitempty"`
	ScaleDownCooldown int     `json:"scaleDownCooldown,omitempty"`
	EnableDynamicPrio *bool   `json:"enableDynamicPrio,omitempty"`
	HighLoadThreshold int     `json:"highLoadThreshold,omitempty"`
	PriorityBoost     int     `json:"priorityBoost,omitempty"`
	AutoscalerEnabled *string `json:"autoscalerEnabled,omitempty"`
}

// EndpointListResponse represents the endpoint list response
type EndpointListResponse struct {
	Endpoints []*EndpointResponse `json:"endpoints"`
	Total     int                 `json:"total"`
}

// EndpointCreateResponse represents the endpoint creation response
type EndpointCreateResponse struct {
	Message   string `json:"message"`
	Endpoint  string `json:"endpoint"`
	CreatedAt string `json:"createdAt,omitempty"`
}

// EndpointUpdateResponse represents the endpoint update response
type EndpointUpdateResponse struct {
	Message  string `json:"message"`
	Endpoint string `json:"endpoint"`
}

// EndpointDeleteResponse represents the endpoint deletion response
type EndpointDeleteResponse struct {
	Message string `json:"message"`
	Name    string `json:"name"`
}

// FromEndpointMetadata converts EndpointMetadata to EndpointResponse
func FromEndpointMetadata(meta *interfaces.EndpointMetadata) *EndpointResponse {
	if meta == nil {
		return nil
	}

	return &EndpointResponse{
		Name:              meta.Name,
		DisplayName:       meta.DisplayName,
		Description:       meta.Description,
		Namespace:         meta.Namespace,
		SpecName:          meta.SpecName,
		Image:             meta.Image,
		ImagePrefix:       meta.ImagePrefix,
		Replicas:          meta.Replicas,
		GpuCount:          meta.GpuCount,
		TaskTimeout:       meta.TaskTimeout,
		MaxPendingTasks:   meta.MaxPendingTasks,
		Env:               meta.Env,
		EnablePtrace:      meta.EnablePtrace,
		Status:            meta.Status,
		ReadyReplicas:     meta.ReadyReplicas,
		AvailableReplicas: meta.AvailableReplicas,
		ShmSize:           meta.ShmSize,
		VolumeMounts:      meta.VolumeMounts,
		HealthStatus:      meta.HealthStatus,
		HealthMessage:     meta.HealthMessage,
		MinReplicas:       meta.MinReplicas,
		MaxReplicas:       meta.MaxReplicas,
		Priority:          meta.Priority,
		ScaleUpThreshold:  meta.ScaleUpThreshold,
		ScaleDownIdleTime: meta.ScaleDownIdleTime,
		ScaleUpCooldown:   meta.ScaleUpCooldown,
		ScaleDownCooldown: meta.ScaleDownCooldown,
		EnableDynamicPrio: meta.EnableDynamicPrio,
		HighLoadThreshold: meta.HighLoadThreshold,
		PriorityBoost:     meta.PriorityBoost,
		AutoscalerEnabled: meta.AutoscalerEnabled,
	}
}

// FromEndpointMetadataList converts a list of EndpointMetadata to EndpointResponse list
func FromEndpointMetadataList(metas []*interfaces.EndpointMetadata) []*EndpointResponse {
	result := make([]*EndpointResponse, len(metas))
	for i, meta := range metas {
		result[i] = FromEndpointMetadata(meta)
	}
	return result
}
