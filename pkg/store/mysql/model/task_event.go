package model

import "time"

// TaskEventType event type constants
type TaskEventType string

const (
	// Task lifecycle events
	EventTaskCreated   TaskEventType = "TASK_CREATED"   // Task created by client
	EventTaskQueued    TaskEventType = "TASK_QUEUED"    // Task enqueued to Redis
	EventTaskAssigned  TaskEventType = "TASK_ASSIGNED"  // Task assigned to worker (pulled)
	EventTaskStarted   TaskEventType = "TASK_STARTED"   // Worker notified task started
	EventTaskCompleted TaskEventType = "TASK_COMPLETED" // Task completed successfully
	EventTaskFailed    TaskEventType = "TASK_FAILED"    // Task failed with error
	EventTaskCancelled TaskEventType = "TASK_CANCELLED" // Task cancelled by user

	// Recovery events
	EventTaskRequeued  TaskEventType = "TASK_REQUEUED"  // Task re-queued (e.g., after worker lost)
	EventTaskOrphaned  TaskEventType = "TASK_ORPHANED"  // Task orphaned (worker lost connection)
	EventTaskRecovered TaskEventType = "TASK_RECOVERED" // Task recovered from error state
	EventTaskTimeout   TaskEventType = "TASK_TIMEOUT"   // Task execution timeout

	// Worker events
	EventWorkerChanged TaskEventType = "WORKER_CHANGED" // Worker reassignment
	EventWorkerLost    TaskEventType = "WORKER_LOST"    // Worker lost connection during task execution
)

// TaskEvent MySQL model for task_events table
type TaskEvent struct {
	ID            int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	EventID       string    `gorm:"column:event_id;type:varchar(255);not null;uniqueIndex:idx_event_id_unique" json:"event_id"`
	TaskID        string    `gorm:"column:task_id;type:varchar(255);not null;index:idx_task_id_event_time,priority:1" json:"task_id"`
	Endpoint      string    `gorm:"column:endpoint;type:varchar(255);not null;index:idx_endpoint_event_time,priority:1" json:"endpoint"`
	EventType     string    `gorm:"column:event_type;type:varchar(50);not null;index:idx_event_type" json:"event_type"`
	EventTime     time.Time `gorm:"column:event_time;type:datetime(3);not null;default:CURRENT_TIMESTAMP(3);index:idx_task_id_event_time,priority:2;index:idx_endpoint_event_time,priority:2;index:idx_event_time" json:"event_time"`
	WorkerID      string    `gorm:"column:worker_id;type:varchar(255);index:idx_worker_id" json:"worker_id"`
	WorkerPodName string    `gorm:"column:worker_pod_name;type:varchar(255)" json:"worker_pod_name"`
	FromStatus    string    `gorm:"column:from_status;type:varchar(50)" json:"from_status"`
	ToStatus      string    `gorm:"column:to_status;type:varchar(50)" json:"to_status"`
	ErrorMessage  string    `gorm:"column:error_message;type:text" json:"error_message"`
	ErrorType     string    `gorm:"column:error_type;type:varchar(100)" json:"error_type"`
	RetryCount    int       `gorm:"column:retry_count;type:int;default:0" json:"retry_count"`
	Metadata      JSONMap   `gorm:"column:metadata;type:json" json:"metadata"`
}

// TableName specifies the table name for TaskEvent
func (TaskEvent) TableName() string {
	return "task_events"
}
