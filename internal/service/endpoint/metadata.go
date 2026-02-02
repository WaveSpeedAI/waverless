package endpoint

import (
	"context"
	"fmt"
	"time"

	"waverless/pkg/interfaces"
	"waverless/pkg/store/mysql"

	"gorm.io/gorm"
)

// MetadataManager encapsulates all metadata + autoscaler persistence logic.
type MetadataManager struct {
	endpointRepo         endpointRepository
	autoscalerConfigRepo autoscalerConfigRepository
	taskRepo             taskRepository
	workerLister         workerLister
}

// NewMetadataManager creates a new metadata manager.
func NewMetadataManager(
	endpointRepo endpointRepository,
	autoscalerConfigRepo autoscalerConfigRepository,
	taskRepo taskRepository,
	workerLister workerLister,
) *MetadataManager {
	return &MetadataManager{
		endpointRepo:         endpointRepo,
		autoscalerConfigRepo: autoscalerConfigRepo,
		taskRepo:             taskRepo,
		workerLister:         workerLister,
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
		// Update existing record - only modify fields from mysqlEndpoint, preserve others
		existing.SpecName = mysqlEndpoint.SpecName
		existing.Description = mysqlEndpoint.Description
		existing.Image = mysqlEndpoint.Image
		existing.ImagePrefix = mysqlEndpoint.ImagePrefix
		existing.ImageDigest = mysqlEndpoint.ImageDigest
		existing.ImageLastChecked = mysqlEndpoint.ImageLastChecked
		existing.LatestImage = mysqlEndpoint.LatestImage
		existing.Replicas = mysqlEndpoint.Replicas
		existing.GpuCount = mysqlEndpoint.GpuCount
		existing.TaskTimeout = mysqlEndpoint.TaskTimeout
		existing.EnablePtrace = mysqlEndpoint.EnablePtrace
		existing.Env = mysqlEndpoint.Env
		existing.Labels = mysqlEndpoint.Labels
		existing.Status = mysqlEndpoint.Status
		existing.UpdatedAt = mysqlEndpoint.UpdatedAt
		if err := m.endpointRepo.Update(ctx, existing); err != nil {
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

func (m *MetadataManager) GetEndpoint(ctx context.Context, name string) (*mysql.Endpoint, error) {
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
	if mysqlEndpoint.Status == "deleted" {
		return nil, nil
	}
	return mysqlEndpoint, nil
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

	// Batch load autoscaler configs for these endpoints only
	configMap := make(map[string]*mysql.AutoscalerConfig)
	if m.autoscalerConfigRepo != nil && len(mysqlEndpoints) > 0 {
		endpoints := make([]string, len(mysqlEndpoints))
		for i, ep := range mysqlEndpoints {
			endpoints[i] = ep.Endpoint
		}
		configs, err := m.autoscalerConfigRepo.ListByEndpoints(ctx, endpoints)
		if err == nil {
			for _, cfg := range configs {
				configMap[cfg.Endpoint] = cfg
			}
		}
	}

	results := make([]*interfaces.EndpointMetadata, 0, len(mysqlEndpoints))
	for _, item := range mysqlEndpoints {
		meta := fromMySQLEndpoint(item)
		if cfg, ok := configMap[item.Endpoint]; ok {
			mergeAutoscalerConfig(meta, cfg)
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
		MinReplicas:       meta.MinReplicas,                           // 0 is valid (scale-to-zero)
		MaxReplicas:       meta.MaxReplicas,                           // Direct value (0 means no autoscaling)
		Replicas:          meta.Replicas,                              // Direct value
		ScaleUpThreshold:  defaultIfZero(meta.ScaleUpThreshold, 1),    // 0 behaves like 1 anyway
		ScaleDownIdleTime: defaultIfZero(meta.ScaleDownIdleTime, 300), // Use default if not set
		ScaleUpCooldown:   meta.ScaleUpCooldown,                       // 0 is valid (no cooldown)
		ScaleDownCooldown: meta.ScaleDownCooldown,                     // 0 is valid (no cooldown)
		Priority:          meta.Priority,                              // 0 is valid (lowest priority)
		EnableDynamicPrio: enableDynamic,
		HighLoadThreshold: defaultIfZero(meta.HighLoadThreshold, 10), // Use default if not set
		PriorityBoost:     meta.PriorityBoost,                        // 0 is valid (no boost)
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
		Endpoint:         endpoint.Name,
		SpecName:         endpoint.SpecName,
		Description:      endpoint.Description,
		Image:            endpoint.Image,
		ImagePrefix:      endpoint.ImagePrefix,
		ImageDigest:      endpoint.ImageDigest,
		ImageLastChecked: endpoint.ImageLastChecked,
		LatestImage:      endpoint.LatestImage,
		Replicas:         endpoint.Replicas,
		GpuCount:         endpoint.GpuCount,
		TaskTimeout:      endpoint.TaskTimeout,
		EnablePtrace:     endpoint.EnablePtrace,
		Env:              mysql.StringMapToJSONMap(endpoint.Env),
		Labels:           mysql.StringMapToJSONMap(endpoint.Labels),
		Status:           endpoint.Status,
		CreatedAt:        endpoint.CreatedAt,
		UpdatedAt:        endpoint.UpdatedAt,
	}
}

func fromMySQLEndpoint(endpoint *mysql.Endpoint) *interfaces.EndpointMetadata {
	meta := &interfaces.EndpointMetadata{
		Name:              endpoint.Endpoint,
		SpecName:          endpoint.SpecName,
		Description:       endpoint.Description,
		Image:             endpoint.Image,
		ImagePrefix:       endpoint.ImagePrefix,
		ImageDigest:       endpoint.ImageDigest,
		ImageLastChecked:  endpoint.ImageLastChecked,
		LatestImage:       endpoint.LatestImage,
		Replicas:          endpoint.Replicas,
		GpuCount:          endpoint.GpuCount,
		TaskTimeout:       endpoint.TaskTimeout,
		EnablePtrace:      endpoint.EnablePtrace,
		MaxPendingTasks:   endpoint.MaxPendingTasks,
		Env:               mysql.JSONMapToStringMap(endpoint.Env),
		Labels:            mysql.JSONMapToStringMap(endpoint.Labels),
		Status:            endpoint.Status,
		HealthStatus:      endpoint.HealthStatus,
		LastHealthCheckAt: endpoint.LastHealthCheckAt,
		CreatedAt:         endpoint.CreatedAt,
		UpdatedAt:         endpoint.UpdatedAt,
	}
	// Set health message if present
	if endpoint.HealthMessage != nil {
		meta.HealthMessage = *endpoint.HealthMessage
	}
	// Parse RuntimeState
	if endpoint.RuntimeState != nil {
		if ns, ok := endpoint.RuntimeState["namespace"].(string); ok {
			meta.Namespace = ns
		}
		if rr, ok := endpoint.RuntimeState["readyReplicas"].(float64); ok {
			meta.ReadyReplicas = int(rr)
		}
		if ar, ok := endpoint.RuntimeState["availableReplicas"].(float64); ok {
			meta.AvailableReplicas = int(ar)
		}
		if shm, ok := endpoint.RuntimeState["shmSize"].(string); ok {
			meta.ShmSize = shm
		}
		if vm, ok := endpoint.RuntimeState["volumeMounts"].([]interface{}); ok {
			for _, v := range vm {
				if m, ok := v.(map[string]interface{}); ok {
					meta.VolumeMounts = append(meta.VolumeMounts, interfaces.VolumeMount{
						PVCName:   m["pvcName"].(string),
						MountPath: m["mountPath"].(string),
					})
				}
			}
		}
	}
	return meta
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
