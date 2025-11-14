package k8s

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ResourceSpec 资源规格定义
type ResourceSpec struct {
	Name         string                       `yaml:"name" json:"name"`
	DisplayName  string                       `yaml:"displayName" json:"displayName"`
	Category     string                       `yaml:"category" json:"category"` // cpu, gpu
	Resources    SpecResources                `yaml:"resources" json:"resources"`
	Platforms    map[string]PlatformConfig    `yaml:"platforms" json:"platforms"`
}

// SpecResources 规格资源
type SpecResources struct {
	CPU               string `yaml:"cpu,omitempty" json:"cpu,omitempty"`
	Memory            string `yaml:"memory" json:"memory"`
	GPU               string `yaml:"gpu,omitempty" json:"gpu,omitempty"`
	GpuType           string `yaml:"gpuType,omitempty" json:"gpuType,omitempty"`
	Disk              string `yaml:"disk" json:"disk"`
	EphemeralStorage  string `yaml:"ephemeralStorage" json:"ephemeralStorage"`
}

// PlatformConfig 平台特定配置
type PlatformConfig struct {
	NodeSelector map[string]string `yaml:"nodeSelector" json:"nodeSelector"`
	Tolerations  []Toleration      `yaml:"tolerations" json:"tolerations"`
	Labels       map[string]string `yaml:"labels" json:"labels"`
	Annotations  map[string]string `yaml:"annotations" json:"annotations"`
}

// Toleration 容忍度
type Toleration struct {
	Key      string `yaml:"key" json:"key"`
	Operator string `yaml:"operator" json:"operator"`
	Value    string `yaml:"value,omitempty" json:"value,omitempty"`
	Effect   string `yaml:"effect" json:"effect"`
}

// SpecsConfig 规格配置文件
type SpecsConfig struct {
	Specs []ResourceSpec `yaml:"specs"`
}

// SpecManager 规格管理器
type SpecManager struct {
	specs map[string]*ResourceSpec
}

// NewSpecManager 创建规格管理器
func NewSpecManager(configPath string) (*SpecManager, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read specs config: %v", err)
	}

	var config SpecsConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse specs config: %v", err)
	}

	specs := make(map[string]*ResourceSpec)
	for i := range config.Specs {
		spec := &config.Specs[i]
		specs[spec.Name] = spec
	}

	return &SpecManager{
		specs: specs,
	}, nil
}

// GetSpec 获取规格
func (m *SpecManager) GetSpec(name string) (*ResourceSpec, error) {
	spec, exists := m.specs[name]
	if !exists {
		return nil, fmt.Errorf("spec not found: %s", name)
	}
	return spec, nil
}

// ListSpecs 列出所有规格
func (m *SpecManager) ListSpecs() []*ResourceSpec {
	result := make([]*ResourceSpec, 0, len(m.specs))
	for _, spec := range m.specs {
		result = append(result, spec)
	}
	return result
}

// ListSpecsByCategory 按类别列出规格
func (m *SpecManager) ListSpecsByCategory(category string) []*ResourceSpec {
	result := make([]*ResourceSpec, 0)
	for _, spec := range m.specs {
		if spec.Category == category {
			result = append(result, spec)
		}
	}
	return result
}

// GetPlatformConfig 获取平台特定配置
func (s *ResourceSpec) GetPlatformConfig(platform string) PlatformConfig {
	if config, exists := s.Platforms[platform]; exists {
		return config
	}
	// Return generic config as default
	if config, exists := s.Platforms["generic"]; exists {
		return config
	}
	return PlatformConfig{}
}
