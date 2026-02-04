package autoscaler

import (
	"time"

	"waverless/pkg/interfaces"
)

// Config defines autoscaler configuration
type Config struct {
	Enabled        bool `json:"enabled"`        // Whether autoscaling is enabled
	Interval       int  `json:"interval"`       // Control loop interval (seconds)
	MaxGPUCount    int  `json:"maxGpuCount"`    // Total cluster GPU count
	MaxCPUCores    int  `json:"maxCpuCores"`    // Total cluster CPU cores
	MaxMemoryGB    int  `json:"maxMemoryGB"`    // Total cluster memory (GB)
	StarvationTime int  `json:"starvationTime"` // Starvation threshold (seconds), temporarily elevate priority if no resources allocated beyond this time
}

// EndpointConfig is an alias to interfaces.EndpointConfig (domain model)
// This allows autoscaler package to use the type without redefining it
type EndpointConfig = interfaces.EndpointConfig

// Resources defines resource requirements
type Resources struct {
	GPUCount int     `json:"gpuCount"`
	CPUCores float64 `json:"cpuCores"`
	MemoryGB float64 `json:"memoryGB"`
}

// Add adds resources
func (r *Resources) Add(other *Resources) {
	r.GPUCount += other.GPUCount
	r.CPUCores += other.CPUCores
	r.MemoryGB += other.MemoryGB
}

// Subtract subtracts resources
func (r *Resources) Subtract(other *Resources) {
	r.GPUCount -= other.GPUCount
	r.CPUCores -= other.CPUCores
	r.MemoryGB -= other.MemoryGB
}

// CanAllocate checks if the required resources can be allocated
// Note: If available resource is -1, it means unlimited
func (r *Resources) CanAllocate(required *Resources) bool {
	// GPU check (0 means unlimited)
	gpuOk := r.GPUCount < 0 || r.GPUCount >= required.GPUCount

	// CPU check (0 or negative means unlimited)
	cpuOk := r.CPUCores <= 0 || r.CPUCores >= required.CPUCores

	// Memory check (0 or negative means unlimited)
	memOk := r.MemoryGB <= 0 || r.MemoryGB >= required.MemoryGB

	return gpuOk && cpuOk && memOk
}

// Clone creates a copy of the resources
func (r *Resources) Clone() *Resources {
	return &Resources{
		GPUCount: r.GPUCount,
		CPUCores: r.CPUCores,
		MemoryGB: r.MemoryGB,
	}
}

// ScaleDecision represents a scaling decision
type ScaleDecision struct {
	Endpoint         string    `json:"endpoint"`
	CurrentReplicas  int       `json:"currentReplicas"`
	DesiredReplicas  int       `json:"desiredReplicas"`
	ScaleAmount      int       `json:"scaleAmount"` // Positive for scale up, negative for scale down
	Priority         int       `json:"priority"`    // Effective priority (including dynamic adjustments)
	BasePriority     int       `json:"basePriority"`
	QueueLength      int64     `json:"queueLength"`
	Reason           string    `json:"reason"`
	Approved         bool      `json:"approved"`
	Blocked          bool      `json:"blocked"`
	BlockedReason    string    `json:"blockedReason,omitempty"`
	PreemptedFrom    []string  `json:"preemptedFrom,omitempty"`    // Endpoints from which resources were preempted
	RequiredResource Resources `json:"requiredResource,omitempty"` // Required resources
}

// ScalingEvent is an alias to interfaces.ScalingEvent (domain model)
// This allows autoscaler package to use the type without redefining it
type ScalingEvent = interfaces.ScalingEvent

// ClusterResources represents cluster resource status
type ClusterResources struct {
	Total     Resources            `json:"total"`     // Total resources
	Used      Resources            `json:"used"`      // Used resources
	Available Resources            `json:"available"` // Available resources
	BySpec    map[string]Resources `json:"bySpec"`    // Resources by spec type
}

// EndpointStatus represents endpoint status for monitoring and display
type EndpointStatus struct {
	Name             string    `json:"name"`
	Enabled          bool      `json:"enabled"`
	CurrentReplicas  int       `json:"currentReplicas"`
	DesiredReplicas  int       `json:"desiredReplicas"`
	MinReplicas      int       `json:"minReplicas"`
	MaxReplicas      int       `json:"maxReplicas"`
	DrainingReplicas int       `json:"drainingReplicas"`
	PendingTasks     int64     `json:"pendingTasks"`
	RunningTasks     int64     `json:"runningTasks"`
	Priority         int       `json:"priority"`
	EffectivePrio    int       `json:"effectivePrio"`
	LastScaleTime    time.Time `json:"lastScaleTime"`
	LastTaskTime     time.Time `json:"lastTaskTime"`
	IdleTime         float64   `json:"idleTime"` // Seconds
	WaitingTime      float64   `json:"waitingTime"`
	ResourceUsage    Resources `json:"resourceUsage"`
}

// ClusterResourcesStatus represents lightweight cluster resource status
type ClusterResourcesStatus struct {
	Enabled          bool             `json:"enabled"`
	Running          bool             `json:"running"`
	LastRunTime      time.Time        `json:"lastRunTime"`
	ClusterResources ClusterResources `json:"clusterResources"`
}

// AutoScalerStatus represents autoscaler system status
type AutoScalerStatus struct {
	Enabled           bool                   `json:"enabled"`
	Running           bool                   `json:"running"`
	LastRunTime       time.Time              `json:"lastRunTime"`
	ClusterResources  ClusterResources       `json:"clusterResources"`
	Endpoints         []EndpointStatus       `json:"endpoints"`
	RecentEvents      []ScalingEvent         `json:"recentEvents"`
	PendingDecisions  []ScaleDecision        `json:"pendingDecisions"`
	BlockedEndpoints  []string               `json:"blockedEndpoints"`
	StarvingEndpoints []string               `json:"starvingEndpoints"`
	Metrics           map[string]interface{} `json:"metrics"`
}
