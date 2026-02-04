package k8s

import (
	corev1 "k8s.io/api/core/v1"
)

// Platform defines K8s platform interface
type Platform interface {
	// GetName returns the platform name
	GetName() string

	// CustomizeAnnotations customizes annotations
	CustomizeAnnotations(annotations map[string]string, spec *ResourceSpec) map[string]string

	// GetNasDriver returns the NAS driver
	GetNasDriver() string

	// DetectSpotInterruption detects spot interruption
	DetectSpotInterruption(pod *corev1.Pod) (bool, string)
}

// GenericPlatform represents a generic K8s platform
type GenericPlatform struct{}

func (p *GenericPlatform) GetName() string {
	return "generic"
}

func (p *GenericPlatform) CustomizeAnnotations(annotations map[string]string, spec *ResourceSpec) map[string]string {
	// Generic platform doesn't need special annotations
	return annotations
}

func (p *GenericPlatform) GetNasDriver() string {
	return "nfs.csi.k8s.io"
}

func (p *GenericPlatform) DetectSpotInterruption(pod *corev1.Pod) (bool, string) {
	// Generic spot interruption detection
	if pod.Annotations != nil {
		patterns := []struct {
			key    string
			reason string
		}{
			{"spot.io/interruption-detected", "Spot Interruption"},
			{"preemptible.interruption", "Preemptible Interruption"},
			{"node.termination", "Node Termination"},
		}

		for _, pattern := range patterns {
			if detected, exists := pod.Annotations[pattern.key]; exists && detected == "true" {
				return true, pattern.reason
			}
		}
	}
	return false, ""
}

// AliyunACKPlatform represents Alibaba Cloud ACK platform
type AliyunACKPlatform struct{}

func (p *AliyunACKPlatform) GetName() string {
	return "aliyun-ack"
}

func (p *AliyunACKPlatform) CustomizeAnnotations(annotations map[string]string, spec *ResourceSpec) map[string]string {
	// Add Alibaba Cloud specific annotations
	if annotations == nil {
		annotations = make(map[string]string)
	}

	// Image acceleration
	annotations["k8s.aliyun.com/image-accelerate-mode"] = "on-demand"

	// Don't use EIP by default
	if _, exists := annotations["k8s.aliyun.com/pod-with-eip"]; !exists {
		annotations["k8s.aliyun.com/pod-with-eip"] = "false"
	}

	return annotations
}

func (p *AliyunACKPlatform) GetNasDriver() string {
	return "nasplugin.csi.alibabacloud.com"
}

func (p *AliyunACKPlatform) DetectSpotInterruption(pod *corev1.Pod) (bool, string) {
	// Alibaba Cloud preemptible instance detection
	if pod.Annotations != nil {
		if detected, exists := pod.Annotations["alicloud.com/preemptible-interruption"]; exists && detected == "true" {
			return true, "Alibaba Preemptible Interruption"
		}
	}
	return false, ""
}

// AWSEKSPlatform represents AWS EKS platform
type AWSEKSPlatform struct{}

func (p *AWSEKSPlatform) GetName() string {
	return "aws-eks"
}

func (p *AWSEKSPlatform) CustomizeAnnotations(annotations map[string]string, spec *ResourceSpec) map[string]string {
	// AWS EKS specific configuration
	return annotations
}

func (p *AWSEKSPlatform) GetNasDriver() string {
	return "efs.csi.aws.com"
}

func (p *AWSEKSPlatform) DetectSpotInterruption(pod *corev1.Pod) (bool, string) {
	// AWS Karpenter spot interruption detection
	// aws not spot interruption events
	return false, ""
}

// PlatformFactory creates platform instances
type PlatformFactory struct{}

// NewPlatformFactory creates a new platform factory
func NewPlatformFactory() *PlatformFactory {
	return &PlatformFactory{}
}

// CreatePlatform creates a platform instance
func (f *PlatformFactory) CreatePlatform(platformName string) Platform {
	switch platformName {
	case "aliyun-ack":
		return &AliyunACKPlatform{}
	case "aws-eks":
		return &AWSEKSPlatform{}
	default:
		return &GenericPlatform{}
	}
}
