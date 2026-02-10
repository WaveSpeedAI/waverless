package novita

import (
	"fmt"
	"strconv"
	"strings"

	"waverless/pkg/interfaces"
	"waverless/pkg/logger"
)

const (
	// Spec names
	SpecNameNovitaH100Single = "novita-h100-single"
	SpecNameNovitaA100Single = "novita-a100-single"
	SpecNameNovitaA10Single  = "novita-a10-single"
	SpecNameNovitaH200Single = "novita-h200-single"
	SpecNameH100Single       = "h100-single"
	SpecNameA1004x           = "a100-4x"

	// Product IDs (placeholder values, replace with actual Novita product IDs)
	ProductIDH100Single = "novita-h100-80gb-product-id"
	ProductIDA100Single = "novita-a100-40gb-product-id"
	ProductIDA10Single  = "novita-a10-24gb-product-id"
	ProductIDH200Single = "novita-h200-141gb-product-id"
	ProductIDA1004x     = "novita-a100-4x-product-id"

	// GPU Types
	GPUTypeH100 = "NVIDIA-H100"
	GPUTypeA100 = "NVIDIA-A100"
	GPUTypeA10  = "NVIDIA-A10"
	GPUTypeH200 = "NVIDIA-H200"

	// Resource category and type
	CategoryGPU            = "gpu"
	ResourceTypeServerless = "serverless"

	// Standard GPU counts
	GPUCount1 = "1"

	// CPU and Memory configurations
	CPUH100    = "16"
	MemoryH100 = "80Gi"
	CPUA100    = "12"
	MemoryA100 = "64Gi"
	CPUA10     = "8"
	MemoryA10  = "32Gi"
	CPUH200    = "16"
	MemoryH200 = "141Gi"

	// Display names
	DisplayNameH100Single = "Novita H100 1x GPU"
	DisplayNameA100Single = "Novita A100 1x GPU"
	DisplayNameA10Single  = "Novita A10 1x GPU"
	DisplayNameH200Single = "Novita H200 1x GPU"

	// Default values
	DefaultPort           = 8000
	DefaultHealthPath     = "/health"
	DefaultRootfsSize     = 100
	DefaultFreeTimeout    = 300 // 5 minutes
	DefaultMaxConcurrent  = 1
	DefaultRegion         = "us-dallas-nas-2" // Default region
	DefaultQueueWaitTime  = 60                // 60 seconds queue wait time
	DefaultLocalStorageGB = 30                // 30GB local storage
	DefaultRequestTimeout = 3600              // 1 hour request timeout

	// Label keys
	LabelKeyRegion     = "region"
	LabelKeyProvider   = "provider"
	LabelKeyEndpointID = "endpoint-id"

	// Label values
	LabelValueNovita = "novita"

	// Policy types
	PolicyTypeQueue       = "queue"
	PolicyTypeConcurrency = "concurrency"

	// Storage types
	// StorageTypeLocal   = "local"
	StorageTypeNetwork = "network"

	// App/Endpoint types
	TypeServerlessEndpoint = "ServerlessEndpoint"

	// Status strings
	StatusRunning     = "Running"
	StatusStopped     = "Stopped"
	StatusFailed      = "Failed"
	StatusPending     = "Pending"
	StatusCreating    = "Creating"
	StatusUpdating    = "Updating"
	StatusTerminating = "Terminating"
	StatusUnknown     = "Unknown"

	// Novita status strings (lowercase)
	NovitaStatusServing  = "serving" // Endpoint is serving (available)
	NovitaStatusRunning  = "running" // Worker is running
	NovitaStatusStopped  = "stopped"
	NovitaStatusFailed   = "failed"
	NovitaStatusPending  = "pending"
	NovitaStatusCreating = "creating"
	NovitaStatusUpdating = "updating"
	NovitaStatusDeleting = "deleting"
	NovitaStatusRemoved  = "removed" // Worker has been deleted, retained for 10 minutes

	// Environment variable keys
	EnvKeyNovitaProvider = "NOVITA_PROVIDER"
	EnvKeyProviderType   = "PROVIDER_TYPE"
	EnvKeyNovitaRegion   = "NOVITA_REGION"

	// Environment variable values
	EnvValueTrue   = "true"
	EnvValueNovita = "novita"

	// Messages
	MessageNoStatusInfo      = "No status information available"
	MessageDeploySuccess     = "Endpoint deployed successfully"
	MessageUpdateSuccess     = "Endpoint updated successfully"
	MessageDeleteSuccess     = "Successfully deleted endpoint"
	MessageNotSupported      = "not supported by Novita provider"
	MessageLogsNotSupported  = "GetAppLogs is not supported by Novita provider - please use Novita dashboard for logs"
	MessagePodsNotSupported  = "GetPods is not supported by Novita provider - Novita manages workers internally"
	MessageWatchNotSupported = "WatchReplicas is implemented using polling mechanism"
)

