package endpoint

import (
	"context"
	"fmt"
	"time"

	"waverless/pkg/interfaces"
	"waverless/pkg/store/mysql"
)

// ScalingStatus provides visibility into desired vs. actual replicas.
type ScalingStatus struct {
	Endpoint        string    `json:"endpoint"`
	DesiredReplicas int       `json:"desiredReplicas"`
	CurrentReplicas int       `json:"currentReplicas"`
	ReadyReplicas   int       `json:"readyReplicas"`
	Status          string    `json:"status"`
	LastScaleTime   time.Time `json:"lastScaleTime"`
}

// ErrEndpointBlockedDueToImageFailure is returned when an endpoint is blocked from scaling
// due to image-related failures.
var ErrEndpointBlockedDueToImageFailure = fmt.Errorf("endpoint is blocked due to image failure")

// ScalerManager coordinates scale operations with the deployment provider.
type ScalerManager struct {
	provider             interfaces.DeploymentProvider
	endpointRepo         *mysql.EndpointRepository
	autoscalerConfigRepo *mysql.AutoscalerConfigRepository
}

// NewScalerManager creates a new scaler manager.
func NewScalerManager(
	provider interfaces.DeploymentProvider,
	endpointRepo *mysql.EndpointRepository,
	autoscalerConfigRepo *mysql.AutoscalerConfigRepository,
) *ScalerManager {
	return &ScalerManager{
		provider:             provider,
		endpointRepo:         endpointRepo,
		autoscalerConfigRepo: autoscalerConfigRepo,
	}
}

// ScaleUp increases replicas by delta.
// Returns ErrEndpointBlockedDueToImageFailure if the endpoint is blocked due to image issues.
// Validates: Requirements 5.5
func (m *ScalerManager) ScaleUp(ctx context.Context, name string, delta int) error {
	if delta <= 0 {
		return fmt.Errorf("scale up delta must be positive")
	}

	// Check if endpoint is blocked due to image failure (Property 8: Failed Endpoint Prevents New Pods)
	if m.endpointRepo != nil {
		blocked, reason, err := m.endpointRepo.IsBlockedDueToImageFailure(ctx, name)
		if err != nil {
			// Log warning but continue (fail-open for availability)
			// In production, we prefer availability over strict enforcement
		} else if blocked {
			return fmt.Errorf("%w: %s - please update the image configuration", ErrEndpointBlockedDueToImageFailure, reason)
		}
	}

	return m.scaleTo(ctx, name, func(current int) int { return current + delta })
}

// ScaleDown decreases replicas by delta.
func (m *ScalerManager) ScaleDown(ctx context.Context, name string, delta int) error {
	if delta <= 0 {
		return fmt.Errorf("scale down delta must be positive")
	}
	return m.scaleTo(ctx, name, func(current int) int {
		target := current - delta
		if target < 0 {
			return 0
		}
		return target
	})
}

// GetScalingStatus reports desired vs. actual runtime replicas.
func (m *ScalerManager) GetScalingStatus(ctx context.Context, name string) (*ScalingStatus, error) {
	if m.endpointRepo == nil {
		return nil, fmt.Errorf("endpoint repository not configured")
	}

	ep, err := m.endpointRepo.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	if ep == nil {
		return nil, fmt.Errorf("endpoint not found: %s", name)
	}

	status := &ScalingStatus{
		Endpoint:        name,
		DesiredReplicas: ep.Replicas,
	}

	if m.provider != nil {
		if runtimeStatus, err := m.provider.GetAppStatus(ctx, name); err == nil && runtimeStatus != nil {
			status.Status = runtimeStatus.Status
			status.CurrentReplicas = int(runtimeStatus.TotalReplicas)
			status.ReadyReplicas = int(runtimeStatus.ReadyReplicas)
		}
	}

	if m.autoscalerConfigRepo != nil {
		if cfg, err := m.autoscalerConfigRepo.Get(ctx, name); err == nil && cfg != nil {
			status.LastScaleTime = cfg.UpdatedAt
		}
	}

	return status, nil
}

func (m *ScalerManager) scaleTo(ctx context.Context, name string, next func(int) int) error {
	if m.provider == nil {
		return fmt.Errorf("deployment provider not configured")
	}
	if m.endpointRepo == nil {
		return fmt.Errorf("endpoint repository not configured")
	}

	current, err := m.endpointRepo.Get(ctx, name)
	if err != nil {
		return err
	}
	if current == nil {
		return fmt.Errorf("endpoint not found: %s", name)
	}

	target := next(current.Replicas)
	if err := m.provider.ScaleApp(ctx, name, target); err != nil {
		return err
	}

	if err := m.endpointRepo.UpdateReplicas(ctx, name, target); err != nil {
		return err
	}

	if m.autoscalerConfigRepo != nil {
		_ = m.autoscalerConfigRepo.UpdateReplicas(ctx, name, target)
	}

	return nil
}
