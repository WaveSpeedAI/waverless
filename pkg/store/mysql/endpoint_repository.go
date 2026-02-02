package mysql

import (
	"context"
	"fmt"
	"time"

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

// UpdateRuntimeState updates endpoint status and runtime state from K8s (merges with existing)
func (r *EndpointRepository) UpdateRuntimeState(ctx context.Context, endpointName, status string, runtimeState map[string]interface{}) error {
	// First get existing runtime_state to merge
	var endpoint Endpoint
	if err := r.ds.DB(ctx).Where("endpoint = ?", endpointName).First(&endpoint).Error; err == nil {
		// Merge: existing values are preserved if not in new runtimeState
		if endpoint.RuntimeState != nil {
			for k, v := range endpoint.RuntimeState {
				if _, exists := runtimeState[k]; !exists {
					runtimeState[k] = v
				}
			}
		}
	}

	return r.ds.DB(ctx).Model(&Endpoint{}).
		Where("endpoint = ?", endpointName).
		Updates(map[string]interface{}{
			"status":        status,
			"runtime_state": JSONMap(runtimeState),
			"updated_at":    gorm.Expr("CURRENT_TIMESTAMP(3)"),
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

// UpdateHealthStatus updates the health status of an endpoint.
// This method is called by the ResourceReleaser to update endpoint health
// based on worker failures.
//
// Parameters:
//   - ctx: Context for database operations
//   - endpointName: The name of the endpoint to update
//   - healthStatus: The new health status (HEALTHY, DEGRADED, UNHEALTHY)
//   - healthMessage: Optional message describing the health status
//
// Returns:
//   - error if the database update fails
//
// Validates: Requirements 5.4, 6.3, 6.4
func (r *EndpointRepository) UpdateHealthStatus(ctx context.Context, endpointName, healthStatus, healthMessage string) error {
	now := time.Now()
	updates := map[string]interface{}{
		"health_status":        healthStatus,
		"last_health_check_at": now,
		"updated_at":           gorm.Expr("CURRENT_TIMESTAMP(3)"),
	}

	// Only set health_message if it's not empty, otherwise set to NULL
	if healthMessage != "" {
		updates["health_message"] = healthMessage
	} else {
		updates["health_message"] = nil
	}

	return r.ds.DB(ctx).Model(&Endpoint{}).
		Where("endpoint = ?", endpointName).
		Updates(updates).Error
}

// GetByHealthStatus retrieves all endpoints with a specific health status.
//
// Parameters:
//   - ctx: Context for database operations
//   - healthStatus: The health status to filter by (HEALTHY, DEGRADED, UNHEALTHY)
//
// Returns:
//   - List of endpoints with the specified health status
//   - error if the database query fails
func (r *EndpointRepository) GetByHealthStatus(ctx context.Context, healthStatus string) ([]*Endpoint, error) {
	var endpoints []*Endpoint
	err := r.ds.DB(ctx).
		Where("health_status = ? AND status != ?", healthStatus, "deleted").
		Find(&endpoints).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get endpoints by health status: %w", err)
	}
	return endpoints, nil
}

// IsBlockedDueToImageFailure checks if an endpoint is blocked from creating new workers
// due to image-related failures. An endpoint is blocked when:
// - health_status is UNHEALTHY
// - health_message indicates image-related issues
//
// This implements Property 8: Failed Endpoint Prevents New Pods from the design document.
//
// Parameters:
//   - ctx: Context for database operations
//   - endpointName: The name of the endpoint to check
//
// Returns:
//   - blocked: true if the endpoint is blocked from creating new workers
//   - reason: the reason for blocking (empty if not blocked)
//   - error: if the database query fails
//
// Validates: Requirements 5.5
func (r *EndpointRepository) IsBlockedDueToImageFailure(ctx context.Context, endpointName string) (blocked bool, reason string, err error) {
	endpoint, err := r.Get(ctx, endpointName)
	if err != nil {
		return false, "", fmt.Errorf("failed to get endpoint: %w", err)
	}
	if endpoint == nil {
		return false, "", nil // Endpoint doesn't exist, not blocked
	}

	// Check if endpoint is UNHEALTHY due to image issues
	if endpoint.HealthStatus == "UNHEALTHY" {
		// Check if the health message indicates image-related issues
		if endpoint.HealthMessage != nil && isImageRelatedFailure(*endpoint.HealthMessage) {
			return true, *endpoint.HealthMessage, nil
		}
	}

	return false, "", nil
}

// isImageRelatedFailure checks if the health message indicates an image-related failure.
// This is used to determine if new Pod creation should be blocked.
func isImageRelatedFailure(healthMessage string) bool {
	// Common image-related failure messages
	imageRelatedKeywords := []string{
		"image",                 // English
		"Image",                 // English (capitalized)
		"IMAGE",                 // English (uppercase)
		"Worker startup failed", // Worker startup failed
	}

	for _, keyword := range imageRelatedKeywords {
		if contains(healthMessage, keyword) {
			return true
		}
	}
	return false
}

// contains checks if s contains substr (simple string contains check)
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

// findSubstring checks if substr exists in s
func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
