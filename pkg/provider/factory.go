package provider

import (
	"fmt"
	"strings"

	"waverless/pkg/config"
	"waverless/pkg/deploy/docker"
	"waverless/pkg/deploy/k8s"
	"waverless/pkg/interfaces"
	queueredis "waverless/pkg/queue/redis"
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
// providerType: k8s, docker, custom, etc.
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

// CreateQueueProvider creates queue provider
// providerType: redis, mysql, postgres, etc.
func (f *ProviderFactory) CreateQueueProvider(providerType string) (interfaces.QueueProvider, error) {
	switch providerType {
	case "redis":
		return queueredis.NewRedisQueueProvider(f.cfg)
	default:
		return nil, fmt.Errorf("unsupported queue provider type: %s", providerType)
	}
}

// BusinessProviders business providers collection (excluding database)
type BusinessProviders struct {
	Deployment interfaces.DeploymentProvider
	Queue      interfaces.QueueProvider
}

// CreateBusinessProviders creates business providers (excluding database)
// Database initialization should be completed in main.go core location
func (f *ProviderFactory) CreateBusinessProviders() (*BusinessProviders, error) {
	// Read provider types from configuration, use defaults if not configured
	deploymentType := "k8s"
	if f.cfg.Providers != nil && f.cfg.Providers.Deployment != "" {
		deploymentType = f.cfg.Providers.Deployment
	}

	queueType := "redis"
	if f.cfg.Providers != nil && f.cfg.Providers.Queue != "" {
		queueType = f.cfg.Providers.Queue
	}

	// creates deployment provider
	deploymentProvider, err := f.CreateDeploymentProvider(deploymentType)
	if err != nil {
		return nil, fmt.Errorf("failed to create deployment provider: %w", err)
	}

	// creates queue provider
	queueProvider, err := f.CreateQueueProvider(queueType)
	if err != nil {
		return nil, fmt.Errorf("failed to create queue provider: %w", err)
	}

	return &BusinessProviders{
		Deployment: deploymentProvider,
		Queue:      queueProvider,
	}, nil
}
