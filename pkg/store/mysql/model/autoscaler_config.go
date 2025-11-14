package model

import "time"

// AutoscalerConfig MySQL model for autoscaler_configs table
type AutoscalerConfig struct {
	ID                int64  `gorm:"primaryKey;autoIncrement" json:"id"`
	Endpoint          string `gorm:"column:endpoint;type:varchar(255);not null;uniqueIndex:idx_endpoint_unique" json:"endpoint"`
	DisplayName       string `gorm:"column:display_name;type:varchar(255)" json:"display_name"`
	SpecName          string `gorm:"column:spec_name;type:varchar(100)" json:"spec_name"`
	MinReplicas       int    `gorm:"column:min_replicas;type:int;not null;default:0" json:"min_replicas"`
	MaxReplicas       int    `gorm:"column:max_replicas;type:int;not null;default:10" json:"max_replicas"`
	Replicas          int    `gorm:"column:replicas;type:int;not null;default:1" json:"replicas"`
	ScaleUpThreshold  int    `gorm:"column:scale_up_threshold;type:int;not null;default:1" json:"scale_up_threshold"`
	ScaleDownIdleTime int    `gorm:"column:scale_down_idle_time;type:int;not null;default:300" json:"scale_down_idle_time"`
	ScaleUpCooldown   int    `gorm:"column:scale_up_cooldown;type:int;not null;default:30" json:"scale_up_cooldown"`
	ScaleDownCooldown int    `gorm:"column:scale_down_cooldown;type:int;not null;default:60" json:"scale_down_cooldown"`
	Priority          int    `gorm:"column:priority;type:int;not null;default:50" json:"priority"`
	EnableDynamicPrio bool   `gorm:"column:enable_dynamic_prio;type:boolean;not null;default:true" json:"enable_dynamic_prio"`
	HighLoadThreshold int    `gorm:"column:high_load_threshold;type:int;not null;default:10" json:"high_load_threshold"`
	PriorityBoost     int    `gorm:"column:priority_boost;type:int;not null;default:20" json:"priority_boost"`
	Enabled           bool   `gorm:"column:enabled;type:boolean;not null;default:true;index:idx_enabled" json:"enabled"`
	// AutoscalerEnabled autoscaler switch override configuration
	// nil/"" = follow global setting (default)
	// "disabled" = force disable autoscaling for this endpoint
	// "enabled" = force enable autoscaling for this endpoint
	AutoscalerEnabled *string   `gorm:"column:autoscaler_enabled;type:varchar(20)" json:"autoscaler_enabled,omitempty"`
	// Time tracking fields (for autoscaler decisions)
	LastTaskTime     *time.Time `gorm:"column:last_task_time;type:datetime(3)" json:"last_task_time,omitempty"`     // Last task completion time (for idle time calculation)
	LastScaleTime    *time.Time `gorm:"column:last_scale_time;type:datetime(3)" json:"last_scale_time,omitempty"`   // Last scaling time (for cooldown)
	FirstPendingTime *time.Time `gorm:"column:first_pending_time;type:datetime(3)" json:"first_pending_time,omitempty"` // First pending task time (for starvation prevention)
	CreatedAt         time.Time `gorm:"column:created_at;type:datetime(3);not null;default:CURRENT_TIMESTAMP(3)" json:"created_at"`
	UpdatedAt         time.Time `gorm:"column:updated_at;type:datetime(3);not null;default:CURRENT_TIMESTAMP(3)" json:"updated_at"`
}

// TableName specifies the table name for AutoscalerConfig
func (AutoscalerConfig) TableName() string {
	return "autoscaler_configs"
}
