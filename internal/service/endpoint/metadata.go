package endpoint

import (
	"context"
	"fmt"
	"time"

	"waverless/internal/model"
	"waverless/pkg/interfaces"
	"waverless/pkg/store/mysql"

	"gorm.io/gorm"
)

// MetadataManager encapsulates all metadata + autoscaler persistence logic.
type MetadataManager struct {
	endpointRepo         endpointRepository
	autoscalerConfigRepo autoscalerConfigRepository
	taskRepo             taskRepository
	workerRepo           workerRepository
}

// NewMetadataManager creates a new metadata manager.
func NewMetadataManager(
	endpointRepo endpointRepository,
	autoscalerConfigRepo autoscalerConfigRepository,
	taskRepo taskRepository,
	workerRepo workerRepository,
) *MetadataManager {
	return &MetadataManager{
		endpointRepo:         endpointRepo,
		autoscalerConfigRepo: autoscalerConfigRepo,
		taskRepo:             taskRepo,
		workerRepo:           workerRepo,
	}
}

// Save persists endpoint metadata and optional autoscaler configuration.
func (m *MetadataManager) Save(ctx context.Context, endpoint *interfaces.EndpointMetadata) error {
	if endpoint == nil {
		return fmt.Errorf("endpoint metadata is nil")
	}
	if m.endpointRepo == nil {
		return fmt.Errorf("endpoint repository not configured")
	}

	ensureMetadataDefaults(endpoint)

	now := time.Now()
	if endpoint.CreatedAt.IsZero() {
		endpoint.CreatedAt = now
	}
	endpoint.UpdatedAt = now

	mysqlEndpoint := toMySQLEndpoint(endpoint)

	existing, err := m.endpointRepo.Get(ctx, endpoint.Name)
	if err != nil {
		return err
	}

	if existing == nil {
		if err := m.endpointRepo.Create(ctx, mysqlEndpoint); err != nil {
			return fmt.Errorf("failed to create endpoint: %w", err)
		}
	} else {
		mysqlEndpoint.ID = existing.ID
		mysqlEndpoint.CreatedAt = existing.CreatedAt
		if err := m.endpointRepo.Update(ctx, mysqlEndpoint); err != nil {
			return fmt.Errorf("failed to update endpoint: %w", err)
		}
	}

	// Always save autoscaler config if repo is available
	// This ensures AutoscalerEnabled and other fields are persisted
	// even when MaxReplicas is 0 (which disables autoscaling but should still be saved)
	if m.autoscalerConfigRepo != nil {
		if err := m.saveAutoscalerConfig(ctx, endpoint); err != nil {
			return err
		}
	}

	return nil
}

// Get fetches endpoint metadata merged with autoscaler configuration.
func (m *MetadataManager) Get(ctx context.Context, name string) (*interfaces.EndpointMetadata, error) {
	if m.endpointRepo == nil {
		return nil, fmt.Errorf("endpoint repository not configured")
	}

	mysqlEndpoint, err := m.endpointRepo.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	if mysqlEndpoint == nil {
		return nil, nil
	}

	meta := fromMySQLEndpoint(mysqlEndpoint)

	if m.autoscalerConfigRepo != nil {
		cfg, err := m.autoscalerConfigRepo.Get(ctx, name)
		if err != nil && err != gorm.ErrRecordNotFound {
			return nil, err
		}
		if cfg != nil {
			mergeAutoscalerConfig(meta, cfg)
		}
	}

	return meta, nil
}

// List returns all stored endpoints.
func (m *MetadataManager) List(ctx context.Context) ([]*interfaces.EndpointMetadata, error) {
	if m.endpointRepo == nil {
		return nil, fmt.Errorf("endpoint repository not configured")
	}

	mysqlEndpoints, err := m.endpointRepo.List(ctx)
	if err != nil {
		return nil, err
	}

	results := make([]*interfaces.EndpointMetadata, 0, len(mysqlEndpoints))
	for _, item := range mysqlEndpoints {
		meta := fromMySQLEndpoint(item)

		if m.autoscalerConfigRepo != nil {
			cfg, err := m.autoscalerConfigRepo.Get(ctx, item.Endpoint)
			if err == nil && cfg != nil {
				mergeAutoscalerConfig(meta, cfg)
			}
		}

		results = append(results, meta)
	}

	return results, nil
}

