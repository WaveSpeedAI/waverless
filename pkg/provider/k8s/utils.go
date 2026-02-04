package k8s

import (
	"waverless/pkg/constants"

	corev1 "k8s.io/api/core/v1"
)

// GetPodEndpoint extracts endpoint name from pod labels
func GetPodEndpoint(pod *corev1.Pod) string {
	if pod == nil || pod.Labels == nil {
		return ""
	}
	return pod.Labels[constants.LabelApp]
}

// IsManagedPod checks if pod is managed by waverless
func IsManagedPod(pod *corev1.Pod) bool {
	return GetPodEndpoint(pod) != ""
}
