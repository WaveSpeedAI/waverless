package k8s

import (
	"context"
	"fmt"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/yaml"

	"waverless/pkg/interfaces"
	"waverless/pkg/logger"
)

// GetPods gets all Pod information for specified endpoint (including Pending, Running, Terminating)
// Uses Informer cache instead of directly calling K8s API to reduce API Server pressure
func (m *Manager) GetPods(ctx context.Context, endpoint string) ([]*interfaces.PodInfo, error) {
	// Use label selector to query pods
	var selector labels.Selector
	if endpoint == "" {
		// Get all pods managed by waverless
		selector = labels.SelectorFromSet(labels.Set{"managed-by": "waverless"})
	} else {
		// Get pods for specified endpoint
		selector = labels.SelectorFromSet(labels.Set{"app": endpoint})
	}

	// Get pods from Informer cache (without calling API Server)
	pods, err := m.podLister.Pods(m.namespace).List(selector)
	if err != nil {
		return nil, fmt.Errorf("failed to list pods from cache: %w", err)
	}

	logger.InfoCtx(ctx, "Found %d pods for endpoint '%s' from informer cache", len(pods), endpoint)

	podInfos := make([]*interfaces.PodInfo, 0, len(pods))
	for _, pod := range pods {
		podInfo := m.convertPodToInfo(pod)
		podInfos = append(podInfos, podInfo)
	}

	// Sort by creation time (newest first)
	sort.Slice(podInfos, func(i, j int) bool {
		return podInfos[i].CreatedAt > podInfos[j].CreatedAt
	})

	return podInfos, nil
}

// DescribePod gets detailed Pod information (similar to kubectl describe)
// Uses Informer cache to get Pod info, Events still need to be fetched from API
func (m *Manager) DescribePod(ctx context.Context, endpoint string, podName string) (*interfaces.PodDetail, error) {
	// Get Pod from Informer cache (without calling API Server)
	pod, err := m.podLister.Pods(m.namespace).Get(podName)
	if err != nil {
		return nil, fmt.Errorf("failed to get pod from cache: %w", err)
	}

	// Verify Pod belongs to specified endpoint (using "app" label)
	if pod.Labels["app"] != endpoint {
		return nil, fmt.Errorf("pod %s does not belong to endpoint %s", podName, endpoint)
	}

	// Convert to PodInfo
	podInfo := m.convertPodToInfo(pod)

	// Get Events
	events, err := m.getPodEvents(ctx, pod)
	if err != nil {
		logger.WarnCtx(ctx, "failed to get events for pod %s: %v", podName, err)
		events = []interfaces.PodEvent{}
	}

	// Build detailed information
	detail := &interfaces.PodDetail{
		PodInfo:         podInfo,
		Namespace:       pod.Namespace,
		UID:             string(pod.UID),
		Annotations:     pod.Annotations,
		Containers:      m.convertContainers(pod.Spec.Containers, pod.Status.ContainerStatuses),
		InitContainers:  m.convertContainers(pod.Spec.InitContainers, pod.Status.InitContainerStatuses),
		Conditions:      m.convertPodConditions(pod.Status.Conditions),
		Events:          events,
		OwnerReferences: m.convertOwnerReferences(pod.OwnerReferences),
		Tolerations:     m.convertTolerations(pod.Spec.Tolerations),
		Affinity:        m.convertAffinity(pod.Spec.Affinity),
		Volumes:         m.convertVolumes(pod.Spec.Volumes),
	}

	return detail, nil
}

