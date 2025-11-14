package deploy

import (
	"fmt"

	"waverless/pkg/config"
	"waverless/pkg/deploy/k8s"
	"waverless/pkg/interfaces"
)

// CreateDeploymentProvider creates deployment provider
func CreateDeploymentProvider(cfg *config.Config, providerType string) (interfaces.DeploymentProvider, error) {
	switch providerType {
	case "k8s", "kubernetes", "":
		return k8s.NewK8sDeploymentProvider(cfg)
	default:
		return nil, fmt.Errorf("unsupported deployment provider type: %s", providerType)
	}
}
