package mysql

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// AutoscalerConfigRepository handles autoscaler configuration persistence in MySQL
type AutoscalerConfigRepository struct {
	ds *Datastore
}

// NewAutoscalerConfigRepository creates a new autoscaler config repository
func NewAutoscalerConfigRepository(ds *Datastore) *AutoscalerConfigRepository {
	return &AutoscalerConfigRepository{ds: ds}
}

// Create creates a new autoscaler configuration
func (r *AutoscalerConfigRepository) Create(ctx context.Context, config *AutoscalerConfig) error {
	return r.ds.DB(ctx).Create(config).Error
}

// Get retrieves autoscaler configuration by endpoint
func (r *AutoscalerConfigRepository) Get(ctx context.Context, endpoint string) (*AutoscalerConfig, error) {
	var config AutoscalerConfig
	err := r.ds.DB(ctx).Where("endpoint = ?", endpoint).First(&config).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get autoscaler config: %w", err)
	}
	return &config, nil
}

// Update updates autoscaler configuration
func (r *AutoscalerConfigRepository) Update(ctx context.Context, config *AutoscalerConfig) error {
	return r.ds.DB(ctx).Save(config).Error
}

// Delete deletes autoscaler configuration
func (r *AutoscalerConfigRepository) Delete(ctx context.Context, endpoint string) error {
	return r.ds.DB(ctx).Where("endpoint = ?", endpoint).Delete(&AutoscalerConfig{}).Error
}

// List retrieves all autoscaler configurations
func (r *AutoscalerConfigRepository) List(ctx context.Context) ([]*AutoscalerConfig, error) {
	var configs []*AutoscalerConfig
	err := r.ds.DB(ctx).Find(&configs).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list autoscaler configs: %w", err)
	}
	return configs, nil
}

// ListEnabled retrieves all enabled autoscaler configurations
func (r *AutoscalerConfigRepository) ListEnabled(ctx context.Context) ([]*AutoscalerConfig, error) {
	var configs []*AutoscalerConfig
	err := r.ds.DB(ctx).Where("enabled = ?", true).Find(&configs).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list enabled autoscaler configs: %w", err)
	}
	return configs, nil
}

// UpdateReplicas updates the target replica count for an endpoint
func (r *AutoscalerConfigRepository) UpdateReplicas(ctx context.Context, endpoint string, replicas int) error {
	return r.ds.DB(ctx).Model(&AutoscalerConfig{}).
		Where("endpoint = ?", endpoint).
		Update("replicas", replicas).Error
}

// Enable enables autoscaling for an endpoint
func (r *AutoscalerConfigRepository) Enable(ctx context.Context, endpoint string) error {
	return r.ds.DB(ctx).Model(&AutoscalerConfig{}).
		Where("endpoint = ?", endpoint).
		Update("enabled", true).Error
}

// Disable disables autoscaling for an endpoint
func (r *AutoscalerConfigRepository) Disable(ctx context.Context, endpoint string) error {
	return r.ds.DB(ctx).Model(&AutoscalerConfig{}).
		Where("endpoint = ?", endpoint).
		Update("enabled", false).Error
}

// Exists checks if autoscaler configuration exists for an endpoint
func (r *AutoscalerConfigRepository) Exists(ctx context.Context, endpoint string) (bool, error) {
	var count int64
	err := r.ds.DB(ctx).Model(&AutoscalerConfig{}).
		Where("endpoint = ?", endpoint).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("failed to check autoscaler config existence: %w", err)
	}
	return count > 0, nil
}

// CreateOrUpdate creates or updates autoscaler configuration
func (r *AutoscalerConfigRepository) CreateOrUpdate(ctx context.Context, config *AutoscalerConfig) error {
	// Check if exists
	existing, err := r.Get(ctx, config.Endpoint)
	if err != nil {
		return err
	}

	if existing == nil {
		// Create new
		return r.Create(ctx, config)
	}

	// Update existing
	config.ID = existing.ID
	config.CreatedAt = existing.CreatedAt
	return r.Update(ctx, config)
}

// BatchUpdateReplicas updates replicas for multiple endpoints in a transaction
func (r *AutoscalerConfigRepository) BatchUpdateReplicas(ctx context.Context, updates map[string]int) error {
	if len(updates) == 0 {
		return nil
	}

	return r.ds.ExecTx(ctx, func(txCtx context.Context) error {
		for endpoint, replicas := range updates {
			err := r.ds.DB(txCtx).Model(&AutoscalerConfig{}).
				Where("endpoint = ?", endpoint).
				Update("replicas", replicas).Error
			if err != nil {
				return fmt.Errorf("failed to update replicas for endpoint %s: %w", endpoint, err)
			}
		}
		return nil
	})
}

// GetByMinReplicas retrieves configs with min_replicas greater than or equal to specified value
func (r *AutoscalerConfigRepository) GetByMinReplicas(ctx context.Context, minReplicas int) ([]*AutoscalerConfig, error) {
	var configs []*AutoscalerConfig
	err := r.ds.DB(ctx).Where("min_replicas >= ?", minReplicas).Find(&configs).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get configs by min replicas: %w", err)
	}
	return configs, nil
}

// UpdatePriority updates the base priority for an endpoint
func (r *AutoscalerConfigRepository) UpdatePriority(ctx context.Context, endpoint string, priority int) error {
	return r.ds.DB(ctx).Model(&AutoscalerConfig{}).
		Where("endpoint = ?", endpoint).
		Update("priority", priority).Error
}