// convertPodToInfo converts K8s Pod to PodInfo
func (m *Manager) convertPodToInfo(pod *corev1.Pod) *interfaces.PodInfo {
	// Determine detailed Pod status
	status, reason, message := m.getPodStatus(pod)

	// Calculate restart count
	restartCount := int32(0)
	for _, cs := range pod.Status.ContainerStatuses {
		restartCount += cs.RestartCount
	}

	podInfo := &interfaces.PodInfo{
		Name:         pod.Name,
		Phase:        string(pod.Status.Phase),
		Status:       status,
		Reason:       reason,
		Message:      message,
		IP:           pod.Status.PodIP,
		NodeName:     pod.Spec.NodeName,
		CreatedAt:    pod.CreationTimestamp.Format(time.RFC3339),
		RestartCount: restartCount,
		Labels:       pod.Labels,
	}

	if pod.Status.StartTime != nil {
		podInfo.StartedAt = pod.Status.StartTime.Format(time.RFC3339)
	}

	if pod.DeletionTimestamp != nil {
		podInfo.DeletionTimestamp = pod.DeletionTimestamp.Format(time.RFC3339)
	}

	return podInfo
}

// getPodStatus gets detailed Pod status
func (m *Manager) getPodStatus(pod *corev1.Pod) (status, reason, message string) {
	// If being deleted
	if pod.DeletionTimestamp != nil {
		return "Terminating", "PodTerminating", fmt.Sprintf("Pod is terminating (grace period: %ds)", *pod.DeletionGracePeriodSeconds)
	}

	// Determine based on Phase
	switch pod.Status.Phase {
	case corev1.PodPending:
		// Check if pulling image or initializing
		for _, cs := range pod.Status.ContainerStatuses {
			if cs.State.Waiting != nil {
				return "Creating", cs.State.Waiting.Reason, cs.State.Waiting.Message
			}
		}
		// Check init containers
		for _, ics := range pod.Status.InitContainerStatuses {
			if ics.State.Waiting != nil {
				return "Initializing", ics.State.Waiting.Reason, ics.State.Waiting.Message
			}
			if ics.State.Running != nil {
				return "Initializing", "InitContainerRunning", fmt.Sprintf("Init container %s is running", ics.Name)
			}
		}
		// Check Pod Conditions
		for _, cond := range pod.Status.Conditions{
			if cond.Type == corev1.PodScheduled && cond.Status != corev1.ConditionTrue {
				return "Pending", cond.Reason, cond.Message
			}
		}
		return "Creating", "ContainerCreating", "Containers are being created"

	case corev1.PodRunning:
		// Check if all containers are Ready
		allReady := true
		for _, cs := range pod.Status.ContainerStatuses {
			if !cs.Ready {
				allReady = false
				if cs.State.Waiting != nil {
					return "Starting", cs.State.Waiting.Reason, cs.State.Waiting.Message
				}
				if cs.State.Terminated != nil {
					return "CrashLoopBackOff", cs.State.Terminated.Reason, cs.State.Terminated.Message
				}
			}
		}
		if allReady {
			return "Running", "Ready", "All containers are ready"
		}
		return "Running", "ContainerNotReady", "Some containers are not ready"

	case corev1.PodSucceeded:
		return "Succeeded", "Completed", "Pod completed successfully"

	case corev1.PodFailed:
		// Check failure reason
		for _, cs := range pod.Status.ContainerStatuses {
			if cs.State.Terminated != nil {
				return "Failed", cs.State.Terminated.Reason, cs.State.Terminated.Message
			}
		}
		return "Failed", "PodFailed", pod.Status.Message

	case corev1.PodUnknown:
		return "Unknown", "Unknown", pod.Status.Message

	default:
		return string(pod.Status.Phase), "", ""
	}
}

