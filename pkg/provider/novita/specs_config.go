package novita

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"waverless/pkg/interfaces"
	"waverless/pkg/logger"
)

const (
	PlatformNovita = "novita"
)

// SpecRepositoryInterface defines the interface for spec repository
type SpecRepositoryInterface interface {
	GetSpec(ctx context.Context, name string) (*interfaces.SpecInfo, error)
	ListSpecs(ctx context.Context) ([]*interfaces.SpecInfo, error)
}

// ResourceSpec defines resource specification
type ResourceSpec struct {
	Name        string                    `yaml:"name" json:"name"`
	DisplayName string                    `yaml:"displayName" json:"displayName"`
	Category    string                    `yaml:"category" json:"category"` // cpu, gpu
	Resources   SpecResources             `yaml:"resources" json:"resources"`
	Platforms   map[string]PlatformConfig `yaml:"platforms" json:"platforms"`
}

// SpecResources defines spec resources
type SpecResources struct {
	CPU              string `yaml:"cpu,omitempty" json:"cpu,omitempty"`
	Memory           string `yaml:"memory" json:"memory"`
	GPU              string `yaml:"gpu,omitempty" json:"gpu,omitempty"`
	GpuType          string `yaml:"gpuType,omitempty" json:"gpuType,omitempty"`
	EphemeralStorage string `yaml:"ephemeralStorage" json:"ephemeralStorage"`
	ShmSize          string `yaml:"shmSize,omitempty" json:"shmSize,omitempty"` // Shared memory size
}

// PlatformConfig defines platform-specific configuration
type PlatformConfig struct {
	ProductID   string `yaml:"productId" json:"productId"`
	Region      string `yaml:"region" json:"region"`
	CudaVersion string `yaml:"cudaVersion" json:"cudaVersion"`
}

// SpecsConfig manages Novita specifications from specs.yaml
type SpecsConfig struct {
	specs     map[string]*ResourceSpec
	configDir string
	specRepo  SpecRepositoryInterface
}

// NewSpecsConfig creates a new specs configuration manager
func NewSpecsConfig(configDir string) (*SpecsConfig, error) {
	if configDir == "" {
		configDir = "config"
	}

	sc := &SpecsConfig{
		specs:     make(map[string]*ResourceSpec),
		configDir: configDir,
	}

	// Load specs from config file as fallback
	if err := sc.loadSpecs(); err != nil {
		logger.Warnf("Failed to load Novita specs from file: %v", err)
	}

	return sc, nil
}

// SetSpecRepository sets the spec repository for database access
func (sc *SpecsConfig) SetSpecRepository(repo SpecRepositoryInterface) {
	sc.specRepo = repo
}

// SpecsFileConfig represents the structure of specs.yaml file
type SpecsFileConfig struct {
	Specs []*ResourceSpec `yaml:"specs"`
}

// loadSpecs loads specs from specs.yaml
func (sc *SpecsConfig) loadSpecs() error {
	specsFile := filepath.Join(sc.configDir, "specs.yaml")

	data, err := os.ReadFile(specsFile)
	if err != nil {
		return fmt.Errorf("failed to read specs file: %w", err)
	}

	// Parse YAML file using SpecsFileConfig structure
	var config SpecsFileConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse specs file: %w", err)
	}

	// Clear and load specs - filter for Novita-compatible specs
	sc.specs = make(map[string]*ResourceSpec)
	loadedCount := 0

	for _, s := range config.Specs {
		sc.specs[s.Name] = s
		loadedCount++
		logger.Debugf("Loaded Novita spec: %s  %+v", s.Name, s.Platforms)
	}

	if loadedCount == 0 {
		logger.Warnf("No Novita-compatible specs found in %s", specsFile)
	} else {
		logger.Infof("Loaded %d Novita-compatible specs from %s", loadedCount, specsFile)
	}
	return nil
}

// GetSpec returns a specific spec info by name (database first, then YAML fallback)
func (sc *SpecsConfig) GetSpec(specName string) (*interfaces.SpecInfo, error) {
	// Try database first
	if sc.specRepo != nil {
		dbSpec, err := sc.specRepo.GetSpec(context.Background(), specName)
		if err == nil && dbSpec != nil {
			return dbSpec, nil
		}
		logger.Warnf("Failed to get spec from database, falling back to YAML: %v", err)
	}

	// Fallback to YAML
	resourceSpec, ok := sc.specs[specName]
	if !ok {
		return nil, fmt.Errorf("spec %s not found", specName)
	}

	return sc.convertToSpecInfo(resourceSpec), nil
}

// ListSpecs returns all available spec infos (database first, then YAML fallback)
func (sc *SpecsConfig) ListSpecs() []*interfaces.SpecInfo {
	// Try database first
	if sc.specRepo != nil {
		dbSpecs, err := sc.specRepo.ListSpecs(context.Background())
		if err == nil && len(dbSpecs) > 0 {
			return dbSpecs
		}
		logger.Warnf("Failed to list specs from database, falling back to YAML: %v", err)
	}

	// Fallback to YAML
	specs := make([]*interfaces.SpecInfo, 0, len(sc.specs))
	for _, spec := range sc.specs {
		specs = append(specs, sc.convertToSpecInfo(spec))
	}
	return specs
}

// convertToSpecInfo converts ResourceSpec to interfaces.SpecInfo
func (sc *SpecsConfig) convertToSpecInfo(spec *ResourceSpec) *interfaces.SpecInfo {
	// Convert Platforms map to map[string]interface{}
	platforms := make(map[string]interface{})
	for platformName, platformConfig := range spec.Platforms {
		// Keep the full PlatformConfig struct instead of converting to map
		platforms[platformName] = platformConfig
	}

	return &interfaces.SpecInfo{
		Name:        spec.Name,
		DisplayName: spec.DisplayName,
		Category:    spec.Category,
		Resources: interfaces.ResourceRequirements{
			GPU:              spec.Resources.GPU,
			GPUType:          spec.Resources.GpuType,
			CPU:              spec.Resources.CPU,
			Memory:           spec.Resources.Memory,
			EphemeralStorage: spec.Resources.EphemeralStorage,
			ShmSize:          spec.Resources.ShmSize,
		},
		Platforms: platforms,
	}
}
