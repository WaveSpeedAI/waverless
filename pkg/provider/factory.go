package provider

import (
	"fmt"
	"strings"
	"sync"

	"waverless/pkg/config"
	"waverless/pkg/interfaces"
	"waverless/pkg/provider/docker"
	"waverless/pkg/provider/gmi"
	"waverless/pkg/provider/k8s"
	"waverless/pkg/provider/novita"
)

// ProviderFactory provider factory
type ProviderFactory struct {
	cfg *config.Config
}

// NewProviderFactory creates provider factory
func NewProviderFactory(cfg *config.Config) *ProviderFactory {
	return &ProviderFactory{cfg: cfg}
}

// DeploymentFactory creates deployment provider
type DeploymentFactory func(cfg *config.Config) (interfaces.DeploymentProvider, error)

var (
	deploymentFactories   = map[string]DeploymentFactory{}
	deploymentFactoriesMu sync.RWMutex
)

// RegisterDeploymentProvider registers new deployment provider factory
// This function is thread-safe and can be called from multiple goroutines
//
// Example:
//   provider.RegisterDeploymentProvider("custom", func(cfg *config.Config) (interfaces.DeploymentProvider, error) {
//       return NewCustomProvider(cfg)
//   })
func RegisterDeploymentProvider(name string, factory DeploymentFactory) {
	if name == "" || factory == nil {
		return
	}

	deploymentFactoriesMu.Lock()
	defer deploymentFactoriesMu.Unlock()

	normalizedName := strings.ToLower(name)
	if _, exists := deploymentFactories[normalizedName]; exists {
		panic(fmt.Sprintf("deployment provider %s is already registered", name))
	}

	deploymentFactories[normalizedName] = factory
}

// ListDeploymentProviders returns all registered provider names
func ListDeploymentProviders() []string {
	deploymentFactoriesMu.RLock()
	defer deploymentFactoriesMu.RUnlock()

	names := make([]string, 0, len(deploymentFactories))
	for name := range deploymentFactories {
		names = append(names, name)
	}
	return names
}

// IsDeploymentProviderRegistered checks if a provider is registered
func IsDeploymentProviderRegistered(name string) bool {
	deploymentFactoriesMu.RLock()
	defer deploymentFactoriesMu.RUnlock()

	_, exists := deploymentFactories[strings.ToLower(name)]
	return exists
}

func init() {
	// Register built-in providers
	RegisterDeploymentProvider("k8s", k8s.NewK8sDeploymentProvider)
	RegisterDeploymentProvider("kubernetes", k8s.NewK8sDeploymentProvider)
	RegisterDeploymentProvider("docker", docker.NewDockerDeploymentProvider)
	RegisterDeploymentProvider("novita", novita.NewNovitaDeploymentProvider)
	RegisterDeploymentProvider("gmi", gmi.NewGMIDeploymentProvider)
}

func (f *ProviderFactory) CreateDeploymentProvider(providerType string) (interfaces.DeploymentProvider, error) {
	deploymentFactoriesMu.RLock()
	defer deploymentFactoriesMu.RUnlock()

	if len(deploymentFactories) == 0 {
		return nil, fmt.Errorf("no deployment providers registered")
	}

	factory, ok := deploymentFactories[strings.ToLower(providerType)]
	if !ok {
		return nil, fmt.Errorf("unsupported deployment provider type: %s (available: %v)", providerType, ListDeploymentProviders())
	}

	return factory(f.cfg)
}

// BusinessProviders business providers collection
type BusinessProviders struct {
	Deployment interfaces.DeploymentProvider
}

// CreateBusinessProviders creates business providers
func (f *ProviderFactory) CreateBusinessProviders() (*BusinessProviders, error) {
	deploymentType := "k8s"
	if f.cfg.Providers != nil && f.cfg.Providers.Deployment != "" {
		deploymentType = f.cfg.Providers.Deployment
	}

	deploymentProvider, err := f.CreateDeploymentProvider(deploymentType)
	if err != nil {
		return nil, fmt.Errorf("failed to create deployment provider: %w", err)
	}

	return &BusinessProviders{
		Deployment: deploymentProvider,
	}, nil
}