// extractNovitaConfig extracts PlatformConfig from spec.Platforms
func extractNovitaConfig(spec *interfaces.SpecInfo) (PlatformConfig, error) {
	platformData, ok := spec.Platforms[PlatformNovita]
	if !ok {
		return PlatformConfig{}, fmt.Errorf("novita config not found for spec %s (available platforms: %v)", spec.Name, spec.Platforms)
	}

	// Direct type assertion
	if cfg, ok := platformData.(PlatformConfig); ok {
		return cfg, nil
	}

	// Handle map[string]interface{} from database
	if m, ok := platformData.(map[string]interface{}); ok {
		cfg := PlatformConfig{}
		if v, ok := m["productId"].(string); ok {
			cfg.ProductID = v
		}
		if v, ok := m["region"].(string); ok {
			cfg.Region = v
		}
		if v, ok := m["cudaVersion"].(string); ok {
			cfg.CudaVersion = v
		}
		return cfg, nil
	}

	return PlatformConfig{}, fmt.Errorf("invalid novita config type %T for spec %s", platformData, spec.Name)
}

// mapDeployRequestToNovita converts Waverless DeployRequest to Novita CreateEndpointRequest
func mapDeployRequestToNovita(req *interfaces.DeployRequest, spec *interfaces.SpecInfo) (*CreateEndpointRequest, error) {
	novitaConfig, err := extractNovitaConfig(spec)
	if err != nil {
		return nil, err
	}
	gpuNum, err := strconv.ParseInt(spec.Resources.GPU, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse GPU number: %w", err)
	}
	rootfsSize, err := strconv.ParseInt(spec.Resources.EphemeralStorage, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse rootfs size from '%s': %w", spec.Resources.EphemeralStorage, err)
	}
	// Worker configuration
	workerConfig := WorkerConfig{
		MinNum:         req.Replicas,
		MaxNum:         req.Replicas, // Initially set to same as min
		FreeTimeout:    DefaultFreeTimeout,
		MaxConcurrent:  DefaultMaxConcurrent,
		GPUNum:         int(gpuNum),
		RequestTimeout: DefaultRequestTimeout,
	}

	// Set optional fields only if they have values
	if novitaConfig.CudaVersion != "" {
		workerConfig.CudaVersion = novitaConfig.CudaVersion
	}

	// Port configuration - default to 8000
	ports := []PortConfig{
		{Port: DefaultPort},
	}

	// Auto-scaling policy - single object (not array)
	policy := PolicyConfig{
		Type:  PolicyTypeQueue,
		Value: DefaultQueueWaitTime,
	}

	// Image configuration - single object (not array)
	imageConfig := ImageConfig{
		Image: req.Image,
	}

	// Product configuration
	products := []ProductConfig{
		{ID: novitaConfig.ProductID},
	}

	// Environment variables
	var envs []EnvVar
	for k, v := range req.Env {
		envs = append(envs, EnvVar{
			Key:   k,
			Value: v,
		})
	}

	// Add region as environment variable for worker to access
	envs = append(envs, EnvVar{
		Key:   EnvKeyNovitaRegion,
		Value: novitaConfig.Region,
	})

	// TODO: Volume mounts
	// var volumeMounts []VolumeMount
	// for _, vm := range req.VolumeMounts {
	// 	volumeMounts = append(volumeMounts, VolumeMount{
	// 		Type:      StorageTypeLocal,
	// 		Size:      DefaultLocalStorageGB,
	// 		MountPath: vm.MountPath,
	// 	})
	// }

	// Health check
	healthCheck := &HealthCheck{
		Path: DefaultHealthPath,
	}

	createReq := &CreateEndpointRequest{
		Endpoint: EndpointCreateConfig{
			Name:         req.Endpoint,
			AppName:      req.Endpoint,        // Use endpoint name as app name
			ClusterID:    novitaConfig.Region, // Set region as cluster ID
			WorkerConfig: workerConfig,        // Single object, not array
			Ports:        ports,
			Policy:       policy,      // Single object, not array
			Image:        imageConfig, // Single object, not array
			Products:     products,
			RootfsSize:   int(rootfsSize),
			// VolumeMounts: volumeMounts,
			Envs:    envs,
			Healthy: healthCheck,
		},
	}

	// Log the mapped request for debugging
	logger.Debugf("Mapped CreateEndpoint request: Name=%s, ClusterID=%s, RootfsSize=%d, ProductID=%s, Image=%s",
		createReq.Endpoint.Name,
		createReq.Endpoint.ClusterID,
		createReq.Endpoint.RootfsSize,
		novitaConfig.ProductID,
		imageConfig.Image,
	)

	return createReq, nil
}

