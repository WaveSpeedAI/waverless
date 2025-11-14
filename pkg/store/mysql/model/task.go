package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// Task MySQL model for tasks table
type Task struct {
	ID          int64       `gorm:"primaryKey;autoIncrement" json:"id"`
	TaskID      string      `gorm:"column:task_id;type:varchar(255);not null;uniqueIndex:idx_task_id_unique" json:"task_id"`
	Endpoint    string      `gorm:"column:endpoint;type:varchar(255);not null;index:idx_endpoint_status,priority:1" json:"endpoint"`
	Input       JSONMap     `gorm:"column:input;type:json;not null" json:"input"`
	Status      string      `gorm:"column:status;type:varchar(50);not null;index:idx_status;index:idx_endpoint_status,priority:2" json:"status"`
	Output      JSONMap     `gorm:"column:output;type:json" json:"output"`
	Error       string      `gorm:"column:error;type:text" json:"error"`
	WorkerID    string      `gorm:"column:worker_id;type:varchar(255);index:idx_worker_id" json:"worker_id"`
	WebhookURL  string      `gorm:"column:webhook_url;type:varchar(1000)" json:"webhook_url"`
	CreatedAt   time.Time   `gorm:"column:created_at;type:datetime(3);not null;default:CURRENT_TIMESTAMP(3);index:idx_created_at" json:"created_at"`
	UpdatedAt   time.Time   `gorm:"column:updated_at;type:datetime(3);not null;default:CURRENT_TIMESTAMP(3)" json:"updated_at"`
	StartedAt   *time.Time  `gorm:"column:started_at;type:datetime(3)" json:"started_at"`
	CompletedAt *time.Time  `gorm:"column:completed_at;type:datetime(3);index:idx_completed_at" json:"completed_at"`
	Extend      *TaskExtend `gorm:"column:extend;type:json" json:"extend,omitempty"`
}

// TaskExtend task execution history (stored in JSON)
// Records each task execution: worker, start time, end time, duration
type TaskExtend []ExecutionRecord

// ExecutionRecord single execution record (simplified)
type ExecutionRecord struct {
	WorkerID   string     `json:"worker_id"`
	StartTime  time.Time  `json:"start_time"`
	EndTime    *time.Time `json:"end_time,omitempty"`
	DurationMs int64      `json:"duration_ms,omitempty"`
}

// TableName specifies the table name for Task
func (Task) TableName() string {
	return "tasks"
}

// Value implements driver.Valuer interface for TaskExtend
// This allows GORM to convert TaskExtend to JSON string for database storage
func (t TaskExtend) Value() (driver.Value, error) {
	if t == nil {
		return nil, nil
	}
	return json.Marshal(t)
}

// Scan implements sql.Scanner interface for TaskExtend
// This allows GORM to convert JSON string from database to TaskExtend
func (t *TaskExtend) Scan(value interface{}) error {
	if value == nil {
		*t = nil
		return nil
	}

	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return fmt.Errorf("failed to scan TaskExtend: unsupported type %T", value)
	}

	var records []ExecutionRecord
	if err := json.Unmarshal(bytes, &records); err != nil {
		return fmt.Errorf("failed to unmarshal TaskExtend: %w", err)
	}

	*t = records
	return nil
}

// AddExecutionRecord adds a new execution record to the history
func (t *Task) AddExecutionRecord(workerID string, startTime time.Time) {
	if t.Extend == nil {
		empty := TaskExtend{}
		t.Extend = &empty
	}
	record := ExecutionRecord{
		WorkerID:  workerID,
		StartTime: startTime,
	}
	*t.Extend = append(*t.Extend, record)
}

// GetCurrentExecution returns the current execution record (last record with nil end_time)
func (t *Task) GetCurrentExecution() *ExecutionRecord {
	if t.Extend == nil || len(*t.Extend) == 0 {
		return nil
	}

	// Find the last record with nil end_time
	for i := len(*t.Extend) - 1; i >= 0; i-- {
		if (*t.Extend)[i].EndTime == nil {
			return &(*t.Extend)[i]
		}
	}
	return nil
}

// CompleteCurrentExecution completes the current execution record
func (t *Task) CompleteCurrentExecution() {
	if t.Extend == nil || len(*t.Extend) == 0 {
		return
	}

	// Find and complete the last uncompleted record
	for i := len(*t.Extend) - 1; i >= 0; i-- {
		if (*t.Extend)[i].EndTime == nil {
			now := time.Now()
			record := &(*t.Extend)[i]
			record.EndTime = &now
			record.DurationMs = now.Sub(record.StartTime).Milliseconds()
			break
		}
	}
}

// GetExecutionHistory returns the execution history
func (t *Task) GetExecutionHistory() []ExecutionRecord {
	if t.Extend == nil {
		return []ExecutionRecord{}
	}
	return *t.Extend
}
