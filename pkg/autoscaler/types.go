package autoscaler

import (
	"time"

	"waverless/pkg/interfaces"
)

// Config 自动扩缩容配置
type Config struct {
	Enabled        bool `json:"enabled"`        // 是否启用自动扩缩容
	Interval       int  `json:"interval"`       // 控制循环间隔（秒）
	MaxGPUCount    int  `json:"maxGpuCount"`    // 集群总 GPU 数量
	MaxCPUCores    int  `json:"maxCpuCores"`    // 集群总 CPU 核心数
	MaxMemoryGB    int  `json:"maxMemoryGB"`    // 集群总内存（GB）
	StarvationTime int  `json:"starvationTime"` // 饥饿时间阈值（秒），超过此时间未分配资源则临时提升优先级
}

// EndpointConfig is an alias to interfaces.EndpointConfig (domain model)
// This allows autoscaler package to use the type without redefining it
type EndpointConfig = interfaces.EndpointConfig

// Resources 资源定义
type Resources struct {
	GPUCount int     `json:"gpuCount"`
	CPUCores float64 `json:"cpuCores"`
	MemoryGB float64 `json:"memoryGB"`
}

// Add 增加资源
func (r *Resources) Add(other *Resources) {
	r.GPUCount += other.GPUCount
	r.CPUCores += other.CPUCores
	r.MemoryGB += other.MemoryGB
}

// Subtract 减少资源
func (r *Resources) Subtract(other *Resources) {
	r.GPUCount -= other.GPUCount
	r.CPUCores -= other.CPUCores
	r.MemoryGB -= other.MemoryGB
}

// CanAllocate 检查是否可以分配指定资源
// 注意：如果 available 资源为 -1，表示该资源不受限制（unlimited）
func (r *Resources) CanAllocate(required *Resources) bool {
	// GPU check (0 means unlimited)
	gpuOk := r.GPUCount < 0 || r.GPUCount >= required.GPUCount

	// CPU check (0 or negative means unlimited)
	cpuOk := r.CPUCores <= 0 || r.CPUCores >= required.CPUCores

	// Memory check (0 or negative means unlimited)
	memOk := r.MemoryGB <= 0 || r.MemoryGB >= required.MemoryGB

	return gpuOk && cpuOk && memOk
}

// Clone 克隆资源对象
func (r *Resources) Clone() *Resources {
	return &Resources{
		GPUCount: r.GPUCount,
		CPUCores: r.CPUCores,
		MemoryGB: r.MemoryGB,
	}
}

// ScaleDecision 扩缩容决策
type ScaleDecision struct {
	Endpoint         string    `json:"endpoint"`
	CurrentReplicas  int       `json:"currentReplicas"`
	DesiredReplicas  int       `json:"desiredReplicas"`
	ScaleAmount      int       `json:"scaleAmount"` // 正数为扩容，负数为缩容
	Priority         int       `json:"priority"`    // 有效优先级（包含动态调整）
	BasePriority     int       `json:"basePriority"`
	QueueLength      int64     `json:"queueLength"`
	Reason           string    `json:"reason"`
	Approved         bool      `json:"approved"`
	Blocked          bool      `json:"blocked"`
	BlockedReason    string    `json:"blockedReason,omitempty"`
	PreemptedFrom    []string  `json:"preemptedFrom,omitempty"`    // 从哪些 endpoint 抢占的资源
	RequiredResource Resources `json:"requiredResource,omitempty"` // 所需资源
}

// ScalingEvent is an alias to interfaces.ScalingEvent (domain model)
// This allows autoscaler package to use the type without redefining it
type ScalingEvent = interfaces.ScalingEvent

// ClusterResources 集群资源状态
type ClusterResources struct {
	Total     Resources            `json:"total"`     // 总资源
	Used      Resources            `json:"used"`      // 已使用资源
	Available Resources            `json:"available"` // 可用资源
	BySpec    map[string]Resources `json:"bySpec"`    // 按 Spec 分类的资源需求
}

// EndpointStatus Endpoint 状态（用于监控和展示）
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
	IdleTime         float64   `json:"idleTime"` // 秒
	WaitingTime      float64   `json:"waitingTime"`
	ResourceUsage    Resources `json:"resourceUsage"`
}

// AutoScalerStatus 自动扩缩容系统状态
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