// mapNovitaResponseToAppInfo converts Novita GetEndpointResponse to Waverless AppInfo
func mapNovitaResponseToAppInfo(resp *GetEndpointResponse) *interfaces.AppInfo {
	if resp == nil {
		return nil
	}

	endpoint := resp.Endpoint

	// Extract worker configuration
	var replicas, readyReplicas, availableReplicas int32
	replicas = int32(endpoint.WorkerConfig.MaxNum)

	// Count running and healthy workers (exclude removed workers)
	runningWorkers := 0
	healthyWorkers := 0
	for _, worker := range endpoint.Workers {
		if worker.State.State == NovitaStatusRemoved {
			continue
		}
		if worker.State.State == NovitaStatusRunning {
			runningWorkers++
		}
		if worker.Healthy {
			healthyWorkers++
		}
	}

	// Use healthy workers as ready replicas, running workers as available replicas
	readyReplicas = int32(healthyWorkers)
	availableReplicas = int32(runningWorkers)

	// Extract image
	image := endpoint.Image.Image

	// Build labels
	labels := make(map[string]string)
	labels[LabelKeyProvider] = LabelValueNovita
	labels[LabelKeyEndpointID] = endpoint.ID

	// Extract region from environment variables
	for _, env := range endpoint.Envs {
		if env.Key == EnvKeyNovitaRegion {
			labels[LabelKeyRegion] = env.Value
			break
		}
	}

	return &interfaces.AppInfo{
		Name:              endpoint.Name,
		Type:              TypeServerlessEndpoint,
		Status:            mapNovitaStatusToWaverless(endpoint.State.State),
		Replicas:          replicas,
		ReadyReplicas:     readyReplicas,
		AvailableReplicas: availableReplicas,
		Image:             image,
		Labels:            labels,
		CreatedAt:         "", // Not provided in response, could use log timestamp if needed
	}
}

// mapNovitaListItemToAppInfo converts Novita EndpointListItem to Waverless AppInfo
func mapNovitaListItemToAppInfo(item *EndpointListItem) *interfaces.AppInfo {
	if item == nil {
		return nil
	}

	labels := make(map[string]string)
	labels[LabelKeyProvider] = LabelValueNovita
	labels[LabelKeyEndpointID] = item.ID

	return &interfaces.AppInfo{
		Name:      item.Name,
		Type:      TypeServerlessEndpoint,
		Status:    mapNovitaStatusToWaverless(item.State.State),
		Image:     "", // Not available in list view
		Labels:    labels,
		CreatedAt: item.CreatedAt,
	}
}

