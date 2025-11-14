package model

import "time"

// TaskStatistics represents task statistics in the database
type TaskStatistics struct {
	ID              int       `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	ScopeType       string    `gorm:"column:scope_type;not null;uniqueIndex:uk_scope" json:"scope_type"` // 'global' or 'endpoint'
	ScopeValue      *string   `gorm:"column:scope_value;uniqueIndex:uk_scope" json:"scope_value"`        // endpoint name or 'global' for global stats
	PendingCount    int       `gorm:"column:pending_count;default:0" json:"pending_count"`               // Number of PENDING tasks
	InProgressCount int       `gorm:"column:in_progress_count;default:0" json:"in_progress_count"`       // Number of IN_PROGRESS tasks
	CompletedCount  int       `gorm:"column:completed_count;default:0" json:"completed_count"`           // Number of COMPLETED tasks
	FailedCount     int       `gorm:"column:failed_count;default:0" json:"failed_count"`                 // Number of FAILED tasks
	CancelledCount  int       `gorm:"column:cancelled_count;default:0" json:"cancelled_count"`           // Number of CANCELLED tasks
	TotalCount      int       `gorm:"column:total_count;default:0" json:"total_count"`                   // Total number of tasks
	UpdatedAt       time.Time `gorm:"column:updated_at;not null" json:"updated_at"`                      // Last update timestamp
}

// TableName returns the table name for TaskStatistics
func (TaskStatistics) TableName() string {
	return "task_statistics"
}
