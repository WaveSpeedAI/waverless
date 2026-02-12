package gmi

import (
	"strings"

	"waverless/pkg/interfaces"
)

// ========================================
// Spec name → GPU type mapping
// ========================================

// specToGPUTypeMap maps waverless spec names to GPU type IDs used by gmiless
var specToGPUTypeMap = map[string]string{
	"h100-single-hbm3": "NVIDIA-H100-80GB-HBM3",
	"h100-single":      "NVIDIA-H100-80GB-HBM3",
	"h100-pcie-single": "NVIDIA-H100-PCIe",
	"a100-single":      "NVIDIA-A100-80GB-PCIe",
	"5090-single":      "NVIDIA-GeForce-RTX-5090",
	"4090-single":      "NVIDIA-GeForce-RTX-4090",
	"a6000-single":     "NVIDIA-RTX-A6000",
	"l40-single":       "NVIDIA-L40",
}

// specNameToGPUType converts a waverless spec name to a gmiless GPU type ID.
// If no mapping exists, returns the spec name as-is (assumes it's already a GPU type).
func specNameToGPUType(specName string) string {
	if gpuType, ok := specToGPUTypeMap[specName]; ok {
		return gpuType
	}
	return specName
}

// ========================================
// Response → AppInfo conversion
// ========================================

// convertToAppInfo converts gmiless endpoint response to waverless AppInfo
func convertToAppInfo(resp *gmiEndpointResponse) *interfaces.AppInfo {
	status := resp.Status
	if status == "" {
		if resp.Replicas == 0 {
			status = "Stopped"
		} else {
			status = "Pending"
		}
	}

	// Count ready workers
	var readyReplicas, availableReplicas int32
	for _, w := range resp.Workers {
		if strings.EqualFold(w.DesiredStatus, "ONLINE") || strings.EqualFold(w.DesiredStatus, "BUSY") {
			availableReplicas++
			readyReplicas++
		}
	}

	image := resp.Image
	if image == "" && resp.Template != nil {
		image = resp.Template.ImageName
	}

	return &interfaces.AppInfo{
		Name:              resp.Name,
		Status:            status,
		Replicas:          int32(resp.Replicas),
		ReadyReplicas:     readyReplicas,
		AvailableReplicas: availableReplicas,
		Image:             image,
		Labels:            map[string]string{},
		CreatedAt:         resp.CreatedAt,
	}
}

// ========================================
// Response → PodInfo / PodDetail conversion
// ========================================

// convertPodInfoFromGMI converts gmiPodInfo to interfaces.PodInfo
func convertPodInfoFromGMI(pod *gmiPodInfo) *interfaces.PodInfo {
	return &interfaces.PodInfo{
		Name:      pod.Name,
		Phase:     pod.Phase,
		Status:    pod.Status,
		Reason:    pod.Reason,
		Message:   pod.Message,
		IP:        pod.IP,
		NodeName:  pod.NodeName,
		CreatedAt: pod.CreatedAt,
		StartedAt: pod.StartedAt,
		Labels:    pod.Labels,
	}
}

// convertPodDetailFromGMI converts gmiPodInfo to interfaces.PodDetail
func convertPodDetailFromGMI(pod *gmiPodInfo) *interfaces.PodDetail {
	// Convert containers
	containers := make([]interfaces.ContainerInfo, len(pod.Containers))
	for i, c := range pod.Containers {
		env := make([]interfaces.EnvVar, 0, len(c.Env))
		for _, e := range c.Env {
			env = append(env, interfaces.EnvVar{Name: e.Name, Value: e.Value})
		}

		resources := make(map[string]interface{})
		if len(c.Resources.Requests) > 0 || len(c.Resources.Limits) > 0 {
			resources["requests"] = c.Resources.Requests
			resources["limits"] = c.Resources.Limits
		}

		state := "Running"
		if !c.Ready {
			state = "Waiting"
		}

		containers[i] = interfaces.ContainerInfo{
			Name:      c.Name,
			Image:     c.Image,
			State:     state,
			Ready:     c.Ready,
			Resources: resources,
			Env:       env,
		}
	}

	// Convert conditions
	conditions := make([]interfaces.PodCondition, len(pod.Conditions))
	for i, c := range pod.Conditions {
		conditions[i] = interfaces.PodCondition{
			Type:               c.Type,
			Status:             c.Status,
			LastTransitionTime: c.LastTransitionTime,
		}
	}

	// Convert volumes
	volumes := make([]interfaces.VolumeInfo, len(pod.Volumes))
	for i, v := range pod.Volumes {
		volumes[i] = interfaces.VolumeInfo{Name: v.Name, Type: v.Type}
	}

	return &interfaces.PodDetail{
		PodInfo: &interfaces.PodInfo{
			Name:         pod.Name,
			Phase:        pod.Phase,
			Status:       pod.Status,
			Reason:       pod.Reason,
			Message:      pod.Message,
			IP:           pod.IP,
			NodeName:     pod.NodeName,
			CreatedAt:    pod.CreatedAt,
			StartedAt:    pod.StartedAt,
			Labels:       pod.Labels,
			RestartCount: int32(pod.RestartCount),
		},
		Namespace:   pod.Namespace,
		UID:         pod.UID,
		Annotations: pod.Annotations,
		Containers:  containers,
		Conditions:  conditions,
		Events:      []interfaces.PodEvent{},
		Volumes:     volumes,
	}
}

// convertWorkerToPodInfo converts gmiWorkerResponse to interfaces.PodInfo (for status sync)
func convertWorkerToPodInfo(worker *gmiWorkerResponse, endpoint string) *interfaces.PodInfo {
	status := worker.DesiredStatus
	phase := "Running"
	if strings.EqualFold(status, "STARTING") {
		phase = "Pending"
	}

	return &interfaces.PodInfo{
		Name:      worker.Name,
		Phase:     phase,
		Status:    status,
		CreatedAt: worker.LastStartedAt,
		StartedAt: worker.LastStartedAt,
		Labels:    map[string]string{"app": endpoint},
	}
}
