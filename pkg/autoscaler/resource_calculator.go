package autoscaler

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	endpointsvc "waverless/internal/service/endpoint"
	"waverless/pkg/interfaces"
	"waverless/pkg/logger"
	"waverless/pkg/provider/k8s"
)

// ResourceCalculator resource calculator
type ResourceCalculator struct {
	deploymentProvider interfaces.DeploymentProvider
	endpointService    *endpointsvc.Service
	specManager        *k8s.SpecManager
}

// NewResourceCalculator creates a resource calculator
func NewResourceCalculator(deploymentProvider interfaces.DeploymentProvider, endpointService *endpointsvc.Service, specManager *k8s.SpecManager) *ResourceCalculator {
	return &ResourceCalculator{
		deploymentProvider: deploymentProvider,
		endpointService:    endpointService,
		specManager:        specManager,
	}
}

// CalculateEndpointResource calculates resources needed for a single Endpoint
// OPTIMIZATION: Accept specName parameter to avoid re-querying metadata
func (c *ResourceCalculator) CalculateEndpointResource(ctx context.Context, endpoint *EndpointConfig, replicas int) (*Resources, error) {
	// Use SpecName from EndpointConfig to avoid redundant metadata query
	specName := endpoint.SpecName
	if specName == "" {
		// Fallback: query metadata (for backward compatibility)
		meta, err := c.getEndpointMetadata(ctx, endpoint.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to get endpoint metadata: %w", err)
		}
		specName = meta.SpecName
	}

	// Get spec
	spec, err := c.specManager.GetSpec(specName)
	if err != nil {
		return nil, fmt.Errorf("failed to get spec %s: %w", specName, err)
	}

	// Parse resource requirements
	resources := &Resources{}

	// GPU
	if spec.Category == "gpu" && spec.Resources.GPU != "" {
		gpuCount, err := strconv.Atoi(spec.Resources.GPU)
		if err != nil {
			logger.WarnCtx(ctx, "failed to parse GPU count for spec %s: %v", spec.Name, err)
			gpuCount = 0
		}
		resources.GPUCount = gpuCount * replicas
	}

	// CPU
	cpuStr := spec.Resources.CPU
	cpuCores, err := parseCPU(cpuStr)
	if err != nil {
		logger.WarnCtx(ctx, "failed to parse CPU for spec %s: %v", spec.Name, err)
		cpuCores = 1.0
	}
	resources.CPUCores = cpuCores * float64(replicas)

	// Memory
	memStr := spec.Resources.Memory
	memGB, err := parseMemory(memStr)
	if err != nil {
		logger.WarnCtx(ctx, "failed to parse memory for spec %s: %v", spec.Name, err)
		memGB = 1.0
	}
	resources.MemoryGB = memGB * float64(replicas)

	return resources, nil
}

// CalculateClusterResources calculates cluster resource usage
func (c *ResourceCalculator) CalculateClusterResources(ctx context.Context, endpoints []*EndpointConfig, maxResources *Resources) (*ClusterResources, error) {
	cluster := &ClusterResources{
		Total:     *maxResources,
		Used:      Resources{},
		Available: *maxResources.Clone(),
		BySpec:    make(map[string]Resources),
	}

	// Calculate resources used by each endpoint
	// OPTIMIZATION: Use SpecName from EndpointConfig instead of re-querying metadata
	for _, ep := range endpoints {
		if ep.ActualReplicas == 0 {
			continue
		}

		resources, err := c.CalculateEndpointResource(ctx, ep, ep.ActualReplicas)
		if err != nil {
			logger.WarnCtx(ctx, "failed to calculate resources for endpoint %s: %v", ep.Name, err)
			continue
		}

		cluster.Used.Add(resources)

		// Categorize by spec - use SpecName from EndpointConfig
		if ep.SpecName != "" {
			if _, ok := cluster.BySpec[ep.SpecName]; !ok {
				cluster.BySpec[ep.SpecName] = Resources{}
			}
			spec := cluster.BySpec[ep.SpecName]
			spec.Add(resources)
			cluster.BySpec[ep.SpecName] = spec
		}
	}

	// Calculate available resources
	cluster.Available.Subtract(&cluster.Used)

	return cluster, nil
}

// parseCPU parses CPU string (e.g., "4", "4000m")
func parseCPU(cpuStr string) (float64, error) {
	cpuStr = strings.TrimSpace(cpuStr)
	if cpuStr == "" {
		return 1.0, nil
	}

	// Handle millicores format (e.g., "4000m")
	if strings.HasSuffix(cpuStr, "m") {
		milliStr := strings.TrimSuffix(cpuStr, "m")
		milli, err := strconv.ParseFloat(milliStr, 64)
		if err != nil {
			return 0, err
		}
		return milli / 1000.0, nil
	}

	// Handle plain number format (e.g., "4")
	cores, err := strconv.ParseFloat(cpuStr, 64)
	if err != nil {
		return 0, err
	}
	return cores, nil
}

// parseMemory parses memory string (e.g., "8Gi", "8192Mi", "8GB")
func parseMemory(memStr string) (float64, error) {
	memStr = strings.TrimSpace(memStr)
	if memStr == "" {
		return 1.0, nil
	}

	// Remove possible units
	memStr = strings.ToUpper(memStr)

	// Gibibyte (1 GiB = 1024 MiB)
	if strings.HasSuffix(memStr, "GI") || strings.HasSuffix(memStr, "GIB") {
		numStr := strings.TrimSuffix(strings.TrimSuffix(memStr, "B"), "I")
		numStr = strings.TrimSuffix(numStr, "G")
		num, err := strconv.ParseFloat(numStr, 64)
		if err != nil {
			return 0, err
		}
		return num, nil
	}

	// Gigabyte (1 GB = 1000 MB)
	if strings.HasSuffix(memStr, "GB") || strings.HasSuffix(memStr, "G") {
		numStr := strings.TrimSuffix(strings.TrimSuffix(memStr, "B"), "G")
		num, err := strconv.ParseFloat(numStr, 64)
		if err != nil {
			return 0, err
		}
		return num * 0.93, nil // Approximate conversion from GB to GiB
	}

	// Mebibyte (1 MiB = 1024 KiB)
	if strings.HasSuffix(memStr, "MI") || strings.HasSuffix(memStr, "MIB") {
		numStr := strings.TrimSuffix(strings.TrimSuffix(memStr, "B"), "I")
		numStr = strings.TrimSuffix(numStr, "M")
		num, err := strconv.ParseFloat(numStr, 64)
		if err != nil {
			return 0, err
		}
		return num / 1024.0, nil
	}

	// Megabyte
	if strings.HasSuffix(memStr, "MB") || strings.HasSuffix(memStr, "M") {
		numStr := strings.TrimSuffix(strings.TrimSuffix(memStr, "B"), "M")
		num, err := strconv.ParseFloat(numStr, 64)
		if err != nil {
			return 0, err
		}
		return num / 1024.0 * 0.93, nil
	}

	// Default: treat as number in GB
	num, err := strconv.ParseFloat(memStr, 64)
	if err != nil {
		return 0, err
	}
	return num, nil
}

// getEndpointMetadata gets endpoint metadata (helper method)
func (c *ResourceCalculator) getEndpointMetadata(ctx context.Context, name string) (*interfaces.EndpointMetadata, error) {
	// Get from endpoint service
	return c.endpointService.GetEndpoint(ctx, name)
}
