package endpoint

import (
	"context"
	"fmt"
	"time"

	"waverless/pkg/interfaces"
	"waverless/pkg/store/mysql"
)

// Service coordinates endpoint metadata, deployment, and scaling responsibilities.
type Service struct {
	metadata   *MetadataManager
	deployment *DeploymentManager
	scaler     *ScalerManager
}

// NewService wires all managers together into a single facade that handlers
// and other components can depend on.
func NewService(
	endpointRepo *mysql.EndpointRepository,
	autoscalerConfigRepo *mysql.AutoscalerConfigRepository,
	taskRepo *mysql.TaskRepository,
	workerLister workerLister,
	deploymentProvider interfaces.DeploymentProvider,
) *Service {
	metadata := NewMetadataManager(endpointRepo, autoscalerConfigRepo, taskRepo, workerLister)
	deployment := NewDeploymentManager(deploymentProvider, metadata, endpointRepo)
	scaler := NewScalerManager(deploymentProvider, endpointRepo, autoscalerConfigRepo)

	return &Service{
		metadata:   metadata,
		deployment: deployment,
		scaler:     scaler,
	}
}

// SaveEndpoint persists endpoint metadata and autoscaler configuration.
func (s *Service) SaveEndpoint(ctx context.Context, endpoint *interfaces.EndpointMetadata) error {
	if s.metadata == nil {
		return fmt.Errorf("metadata manager not configured")
	}
	return s.metadata.Save(ctx, endpoint)
}

// GetEndpoint fetches metadata information and merges autoscaler config.
func (s *Service) GetEndpoint(ctx context.Context, name string) (*interfaces.EndpointMetadata, error) {
	if s.metadata == nil {
		return nil, fmt.Errorf("metadata manager not configured")
	}
	return s.metadata.Get(ctx, name)
}

// GetEndpointOnly fetches metadata information without merging autoscaler config.
func (s *Service) GetEndpointOnly(ctx context.Context, name string) (*mysql.Endpoint, error) {
	if s.metadata == nil {
		return nil, fmt.Errorf("metadata manager not configured")
	}
	return s.metadata.GetEndpoint(ctx, name)
}

// ListEndpoints lists all endpoints, combining metadata and autoscaler config.
func (s *Service) ListEndpoints(ctx context.Context) ([]*interfaces.EndpointMetadata, error) {
	if s.metadata == nil {
		return nil, fmt.Errorf("metadata manager not configured")
	}
	return s.metadata.List(ctx)
}

// UpdateEndpoint updates endpoint metadata.
func (s *Service) UpdateEndpoint(ctx context.Context, endpoint *interfaces.EndpointMetadata) error {
	if endpoint == nil {
		return fmt.Errorf("endpoint metadata is nil")
	}
	endpoint.UpdatedAt = time.Now()
	return s.SaveEndpoint(ctx, endpoint)
}

// DeleteEndpoint removes endpoint metadata from the datastore.
func (s *Service) DeleteEndpoint(ctx context.Context, name string) error {
	if s.metadata == nil {
		return fmt.Errorf("metadata manager not configured")
	}
	return s.metadata.Delete(ctx, name)
}

// Deploy triggers a deployment through the provider and persists metadata.
func (s *Service) Deploy(ctx context.Context, req *interfaces.DeployRequest, metadata *interfaces.EndpointMetadata) (*interfaces.DeployResponse, error) {
	if s.deployment == nil {
		return nil, fmt.Errorf("deployment manager not configured")
	}
	return s.deployment.Deploy(ctx, req, metadata)
}

// UpdateDeployment updates deployment fields (image/spec/replicas).
func (s *Service) UpdateDeployment(ctx context.Context, req *interfaces.UpdateDeploymentRequest) (*interfaces.DeployResponse, error) {
	if s.deployment == nil {
		return nil, fmt.Errorf("deployment manager not configured")
	}
	return s.deployment.Update(ctx, req)
}

// DeleteDeployment removes runtime deployment resources and metadata.
func (s *Service) DeleteDeployment(ctx context.Context, name string) error {
	if s.deployment == nil {
		return fmt.Errorf("deployment manager not configured")
	}
	return s.deployment.Delete(ctx, name)
}

// ScaleUp increases replicas by the provided delta.
func (s *Service) ScaleUp(ctx context.Context, name string, delta int) error {
	if s.scaler == nil {
		return fmt.Errorf("scaler manager not configured")
	}
	return s.scaler.ScaleUp(ctx, name, delta)
}

// ScaleDown decreases replicas by the provided delta.
func (s *Service) ScaleDown(ctx context.Context, name string, delta int) error {
	if s.scaler == nil {
		return fmt.Errorf("scaler manager not configured")
	}
	return s.scaler.ScaleDown(ctx, name, delta)
}

// GetScalingStatus returns the current scaling status for an endpoint.
func (s *Service) GetScalingStatus(ctx context.Context, name string) (*ScalingStatus, error) {
	if s.scaler == nil {
		return nil, fmt.Errorf("scaler manager not configured")
	}
	return s.scaler.GetScalingStatus(ctx, name)
}