// getPodEvents gets Events related to Pod
func (m *Manager) getPodEvents(ctx context.Context, pod *corev1.Pod) ([]interfaces.PodEvent, error) {
	// Use only pod name for filtering, as UID might cause issues with some K8s versions
	// This is sufficient since pod names are unique within a namespace
	events, err := m.client.CoreV1().Events(m.namespace).List(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("involvedObject.name=%s,involvedObject.kind=Pod", pod.Name),
	})
	if err != nil {
		return nil, err
	}

	podEvents := make([]interfaces.PodEvent, 0, len(events.Items))
	for _, event := range events.Items {
		// Handle both old (FirstTimestamp/LastTimestamp) and new (EventTime) Event API
		// Some events only have EventTime, some only have FirstTimestamp/LastTimestamp
		var firstSeen, lastSeen string

		// Try EventTime first (new API), fallback to FirstTimestamp (old API)
		if !event.EventTime.IsZero() {
			firstSeen = event.EventTime.Format(time.RFC3339)
			lastSeen = event.EventTime.Format(time.RFC3339)
		} else if !event.FirstTimestamp.IsZero() {
			firstSeen = event.FirstTimestamp.Format(time.RFC3339)
		}

		// For LastTimestamp, prefer it over EventTime if available
		if !event.LastTimestamp.IsZero() {
			lastSeen = event.LastTimestamp.Format(time.RFC3339)
		} else if lastSeen == "" && !event.EventTime.IsZero() {
			lastSeen = event.EventTime.Format(time.RFC3339)
		}

		podEvents = append(podEvents, interfaces.PodEvent{
			Type:      event.Type,
			Reason:    event.Reason,
			Message:   event.Message,
			Count:     event.Count,
			FirstSeen: firstSeen,
			LastSeen:  lastSeen,
		})
	}

	// Sort by last seen time (newest first)
	sort.Slice(podEvents, func(i, j int) bool {
		return podEvents[i].LastSeen > podEvents[j].LastSeen
	})

	return podEvents, nil
}

// convertContainers converts container information
func (m *Manager) convertContainers(specs []corev1.Container, statuses []corev1.ContainerStatus) []interfaces.ContainerInfo {
	containers := make([]interfaces.ContainerInfo, 0, len(specs))

	for _, spec := range specs {
		container := interfaces.ContainerInfo{
			Name:  spec.Name,
			Image: spec.Image,
			Ports: m.convertPorts(spec.Ports),
			Env:   m.convertEnv(spec.Env),
			Resources: map[string]interface{}{
				"requests": spec.Resources.Requests,
				"limits":   spec.Resources.Limits,
			},
		}

		// Match status
		for _, status := range statuses {
			if status.Name == spec.Name {
				container.Ready = status.Ready
				container.RestartCount = status.RestartCount

				if status.State.Waiting != nil {
					container.State = "Waiting"
					container.Reason = status.State.Waiting.Reason
					container.Message = status.State.Waiting.Message
				} else if status.State.Running != nil {
					container.State = "Running"
					container.StartedAt = status.State.Running.StartedAt.Format(time.RFC3339)
				} else if status.State.Terminated != nil {
					container.State = "Terminated"
					container.Reason = status.State.Terminated.Reason
					container.Message = status.State.Terminated.Message
					container.ExitCode = status.State.Terminated.ExitCode
					container.StartedAt = status.State.Terminated.StartedAt.Format(time.RFC3339)
					container.FinishedAt = status.State.Terminated.FinishedAt.Format(time.RFC3339)
				}
				break
			}
		}

		containers = append(containers, container)
	}

	return containers
}

// convertPorts converts port information
func (m *Manager) convertPorts(ports []corev1.ContainerPort) []interfaces.ContainerPort {
	result := make([]interfaces.ContainerPort, len(ports))
	for i, p := range ports {
		result[i] = interfaces.ContainerPort{
			Name:          p.Name,
			ContainerPort: p.ContainerPort,
			Protocol:      string(p.Protocol),
		}
	}
	return result
}

// convertEnv converts environment variables
func (m *Manager) convertEnv(envs []corev1.EnvVar) []interfaces.EnvVar {
	result := make([]interfaces.EnvVar, 0, len(envs))
	for _, e := range envs {
		// Only return simple values, exclude references from ConfigMap/Secret
		if e.Value != "" {
			result = append(result, interfaces.EnvVar{
				Name:  e.Name,
				Value: e.Value,
			})
		}
	}
	return result
}

