package model

import "time"

// ScalingEvent MySQL model for scaling_events table
type ScalingEvent struct {
	ID            int64           `gorm:"primaryKey;autoIncrement" json:"id"`
	EventID       string          `gorm:"column:event_id;type:varchar(255);not null;uniqueIndex:idx_event_id_unique" json:"event_id"`
	Endpoint      string          `gorm:"column:endpoint;type:varchar(255);not null;index:idx_endpoint_timestamp,priority:1" json:"endpoint"`
	Timestamp     time.Time       `gorm:"column:timestamp;type:datetime(3);not null;default:CURRENT_TIMESTAMP(3);index:idx_timestamp;index:idx_endpoint_timestamp,priority:2" json:"timestamp"`
	Action        string          `gorm:"column:action;type:varchar(50);not null;index:idx_action" json:"action"`
	FromReplicas  int             `gorm:"column:from_replicas;type:int;not null" json:"from_replicas"`
	ToReplicas    int             `gorm:"column:to_replicas;type:int;not null" json:"to_replicas"`
	Reason        string          `gorm:"column:reason;type:text;not null" json:"reason"`
	QueueLength   int64           `gorm:"column:queue_length;type:bigint;not null;default:0" json:"queue_length"`
	Priority      int             `gorm:"column:priority;type:int;not null;default:50" json:"priority"`
	PreemptedFrom JSONStringArray `gorm:"column:preempted_from;type:json" json:"preempted_from"`
}

// TableName specifies the table name for ScalingEvent
func (ScalingEvent) TableName() string {
	return "scaling_events"
}
