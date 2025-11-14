package docker

import (
	"context"
	"fmt"

	"waverless/pkg/config"
	"waverless/pkg/interfaces"
)

// DockerDeploymentProvider is a placeholder implementation for future Docker support.
type DockerDeploymentProvider struct{}

// NewDockerDeploymentProvider creates a stub Docker provider.
func NewDockerDeploymentProvider(cfg *config.Config) (interfaces.DeploymentProvider, error) {
	// For now we return a stub implementation that keeps interface compatibility.
	return &DockerDeploymentProvider{}, nil
}

func (p *DockerDeploymentProvider) unsupported(method string) error {
	return fmt.Errorf("docker deployment provider: %s not implemented yet", method)
}

func (p *DockerDeploymentProvider) Deploy(ctx context.Context, req *interfaces.DeployRequest) (*interfaces.DeployResponse, error) {
	return nil, p.unsupported("Deploy")
}

func (p *DockerDeploymentProvider) GetApp(ctx context.Context, endpoint string) (*interfaces.AppInfo, error) {
	return nil, p.unsupported("GetApp")
}

func (p *DockerDeploymentProvider) ListApps(ctx context.Context) ([]*interfaces.AppInfo, error) {
	return nil, p.unsupported("ListApps")
}

func (p *DockerDeploymentProvider) DeleteApp(ctx context.Context, endpoint string) error {
	return p.unsupported("DeleteApp")
}

func (p *DockerDeploymentProvider) GetAppLogs(ctx context.Context, endpoint string, lines int, podName ...string) (string, error) {
	return "", p.unsupported("GetAppLogs")
}

func (p *DockerDeploymentProvider) ScaleApp(ctx context.Context, endpoint string, replicas int) error {
	return p.unsupported("ScaleApp")
}

func (p *DockerDeploymentProvider) GetAppStatus(ctx context.Context, endpoint string) (*interfaces.AppStatus, error) {
	return nil, p.unsupported("GetAppStatus")
}

func (p *DockerDeploymentProvider) ListSpecs(ctx context.Context) ([]*interfaces.SpecInfo, error) {
	return nil, p.unsupported("ListSpecs")
}

func (p *DockerDeploymentProvider) GetSpec(ctx context.Context, specName string) (*interfaces.SpecInfo, error) {
	return nil, p.unsupported("GetSpec")
}

func (p *DockerDeploymentProvider) PreviewDeploymentYAML(ctx context.Context, req *interfaces.DeployRequest) (string, error) {
	return "", p.unsupported("PreviewDeploymentYAML")
}

func (p *DockerDeploymentProvider) UpdateDeployment(ctx context.Context, req *interfaces.UpdateDeploymentRequest) (*interfaces.DeployResponse, error) {
	return nil, p.unsupported("UpdateDeployment")
}

func (p *DockerDeploymentProvider) WatchReplicas(ctx context.Context, callback interfaces.ReplicaCallback) error {
	return p.unsupported("WatchReplicas")
}

func (p *DockerDeploymentProvider) GetPods(ctx context.Context, endpoint string) ([]*interfaces.PodInfo, error) {
	return nil, p.unsupported("GetPods")
}

func (p *DockerDeploymentProvider) DescribePod(ctx context.Context, endpoint string, podName string) (*interfaces.PodDetail, error) {
	return nil, p.unsupported("DescribePod")
}
