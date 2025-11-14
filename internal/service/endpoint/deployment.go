package endpoint

import (
	"context"
	"fmt"

	"waverless/pkg/interfaces"
)

// DeploymentManager wraps all runtime deployment operations.
type DeploymentManager struct {
	provider interfaces.DeploymentProvider
	metadata *MetadataManager
}

// NewDeploymentManager creates a deployment manager.
func NewDeploymentManager(provider interfaces.DeploymentProvider, metadata *MetadataManager) *DeploymentManager {
	return &DeploymentManager{
		provider: provider,
		metadata: metadata,
	}
}

// Deploy provisions runtime resources and persists metadata on success.
func (m *DeploymentManager) Deploy(ctx context.Context, req *interfaces.DeployRequest, metadata *interfaces.EndpointMetadata) (*interfaces.DeployResponse, error) {
	if m.provider == nil {
		return nil, fmt.Errorf("deployment provider not configured")
	}
	if req == nil {
		return nil, fmt.Errorf("deploy request is nil")
	}

	resp, err := m.provider.Deploy(ctx, req)
	if err != nil {
		return nil, err
	}

	if metadata != nil && m.metadata != nil {
		if metadata.Name == "" {
			metadata.Name = req.Endpoint
		}
		if metadata.SpecName == "" {
			metadata.SpecName = req.SpecName
		}
		if metadata.Image == "" {
			metadata.Image = req.Image
		}
		if metadata.Replicas == 0 {
			metadata.Replicas = req.Replicas
		}
		if metadata.TaskTimeout == 0 {
			metadata.TaskTimeout = req.TaskTimeout
		}
		if err := m.metadata.Save(ctx, metadata); err != nil {
			return resp, fmt.Errorf("deployment succeeded but failed to persist metadata: %w", err)
		}
	}

	return resp, nil
}

// Update orchestrates deployment updates and metadata synchronization.
func (m *DeploymentManager) Update(ctx context.Context, req *interfaces.UpdateDeploymentRequest) (*interfaces.DeployResponse, error) {
	if m.provider == nil {
		return nil, fmt.Errorf("deployment provider not configured")
	}
	if req == nil {
		return nil, fmt.Errorf("update request is nil")
	}

	resp, err := m.provider.UpdateDeployment(ctx, req)
	if err != nil {
		return nil, err
	}

	if m.metadata != nil {
		meta, err := m.metadata.Get(ctx, req.Endpoint)
		if err == nil && meta != nil {
			if req.SpecName != "" {
				meta.SpecName = req.SpecName
			}
			if req.Image != "" {
				meta.Image = req.Image
			}
			if req.Replicas != nil {
				meta.Replicas = *req.Replicas
			}
			if req.TaskTimeout != nil {
				meta.TaskTimeout = *req.TaskTimeout
			}
			if err := m.metadata.Save(ctx, meta); err != nil {
				return resp, fmt.Errorf("deployment updated but failed to persist metadata: %w", err)
			}
		}
	}

	return resp, nil
}

// Delete destroys runtime resources and metadata.
func (m *DeploymentManager) Delete(ctx context.Context, name string) error {
	if m.provider == nil {
		return fmt.Errorf("deployment provider not configured")
	}
	if err := m.provider.DeleteApp(ctx, name); err != nil {
		return err
	}
	if m.metadata != nil {
		if err := m.metadata.Delete(ctx, name); err != nil {
			return err
		}
	}
	return nil
}