// mapNovitaStatusToAppStatus converts Novita endpoint data to Waverless AppStatus
func mapNovitaStatusToAppStatus(endpointName string, data *EndpointConfig) *interfaces.AppStatus {
	if data == nil {
		return &interfaces.AppStatus{
			Endpoint: endpointName,
			Status:   StatusUnknown,
			Message:  MessageNoStatusInfo,
		}
	}

	// Count workers by state
	runningWorkers := 0
	healthyWorkers := 0
	pendingWorkers := 0
	for _, worker := range data.Workers {
		if worker.State.State == NovitaStatusRemoved {
			continue // Skip removed workers
		}
		switch worker.State.State {
		case NovitaStatusRunning:
			runningWorkers++
			if worker.Healthy {
				healthyWorkers++
			}
		case NovitaStatusPending, NovitaStatusCreating:
			pendingWorkers++
		}
	}

	totalReplicas := runningWorkers + pendingWorkers

	return &interfaces.AppStatus{
		Endpoint:          endpointName,
		Status:            mapNovitaStatusToWaverless(data.State.State),
		ReadyReplicas:     int32(healthyWorkers),
		AvailableReplicas: int32(runningWorkers),
		TotalReplicas:     int32(totalReplicas),
		Message:           data.State.Message,
	}
}

// mapNovitaStatusToWaverless converts Novita state to Waverless status
func mapNovitaStatusToWaverless(state string) string {
	switch strings.ToLower(state) {
	case NovitaStatusServing, NovitaStatusRunning:
		return StatusRunning
	case NovitaStatusStopped:
		return StatusStopped
	case NovitaStatusFailed:
		return StatusFailed
	case NovitaStatusPending:
		return StatusPending
	case NovitaStatusCreating:
		return StatusCreating
	case NovitaStatusUpdating:
		return StatusUpdating
	case NovitaStatusDeleting:
		return StatusTerminating
	case NovitaStatusRemoved:
		return StatusStopped
	default:
		return StatusUnknown
	}
}

// mapUpdateRequestToNovita converts Waverless UpdateDeploymentRequest to Novita UpdateEndpointRequest
func mapUpdateRequestToNovita(endpointID string, req *interfaces.UpdateDeploymentRequest, currentConfig *GetEndpointResponse) *UpdateEndpointRequest {
	if currentConfig == nil {
		logger.Warnf("No current config available for endpoint %s, update may fail", endpointID)
		return nil
	}

	data := currentConfig.Endpoint

	// Build worker config - use string types for freeTimeout and maxConcurrent
	workerConfig := WorkerConfigResponse{
		MinNum:         data.WorkerConfig.MinNum,
		MaxNum:         data.WorkerConfig.MaxNum,
		FreeTimeout:    data.WorkerConfig.FreeTimeout,   // Already string
		MaxConcurrent:  data.WorkerConfig.MaxConcurrent, // Already string
		GPUNum:         data.WorkerConfig.GPUNum,
		RequestTimeout: data.WorkerConfig.RequestTimeout,
	}

	// Build policy config - use string type for value
	policy := PolicyResponse{
		Type:  data.Policy.Type,
		Value: data.Policy.Value, // Already string
	}

	// Build image config
	imageConfig := ImageConfig{
		Image:   data.Image.Image,
		AuthID:  data.Image.AuthID,
		Command: data.Image.Command,
	}

	// Build ports config
	portsConfig := []PortConfig{}
	for _, port := range data.Ports {
		portsConfig = append(portsConfig, PortConfig{
			Port: port.Port,
		})
	}

	// Build health check config with full details

	// Apply updates from request
	// Update image if specified
	if req.Image != "" {
		imageConfig.Image = req.Image
	}

	// Update replicas if specified
	if req.Replicas != nil {
		workerConfig.MinNum = *req.Replicas
		workerConfig.MaxNum = *req.Replicas
	}

	// Update environment variables if specified
	envs := data.Envs
	if req.Env != nil {
		envs = []EnvVar{}
		for k, v := range *req.Env {
			envs = append(envs, EnvVar{
				Key:   k,
				Value: v,
			})
		}
	}
	healthCheck := &HealthCheck{}
	if data.Healthy != nil {
		healthCheck.Path = data.Healthy.Path
	}
	// Build flattened update request
	return &UpdateEndpointRequest{
		ID:           endpointID,
		Name:         data.Name,
		AppName:      data.AppName,
		WorkerConfig: workerConfig,
		Policy:       policy,
		Image:        imageConfig,
		RootfsSize:   data.RootfsSize,
		VolumeMounts: data.VolumeMounts,
		Envs:         envs,
		Ports:        portsConfig,
		Workers:      nil, // Set to nil as per API example
		Products:     data.Products,
		Healthy:      healthCheck,
	}
}
