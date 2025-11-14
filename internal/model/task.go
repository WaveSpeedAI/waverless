package model

import (
	"encoding/json"
	"time"
)

// TaskStatus task status
type TaskStatus string

const (
	TaskStatusPending    TaskStatus = "PENDING"     // Pending
	TaskStatusInProgress TaskStatus = "IN_PROGRESS" // In Progress
	TaskStatusCompleted  TaskStatus = "COMPLETED"   // Completed
	TaskStatusFailed     TaskStatus = "FAILED"      // Failed
	TaskStatusCancelled  TaskStatus = "CANCELLED"   // Cancelled
)

// Task task model
type Task struct {
	ID          string                 `json:"id"`
	Endpoint    string                 `json:"endpoint"`                // Endpoint to which the task belongs
	Input       map[string]interface{} `json:"input"`
	Status      TaskStatus             `json:"status"`
	Output      map[string]interface{} `json:"output,omitempty"`
	Error       string                 `json:"error,omitempty"`
	WorkerID    string                 `json:"worker_id,omitempty"`
	WebhookURL  string                 `json:"webhook_url,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
	StartedAt   *time.Time             `json:"started_at,omitempty"`
	CompletedAt *time.Time             `json:"completed_at,omitempty"`
}

// SubmitRequest submit task request
type SubmitRequest struct {
	Input      map[string]interface{} `json:"input" binding:"required"`
	WebhookURL string                 `json:"webhook,omitempty"`
	Endpoint   string                 `json:"endpoint,omitempty"` // Specify endpoint, internal use
}

// SubmitResponse submit task response
type SubmitResponse struct {
	ID     string     `json:"id"`
	Status TaskStatus `json:"status"`
}

// StatusResponse task status response
type StatusResponse struct {
	ID          string                 `json:"id"`
	Status      TaskStatus             `json:"status"`
	Output      map[string]interface{} `json:"output,omitempty"`
	Error       string                 `json:"error,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
	StartedAt   *time.Time             `json:"started_at,omitempty"`
	CompletedAt *time.Time             `json:"completed_at,omitempty"`
}

// StreamOutput stream output
type StreamOutput struct {
	Output interface{} `json:"output"`
}

// TaskResponse task response (RunPod format compatible)
type TaskResponse struct {
	ID          string                 `json:"id"`
	Status      string                 `json:"status"`
	Endpoint    string                 `json:"endpoint,omitempty"`    // Endpoint name
	WorkerID    string                 `json:"workerId,omitempty"`
	DelayTime   int64                  `json:"delayTime"`   // Processing delay in milliseconds
	ExecutionMS int64                  `json:"executionTime"` // Execution time in milliseconds
	CreatedAt   string                 `json:"createdAt,omitempty"`   // Task creation time (ISO 8601 format)
	Input       map[string]interface{} `json:"input,omitempty"`
	Output      map[string]interface{} `json:"output,omitempty"`
	Error       string                 `json:"error,omitempty"`
}

// ToJSON converts task to JSON bytes
func (t *Task) ToJSON() ([]byte, error) {
	return json.Marshal(t)
}

// FromJSON converts JSON bytes to task
func (t *Task) FromJSON(data []byte) error {
	return json.Unmarshal(data, t)
}
