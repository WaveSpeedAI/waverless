package model

import "time"

// HealthStatus represents the health status of an endpoint
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "HEALTHY"   // All workers running
	HealthStatusDegraded  HealthStatus = "DEGRADED"  // Some workers failed
	HealthStatusUnhealthy HealthStatus = "UNHEALTHY" // All workers failed or image issue
)

// Endpoint MySQL model for endpoints table
type Endpoint struct {
	ID                int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	Endpoint          string     `gorm:"column:endpoint;type:varchar(255);not null;uniqueIndex:idx_endpoint_unique" json:"endpoint"`
	SpecName          string     `gorm:"column:spec_name;type:varchar(100);not null" json:"spec_name"`
	Description       string     `gorm:"column:description;type:varchar(500);not null;default:''" json:"description"`
	Image             string     `gorm:"column:image;type:varchar(500);not null" json:"image"`
	ImagePrefix       string     `gorm:"column:image_prefix;type:varchar(500);not null;default:''" json:"image_prefix"`
	ImageDigest       string     `gorm:"column:image_digest;type:varchar(255);not null;default:''" json:"image_digest"`
	ImageLastChecked  *time.Time `gorm:"column:image_last_checked;type:datetime(3)" json:"image_last_checked"`
	LatestImage       string     `gorm:"column:latest_image;type:varchar(500);not null;default:''" json:"latest_image"`
	Replicas          int        `gorm:"column:replicas;type:int;not null;default:1" json:"replicas"`
	GpuCount          int        `gorm:"column:gpu_count;type:int;not null;default:1" json:"gpu_count"`
	TaskTimeout       int        `gorm:"column:task_timeout;type:int;not null;default:0" json:"task_timeout"`
	EnablePtrace      bool       `gorm:"column:enable_ptrace;type:tinyint(1);not null;default:0" json:"enable_ptrace"`
	MaxPendingTasks   int        `gorm:"column:max_pending_tasks;type:int;not null;default:1" json:"max_pending_tasks"`
	Env               JSONMap    `gorm:"column:env;type:json" json:"env"`
	Labels            JSONMap    `gorm:"column:labels;type:json" json:"labels"`
	RuntimeState      JSONMap    `gorm:"column:runtime_state;type:json" json:"runtime_state"` // K8s runtime: namespace, readyReplicas, availableReplicas, shmSize, volumeMounts
	Status            string     `gorm:"column:status;type:varchar(50);not null;default:active;index:idx_status" json:"status"`
	HealthStatus      string     `gorm:"column:health_status;type:varchar(16);not null;default:HEALTHY;index:idx_health_status" json:"health_status"`
	HealthMessage     *string    `gorm:"column:health_message;type:varchar(512)" json:"health_message,omitempty"`
	LastHealthCheckAt *time.Time `gorm:"column:last_health_check_at;type:datetime(3)" json:"last_health_check_at,omitempty"`
	CreatedAt         time.Time  `gorm:"column:created_at;type:datetime(3);not null;default:CURRENT_TIMESTAMP(3);index:idx_created_at" json:"created_at"`
	UpdatedAt         time.Time  `gorm:"column:updated_at;type:datetime(3);not null;default:CURRENT_TIMESTAMP(3)" json:"updated_at"`
}

// TableName specifies the table name for Endpoint
func (Endpoint) TableName() string {
	return "endpoints"
}
