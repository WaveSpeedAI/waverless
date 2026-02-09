package model

import "time"

// Worker represents a worker record in database
type Worker struct {
	ID                   int64      `gorm:"column:id;primaryKey;autoIncrement"`
	WorkerID             string     `gorm:"column:worker_id;not null;uniqueIndex"`
	Endpoint             string     `gorm:"column:endpoint;not null;index"`
	PodName              string     `gorm:"column:pod_name"`
	Status               string     `gorm:"column:status;not null;default:ONLINE"`
	Concurrency          int        `gorm:"column:concurrency;default:1"`
	CurrentJobs          int        `gorm:"column:current_jobs;default:0"`
	JobsInProgress       string     `gorm:"column:jobs_in_progress;type:text"` // JSON array of task IDs
	Version              string     `gorm:"column:version"`
	PodCreatedAt         *time.Time `gorm:"column:pod_created_at"`
	PodStartedAt         *time.Time `gorm:"column:pod_started_at"`
	PodReadyAt           *time.Time `gorm:"column:pod_ready_at"`
	ColdStartDurationMs  *int64     `gorm:"column:cold_start_duration_ms"`
	LastHeartbeat        time.Time  `gorm:"column:last_heartbeat;not null"`
	LastTaskTime         *time.Time `gorm:"column:last_task_time"`
	TotalTasksCompleted  int64      `gorm:"column:total_tasks_completed;default:0"`
	TotalTasksFailed     int64      `gorm:"column:total_tasks_failed;default:0"`
	TotalExecutionTimeMs int64      `gorm:"column:total_execution_time_ms;default:0"`
	RuntimeState         JSONMap    `gorm:"column:runtime_state;type:json"` // Pod runtime: phase, status, reason, message, ip, nodeName
	CreatedAt            time.Time  `gorm:"column:created_at;not null"`
	UpdatedAt            time.Time  `gorm:"column:updated_at;not null"`
	TerminatedAt         *time.Time `gorm:"column:terminated_at"` // Time when worker reached terminal state (pod deleted)

	// Failure tracking fields for image validation and status transparency
	FailureType       string     `gorm:"column:failure_type"`              // IMAGE_PULL_FAILED, CONTAINER_CRASH, RESOURCE_LIMIT, TIMEOUT, UNKNOWN
	FailureReason     string     `gorm:"column:failure_reason"`            // Sanitized user-friendly message
	FailureDetails    string     `gorm:"column:failure_details;type:text"` // JSON with full details for debugging
	FailureOccurredAt *time.Time `gorm:"column:failure_occurred_at"`       // Timestamp when failure was detected

	// Pending phase tracking fields for detailed status visibility
	PendingPhase      *string    `gorm:"column:pending_phase;type:varchar(32)" json:"pendingPhase,omitempty"`            // SCHEDULING, WAITING_NODE, PULLING_IMAGE, INITIALIZING
	PendingPhaseSince *time.Time `gorm:"column:pending_phase_since;type:datetime(3)" json:"pendingPhaseSince,omitempty"` // Timestamp when current pending phase started
	PendingReason     *string    `gorm:"column:pending_reason;type:varchar(255)" json:"pendingReason,omitempty"`         // Reason for pending state
	PendingMessage    *string    `gorm:"column:pending_message;type:text" json:"pendingMessage,omitempty"`               // Detailed message about pending state

	// Spot instance cost tracking fields
	SpotPrice        *float64 `gorm:"column:spot_price;type:decimal(10,6)" json:"spotPrice,omitempty"`              // Spot price (USD/hour) at worker creation time
	SpotInstanceType *string  `gorm:"column:spot_instance_type;type:varchar(64)" json:"spotInstanceType,omitempty"` // Spot instance type (e.g., "g4dn.xlarge")
}

func (Worker) TableName() string {
	return "workers"
}