// convertPodConditions converts Pod Conditions
func (m *Manager) convertPodConditions(conditions []corev1.PodCondition) []interfaces.PodCondition {
	result := make([]interfaces.PodCondition, len(conditions))
	for i, c := range conditions {
		result[i] = interfaces.PodCondition{
			Type:               string(c.Type),
			Status:             string(c.Status),
			Reason:             c.Reason,
			Message:            c.Message,
			LastTransitionTime: c.LastTransitionTime.Format(time.RFC3339),
		}
	}
	return result
}

// convertOwnerReferences converts Owner References
func (m *Manager) convertOwnerReferences(refs []metav1.OwnerReference) []interfaces.OwnerReference {
	result := make([]interfaces.OwnerReference, len(refs))
	for i, r := range refs {
		result[i] = interfaces.OwnerReference{
			Kind: r.Kind,
			Name: r.Name,
			UID:  string(r.UID),
		}
	}
	return result
}

// convertTolerations converts Tolerations
func (m *Manager) convertTolerations(tolerations []corev1.Toleration) []map[string]string {
	result := make([]map[string]string, len(tolerations))
	for i, t := range tolerations {
		result[i] = map[string]string{
			"key":      t.Key,
			"operator": string(t.Operator),
			"value":    t.Value,
			"effect":   string(t.Effect),
		}
	}
	return result
}

// convertAffinity converts Affinity
func (m *Manager) convertAffinity(affinity *corev1.Affinity) map[string]interface{} {
	if affinity == nil {
		return nil
	}
	// Simplified processing, return structured data directly
	result := make(map[string]interface{})
	if affinity.NodeAffinity != nil {
		result["nodeAffinity"] = "configured"
	}
	if affinity.PodAffinity != nil {
		result["podAffinity"] = "configured"
	}
	if affinity.PodAntiAffinity != nil {
		result["podAntiAffinity"] = "configured"
	}
	return result
}

// convertVolumes converts Volumes
func (m *Manager) convertVolumes(volumes []corev1.Volume) []interfaces.VolumeInfo {
	result := make([]interfaces.VolumeInfo, len(volumes))
	for i, v := range volumes {
		volumeInfo := interfaces.VolumeInfo{
			Name:   v.Name,
			Source: make(map[string]interface{}),
		}

		// Determine volume type
		if v.EmptyDir != nil {
			volumeInfo.Type = "EmptyDir"
		} else if v.HostPath != nil {
			volumeInfo.Type = "HostPath"
			volumeInfo.Source["path"] = v.HostPath.Path
		} else if v.ConfigMap != nil {
			volumeInfo.Type = "ConfigMap"
			volumeInfo.Source["name"] = v.ConfigMap.Name
		} else if v.Secret != nil {
			volumeInfo.Type = "Secret"
			volumeInfo.Source["name"] = v.Secret.SecretName
		} else if v.PersistentVolumeClaim != nil {
			volumeInfo.Type = "PersistentVolumeClaim"
			volumeInfo.Source["claimName"] = v.PersistentVolumeClaim.ClaimName
		} else {
			volumeInfo.Type = "Other"
		}

		result[i] = volumeInfo
	}
	return result
}

// GetPodYAML gets Pod YAML (similar to kubectl get pod -o yaml)
func (m *Manager) GetPodYAML(ctx context.Context, endpoint string, podName string) (string, error) {
	// Get Pod from API Server (not cache, to get full details)
	pod, err := m.client.CoreV1().Pods(m.namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get pod: %w", err)
	}

	// Verify Pod belongs to specified endpoint (using "app" label)
	if pod.Labels["app"] != endpoint {
		return "", fmt.Errorf("pod %s does not belong to endpoint %s", podName, endpoint)
	}

	// Convert Pod to YAML
	yamlData, err := yaml.Marshal(pod)
	if err != nil {
		return "", fmt.Errorf("failed to marshal pod to yaml: %w", err)
	}

	return string(yamlData), nil
}
