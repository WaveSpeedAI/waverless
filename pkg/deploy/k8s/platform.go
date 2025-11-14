package k8s

// Platform K8s 平台接口
type Platform interface {
	// GetName 获取平台名称
	GetName() string

	// CustomizeAnnotations 定制 annotations
	CustomizeAnnotations(annotations map[string]string, spec *ResourceSpec) map[string]string

	// GetNasDriver 获取 NAS 驱动
	GetNasDriver() string
}

// GenericPlatform 通用 K8s 平台
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

// AliyunACKPlatform 阿里云 ACK 平台
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

// AWSEKS 平台
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

// PlatformFactory 平台工厂
type PlatformFactory struct{}

// NewPlatformFactory 创建平台工厂
func NewPlatformFactory() *PlatformFactory {
	return &PlatformFactory{}
}

// CreatePlatform 创建平台实例
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
