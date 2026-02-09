package model

import "time"

// StatusEventType represents the type of status event
type StatusEventType string

const (
	// StatusEventTypeStatusChange indicates a worker status change
	StatusEventTypeStatusChange StatusEventType = "STATUS_CHANGE"
	// StatusEventTypePhaseChange indicates a pending phase change
	StatusEventTypePhaseChange StatusEventType = "PHASE_CHANGE"
	// StatusEventTypeFailure indicates a worker failure
	StatusEventTypeFailure StatusEventType = "FAILURE"
	// StatusEventTypeRecovery indicates a worker recovery
	StatusEventTypeRecovery StatusEventType = "RECOVERY"
)

// StatusEvent represents a worker status change event record in database
type StatusEvent struct {
	ID         int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	WorkerID   string    `gorm:"column:worker_id;type:varchar(64);not null;index:idx_worker_created" json:"workerId"`
	Endpoint   string    `gorm:"column:endpoint;type:varchar(255);not null;index:idx_endpoint_created" json:"endpoint"`
	EventType  string    `gorm:"column:event_type;type:varchar(32);not null" json:"eventType"`
	OldStatus  *string   `gorm:"column:old_status;type:varchar(32)" json:"oldStatus,omitempty"`
	NewStatus  string    `gorm:"column:new_status;type:varchar(32);not null" json:"newStatus"`
	Phase      *string   `gorm:"column:phase;type:varchar(32)" json:"phase,omitempty"`
	Reason     *string   `gorm:"column:reason;type:varchar(255)" json:"reason,omitempty"`
	Message    *string   `gorm:"column:message;type:text" json:"message,omitempty"`
	SpotStatus JSONMap   `gorm:"column:spot_status;type:json" json:"spotStatus,omitempty"`
	CreatedAt  time.Time `gorm:"column:created_at;type:datetime(3);not null;default:CURRENT_TIMESTAMP(3);index:idx_created_at" json:"createdAt"`
}

// TableName specifies the table name for StatusEvent
func (StatusEvent) TableName() string {
	return "status_events"
}