// Delete performs a soft delete on the endpoint metadata.
func (m *MetadataManager) Delete(ctx context.Context, name string) error {
	if m.endpointRepo == nil {
		return fmt.Errorf("endpoint repository not configured")
	}
	return m.endpointRepo.Delete(ctx, name)
}

// GetStats aggregates worker/task metrics for the provided endpoint.
func (m *MetadataManager) GetStats(ctx context.Context, name string) (*interfaces.EndpointStats, error) {
	if m.taskRepo == nil || m.workerRepo == nil {
		return nil, fmt.Errorf("stats repositories not configured")
	}

	stats := &interfaces.EndpointStats{
		Endpoint: name,
		From:     time.Now().Add(-1 * time.Hour),
		To:       time.Now(),
	}

	if count, err := m.taskRepo.CountByEndpointAndStatus(ctx, name, string(model.TaskStatusPending)); err == nil {
		stats.PendingTasks = int(count)
	}
	if count, err := m.taskRepo.CountByEndpointAndStatus(ctx, name, string(model.TaskStatusInProgress)); err == nil {
		stats.RunningTasks = int(count)
	}
	if count, err := m.taskRepo.CountByEndpointAndStatus(ctx, name, string(model.TaskStatusCompleted)); err == nil {
		stats.CompletedTasks = int(count)
	}
	if count, err := m.taskRepo.CountByEndpointAndStatus(ctx, name, string(model.TaskStatusFailed)); err == nil {
		stats.FailedTasks = int(count)
	}

	workers, err := m.workerRepo.GetAll(ctx)
	if err == nil {
		for _, worker := range workers {
			if worker.Endpoint != name {
				continue
			}
			stats.TotalWorkers++
			switch worker.Status {
			case model.WorkerStatusBusy:
				stats.BusyWorkers++
			case model.WorkerStatusOnline:
				stats.OnlineWorkers++
			case model.WorkerStatusDraining:
				// DRAINING workers are still counted as total but not online/busy
				// They are finishing existing tasks but won't receive new ones
				// This prevents autoscaler from counting them as available capacity
			default:
				// Legacy string-based status support
				if worker.Status == "online" {
					stats.OnlineWorkers++
				}
				if worker.Status == "busy" {
					stats.BusyWorkers++
				}
			}
		}
	}

	return stats, nil
}

func (m *MetadataManager) saveAutoscalerConfig(ctx context.Context, meta *interfaces.EndpointMetadata) error {
	if meta == nil {
		return fmt.Errorf("endpoint metadata is nil")
	}

	enableDynamic := true
	if meta.EnableDynamicPrio != nil {
		enableDynamic = *meta.EnableDynamicPrio
	}

	// Note: We only use defaultIfZero for fields where 0 is NOT a valid value
	// For fields that allow 0 (like Priority=0 for best-effort, Cooldown=0 for no cooldown),
	// we directly use the metadata value to preserve user's explicit 0 settings
	config := &mysql.AutoscalerConfig{
		Endpoint:          meta.Name,
		DisplayName:       meta.DisplayName,
		SpecName:          meta.SpecName,
		MinReplicas:       meta.MinReplicas,       // 0 is valid (scale-to-zero)
		MaxReplicas:       meta.MaxReplicas,       // Direct value (0 means no autoscaling)
		Replicas:          meta.Replicas,          // Direct value
		ScaleUpThreshold:  defaultIfZero(meta.ScaleUpThreshold, 1),   // 0 behaves like 1 anyway
		ScaleDownIdleTime: defaultIfZero(meta.ScaleDownIdleTime, 300), // Use default if not set
		ScaleUpCooldown:   meta.ScaleUpCooldown,   // 0 is valid (no cooldown)
		ScaleDownCooldown: meta.ScaleDownCooldown, // 0 is valid (no cooldown)
		Priority:          meta.Priority,          // 0 is valid (lowest priority)
		EnableDynamicPrio: enableDynamic,
		HighLoadThreshold: defaultIfZero(meta.HighLoadThreshold, 10), // Use default if not set
		PriorityBoost:     meta.PriorityBoost,     // 0 is valid (no boost)
		Enabled:           true,
		AutoscalerEnabled: meta.AutoscalerEnabled,
	}

	// CRITICAL: Copy time tracking fields (for autoscaler decisions)
	if !meta.LastTaskTime.IsZero() {
		config.LastTaskTime = &meta.LastTaskTime
	}
	if !meta.LastScaleTime.IsZero() {
		config.LastScaleTime = &meta.LastScaleTime
	}
	if !meta.FirstPendingTime.IsZero() {
		config.FirstPendingTime = &meta.FirstPendingTime
	}

	if err := m.autoscalerConfigRepo.CreateOrUpdate(ctx, config); err != nil {
		return fmt.Errorf("failed to save autoscaler config: %w", err)
	}

	return nil
}

