package interfaces

import "time"

// EndpointConfig Endpoint autoscaling configuration
type EndpointConfig struct {
	// Basic information
	Name        string `json:"name"`
	DisplayName string `json:"displayName,omitempty"`
	SpecName    string `json:"specName,omitempty"` // Spec name (for resource calculation, avoid repeated queries)

	// Replica configuration
	MinReplicas int `json:"minReplicas"` // Minimum replica count (default 0)
	MaxReplicas int `json:"maxReplicas"` // Maximum replica count
	Replicas    int `json:"replicas"`    // Current target replica count

	// Scaling thresholds
	ScaleUpThreshold  int `json:"scaleUpThreshold"`  // Queued task count threshold, triggers scale up when exceeded (default 1)
	ScaleDownIdleTime int `json:"scaleDownIdleTime"` // Idle time (seconds), scale down when no tasks for this duration (default 300)

	// Cooldown periods
	ScaleUpCooldown   int `json:"scaleUpCooldown"`   // Scale up cooldown time (seconds), default 30
	ScaleDownCooldown int `json:"scaleDownCooldown"` // Scale down cooldown time (seconds), default 60

	// Priority configuration
	Priority          int  `json:"priority"`          // Base priority (0-100), default 50
	EnableDynamicPrio bool `json:"enableDynamicPrio"` // Whether to enable dynamic priority, default true

	// Dynamic priority parameters
	HighLoadThreshold int `json:"highLoadThreshold"` // High load threshold (queued task count), temporarily boost priority when exceeded, default 10
	PriorityBoost     int `json:"priorityBoost"`     // Priority boost amount during high load, default 20

	// Autoscaler switch override configuration
	// nil/"" = follow global setting (default)
	// "disabled" = force disable autoscaling for this endpoint
	// "enabled" = force enable autoscaling for this endpoint
	AutoscalerEnabled *string `json:"autoscalerEnabled,omitempty"`

	// Runtime state (not persisted)
	ActualReplicas    int                `json:"actualReplicas,omitempty"`    // K8s actual running replica count
	AvailableReplicas int                `json:"availableReplicas,omitempty"` // Available replica count
	Conditions        []ReplicaCondition `json:"conditions,omitempty"`        // Deployment conditions
	DrainingReplicas  int                `json:"drainingReplicas,omitempty"`  // Draining replica count
	PendingTasks      int64              `json:"pendingTasks,omitempty"`      // Current queued task count
	RunningTasks      int64              `json:"runningTasks,omitempty"`      // Current running task count
	LastScaleTime     time.Time          `json:"lastScaleTime,omitempty"`     // Last scaling time
	LastTaskTime      time.Time          `json:"lastTaskTime,omitempty"`      // Last task processing time
	FirstPendingTime  time.Time          `json:"firstPendingTime,omitempty"`  // First task queue time (for starvation detection)
}

// EffectivePriority calculates effective priority (including dynamic adjustments)
func (c *EndpointConfig) EffectivePriority(starvationTime int) int {
	priority := c.Priority

	// Dynamic priority boost (high load)
	if c.EnableDynamicPrio && c.PendingTasks >= int64(c.HighLoadThreshold) {
		priority += c.PriorityBoost
	}

	// Starvation protection: temporarily boost priority when resources not allocated for long time
	if !c.FirstPendingTime.IsZero() && c.PendingTasks > 0 {
		waitingTime := time.Since(c.FirstPendingTime)
		if waitingTime.Seconds() > float64(starvationTime) {
			// Boost 10 points for each starvation threshold multiple, max boost 30
			boostMultiplier := int(waitingTime.Seconds() / float64(starvationTime))
			if boostMultiplier > 3 {
				boostMultiplier = 3
			}
			priority += boostMultiplier * 10
		}
	}

	// Ensure priority within reasonable range
	if priority > 150 {
		priority = 150
	}
	if priority < 0 {
		priority = 0
	}

	return priority
}

// ScalingEvent scaling event (history record)
type ScalingEvent struct {
	ID            string    `json:"id"`
	Endpoint      string    `json:"endpoint"`
	Timestamp     time.Time `json:"timestamp"`
	Action        string    `json:"action"` // "scale_up", "scale_down", "blocked", "preempted"
	FromReplicas  int       `json:"fromReplicas"`
	ToReplicas    int       `json:"toReplicas"`
	Reason        string    `json:"reason"`
	QueueLength   int64     `json:"queueLength"`
	Priority      int       `json:"priority"`
	PreemptedFrom []string  `json:"preemptedFrom,omitempty"`
}
