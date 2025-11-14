package model

import "time"

// Endpoint MySQL model for endpoints table
type Endpoint struct {
	ID          int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Endpoint    string    `gorm:"column:endpoint;type:varchar(255);not null;uniqueIndex:idx_endpoint_unique" json:"endpoint"`
	SpecName    string    `gorm:"column:spec_name;type:varchar(100);not null" json:"spec_name"`
	Image       string    `gorm:"column:image;type:varchar(500);not null" json:"image"`
	Replicas    int       `gorm:"column:replicas;type:int;not null;default:1" json:"replicas"`
	TaskTimeout int       `gorm:"column:task_timeout;type:int;not null;default:0" json:"task_timeout"`
	Env         JSONMap   `gorm:"column:env;type:json" json:"env"`
	Labels      JSONMap   `gorm:"column:labels;type:json" json:"labels"`
	Status      string    `gorm:"column:status;type:varchar(50);not null;default:active;index:idx_status" json:"status"`
	CreatedAt   time.Time `gorm:"column:created_at;type:datetime(3);not null;default:CURRENT_TIMESTAMP(3);index:idx_created_at" json:"created_at"`
	UpdatedAt   time.Time `gorm:"column:updated_at;type:datetime(3);not null;default:CURRENT_TIMESTAMP(3)" json:"updated_at"`
}

// TableName specifies the table name for Endpoint
func (Endpoint) TableName() string {
	return "endpoints"
}