func toMySQLEndpoint(endpoint *interfaces.EndpointMetadata) *mysql.Endpoint {
	return &mysql.Endpoint{
		Endpoint:    endpoint.Name,
		SpecName:    endpoint.SpecName,
		Image:       endpoint.Image,
		Replicas:    endpoint.Replicas,
		TaskTimeout: endpoint.TaskTimeout,
		Env:         mysql.StringMapToJSONMap(endpoint.Env),
		Labels:      mysql.StringMapToJSONMap(endpoint.Labels),
		Status:      endpoint.Status,
		CreatedAt:   endpoint.CreatedAt,
		UpdatedAt:   endpoint.UpdatedAt,
	}
}

func fromMySQLEndpoint(endpoint *mysql.Endpoint) *interfaces.EndpointMetadata {
	return &interfaces.EndpointMetadata{
		Name:        endpoint.Endpoint,
		SpecName:    endpoint.SpecName,
		Image:       endpoint.Image,
		Replicas:    endpoint.Replicas,
		TaskTimeout: endpoint.TaskTimeout,
		Env:         mysql.JSONMapToStringMap(endpoint.Env),
		Labels:      mysql.JSONMapToStringMap(endpoint.Labels),
		Status:      endpoint.Status,
		CreatedAt:   endpoint.CreatedAt,
		UpdatedAt:   endpoint.UpdatedAt,
	}
}

func mergeAutoscalerConfig(meta *interfaces.EndpointMetadata, cfg *mysql.AutoscalerConfig) {
	meta.DisplayName = cfg.DisplayName
	if meta.SpecName == "" {
		meta.SpecName = cfg.SpecName
	}
	if meta.Replicas == 0 {
		meta.Replicas = cfg.Replicas
	}
	meta.MinReplicas = cfg.MinReplicas
	meta.MaxReplicas = cfg.MaxReplicas
	meta.ScaleUpThreshold = cfg.ScaleUpThreshold
	meta.ScaleDownIdleTime = cfg.ScaleDownIdleTime
	meta.ScaleUpCooldown = cfg.ScaleUpCooldown
	meta.ScaleDownCooldown = cfg.ScaleDownCooldown
	meta.Priority = cfg.Priority
	meta.HighLoadThreshold = cfg.HighLoadThreshold
	meta.PriorityBoost = cfg.PriorityBoost
	meta.EnableDynamicPrio = &cfg.EnableDynamicPrio
	meta.AutoscalerEnabled = cfg.AutoscalerEnabled

	// CRITICAL: Copy time tracking fields (for autoscaler decisions)
	if cfg.LastTaskTime != nil {
		meta.LastTaskTime = *cfg.LastTaskTime
	}
	if cfg.LastScaleTime != nil {
		meta.LastScaleTime = *cfg.LastScaleTime
	}
	if cfg.FirstPendingTime != nil {
		meta.FirstPendingTime = *cfg.FirstPendingTime
	}
}

func defaultIfZero(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}

// helper to ensure metadata defaults are applied before persistence when service is used directly
func ensureMetadataDefaults(meta *interfaces.EndpointMetadata) {
	if meta.Status == "" {
		meta.Status = "Deploying"
	}
	if meta.DisplayName == "" {
		meta.DisplayName = meta.Name
	}
	if meta.EnableDynamicPrio == nil {
		defaultVal := true
		meta.EnableDynamicPrio = &defaultVal
	}
}
