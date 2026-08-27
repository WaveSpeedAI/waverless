package gmi

import (
	"waverless/pkg/interfaces"
)

// ========================================
// BFF Phase → waverless Status mapping
// ========================================

// mapPhaseToStatus converts ieops-v2 ModelDeployment Phase to waverless status string.
func mapPhaseToStatus(phase string) string {
	switch phase {
	case "Running":
		return "RUNNING"
	case "Deploying":
		return "DEPLOYING"
	case "Draining":
		return "DRAINING"
	case "", "Pending", "Scheduling", "WaitingForGPU", "Planned":
		return "PENDING"
	case "Failed":
		return "FAILED"
	default:
		return "UNKNOWN"
	}
}

// ========================================
// BFF response → waverless interfaces conversion
// ========================================

// convertBFFModelToAppInfo converts a BFF model response to waverless AppInfo.
func convertBFFModelToAppInfo(m *bffModelResponse) *interfaces.AppInfo {
	return &interfaces.AppInfo{
		Name:              m.Name,
		Status:            mapPhaseToStatus(m.Status),
		Replicas:          m.DesiredReplicas,
		ReadyReplicas:     m.ReadyReplicas,
		AvailableReplicas: m.AvailableReplicas,
		Image:             m.Image,
		CreatedAt:         m.CreatedAt,
		ShmSize:           m.ShmSize,
		Labels:            map[string]string{},
	}
}

// convertBFFPodToPodInfo converts a BFF pod status to waverless PodInfo.
func convertBFFPodToPodInfo(pod *bffPodStatus) *interfaces.PodInfo {
	status := pod.Phase
	if pod.Ready {
		status = "Running"
	}

	reason := pod.Reason
	message := pod.Message
	if pod.Ready && reason == "" {
		reason = "Ready"
		message = "All containers are ready"
	}

	return &interfaces.PodInfo{
		Name:              pod.PodName,
		WorkerID:          pod.PodName, // ieops-v2: workerID = podName
		NodeName:          pod.NodeID,
		Phase:             pod.Phase,
		Status:            status,
		Reason:            reason,
		Message:           message,
		CreatedAt:         pod.CreatedAt,
		StartedAt:         pod.StartedAt,
		ReadyAt:           pod.ReadyAt,
		DeletionTimestamp: pod.DeletionTimestamp,
		RestartCount:      pod.RestartCount,
		Labels:            map[string]string{},
	}
}

// ========================================
// BFF request builders
// ========================================

// gpuProfileInfo holds GPU family and compute capability for BFF gpuProfile.
type gpuProfileInfo struct {
	Family string
	MinCC  string
}

// specToGPUProfileMap maps waverless spec names to BFF GPU profile parameters.
var specToGPUProfileMap = map[string]gpuProfileInfo{
	"h200-single":      {Family: "H200", MinCC: "9.0"},
	"h100-single-hbm3": {Family: "H100", MinCC: "9.0"},
	"h100-single":      {Family: "H100", MinCC: "9.0"},
	"h100-pcie-single": {Family: "H100", MinCC: "9.0"},
	"a100-single":      {Family: "A100", MinCC: "8.0"},
	"5090-single":      {Family: "RTX5090", MinCC: "10.0"},
	"4090-single":      {Family: "RTX4090", MinCC: "8.9"},
	"a6000-single":     {Family: "RTXA6000", MinCC: "8.6"},
	"l40-single":       {Family: "L40", MinCC: "8.9"},
	"gpu-1x-l40s":      {Family: "L40S", MinCC: "8.9"},
}

// buildBFFGPUProfile builds a BFF gpuProfile from a waverless DeployRequest.
// For CPU-only (gpuCount=0), returns a minimal profile.
func buildBFFGPUProfile(specName string, gpuCount int) map[string]any {
	if gpuCount <= 0 {
		return map[string]any{
			"gpuCountPerPod": 0,
		}
	}

	profile := map[string]any{
		"gpuCountPerPod": gpuCount,
	}

	if info, ok := specToGPUProfileMap[specName]; ok {
		profile["acceptedFamilies"] = []string{info.Family}
		profile["minCC"] = info.MinCC
		profile["optimalFamily"] = info.Family
	} else if specName != "" {
		// Unknown spec — use spec name as family hint
		profile["acceptedFamilies"] = []string{specName}
		profile["minCC"] = "7.0"
	} else {
		// No spec — use generic GPU
		profile["acceptedFamilies"] = []string{"H100"}
		profile["minCC"] = "7.0"
	}

	return profile
}

