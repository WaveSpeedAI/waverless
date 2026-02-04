package provider

import (
	"fmt"
	"strings"

	"waverless/pkg/config"
	"waverless/pkg/interfaces"
	"waverless/pkg/provider/docker"
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

// CreateDeploymentProvider creates deployment provider
type DeploymentFactory func(cfg *config.Config) (interfaces.DeploymentProvider, error)

var deploymentFactories = map[string]DeploymentFactory{}

// RegisterDeploymentProvider registers new deployment provider factory
func RegisterDeploymentProvider(name string, factory DeploymentFactory) {
	if name == "" || factory == nil {
		return
	}
	deploymentFactories[strings.ToLower(name)] = factory
}

func init() {
	RegisterDeploymentProvider("k8s", k8s.NewK8sDeploymentProvider)
	RegisterDeploymentProvider("kubernetes", k8s.NewK8sDeploymentProvider)
	RegisterDeploymentProvider("docker", docker.NewDockerDeploymentProvider)
	RegisterDeploymentProvider("novita", novita.NewNovitaDeploymentProvider)
}

func (f *ProviderFactory) CreateDeploymentProvider(providerType string) (interfaces.DeploymentProvider, error) {
	if len(deploymentFactories) == 0 {
		return nil, fmt.Errorf("no deployment providers registered")
	}

	factory, ok := deploymentFactories[strings.ToLower(providerType)]
	if !ok {
		return nil, fmt.Errorf("unsupported deployment provider type: %s", providerType)
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
