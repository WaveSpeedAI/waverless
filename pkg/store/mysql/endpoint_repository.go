package mysql

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// EndpointRepository handles endpoint persistence in MySQL
type EndpointRepository struct {
	ds *Datastore
}

// NewEndpointRepository creates a new endpoint repository
func NewEndpointRepository(ds *Datastore) *EndpointRepository {
	return &EndpointRepository{ds: ds}
}

// Create creates a new endpoint
func (r *EndpointRepository) Create(ctx context.Context, endpoint *Endpoint) error {
	return r.ds.DB(ctx).Create(endpoint).Error
}

// Get retrieves an endpoint by name
func (r *EndpointRepository) Get(ctx context.Context, endpointName string) (*Endpoint, error) {
	var endpoint Endpoint
	err := r.ds.DB(ctx).Where("endpoint = ?", endpointName).First(&endpoint).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get endpoint: %w", err)
	}
	return &endpoint, nil
}

// Update updates an endpoint
func (r *EndpointRepository) Update(ctx context.Context, endpoint *Endpoint) error {
	return r.ds.DB(ctx).Save(endpoint).Error
}

// Delete soft deletes an endpoint by setting status to 'deleted'
func (r *EndpointRepository) Delete(ctx context.Context, endpointName string) error {
	return r.ds.DB(ctx).Model(&Endpoint{}).
		Where("endpoint = ?", endpointName).
		Update("status", "deleted").Error
}

// HardDelete physically deletes an endpoint from database
func (r *EndpointRepository) HardDelete(ctx context.Context, endpointName string) error {
	return r.ds.DB(ctx).Where("endpoint = ?", endpointName).Delete(&Endpoint{}).Error
}

// List retrieves all endpoints except those explicitly deleted
func (r *EndpointRepository) List(ctx context.Context) ([]*Endpoint, error) {
	var endpoints []*Endpoint
	err := r.ds.DB(ctx).
		Where("status != ?", "deleted").
		Find(&endpoints).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list endpoints: %w", err)
	}
	return endpoints, nil
}

// ListAll retrieves all endpoints including deleted ones
func (r *EndpointRepository) ListAll(ctx context.Context) ([]*Endpoint, error) {
	var endpoints []*Endpoint
	err := r.ds.DB(ctx).Find(&endpoints).Error
	if err != nil {
		return nil, fmt.Errorf("failed to list all endpoints: %w", err)
	}
	return endpoints, nil
}

// UpdateReplicas updates the replica count for an endpoint
func (r *EndpointRepository) UpdateReplicas(ctx context.Context, endpointName string, replicas int) error {
	return r.ds.DB(ctx).Model(&Endpoint{}).
		Where("endpoint = ?", endpointName).
		Update("replicas", replicas).Error
}

// UpdateImage updates the image for an endpoint
func (r *EndpointRepository) UpdateImage(ctx context.Context, endpointName string, image string) error {
	return r.ds.DB(ctx).Model(&Endpoint{}).
		Where("endpoint = ?", endpointName).
		Update("image", image).Error
}

// Exists checks if an endpoint exists
func (r *EndpointRepository) Exists(ctx context.Context, endpointName string) (bool, error) {
	var count int64
	err := r.ds.DB(ctx).Model(&Endpoint{}).
		Where("endpoint = ? AND status != ?", endpointName, "deleted").
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("failed to check endpoint existence: %w", err)
	}
	return count > 0, nil
}

// UpdateStatus updates endpoint status
func (r *EndpointRepository) UpdateStatus(ctx context.Context, endpointName string, status string) error {
	return r.ds.DB(ctx).Model(&Endpoint{}).
		Where("endpoint = ?", endpointName).
		Updates(map[string]interface{}{
			"status":     status,
			"updated_at": gorm.Expr("CURRENT_TIMESTAMP(3)"),
		}).Error
}

// GetBySpecName queries endpoints by Spec name
func (r *EndpointRepository) GetBySpecName(ctx context.Context, specName string) ([]*Endpoint, error) {
	var endpoints []*Endpoint
	err := r.ds.DB(ctx).
		Where("spec_name = ? AND status != ?", specName, "deleted").
		Find(&endpoints).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get endpoints by spec: %w", err)
	}
	return endpoints, nil
}
