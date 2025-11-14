package model

import (
	"time"
)

// WorkerStatus worker node status
type WorkerStatus string

const (
	WorkerStatusOnline   WorkerStatus = "ONLINE"   // Online - normal operation
	WorkerStatusOffline  WorkerStatus = "OFFLINE"  // Offline - disconnected
	WorkerStatusBusy     WorkerStatus = "BUSY"     // Busy - processing tasks
	WorkerStatusDraining WorkerStatus = "DRAINING" // Draining - pod terminating, no new tasks
)

// Worker worker node information
type Worker struct {
	ID              string       `json:"id"`
	Endpoint        string       `json:"endpoint"`         // Endpoint to which the worker belongs
	Status          WorkerStatus `json:"status"`
	Concurrency     int          `json:"concurrency"`      // Maximum concurrency
	CurrentJobs     int          `json:"current_jobs"`     // Current number of jobs
	JobsInProgress  []string     `json:"jobs_in_progress"` // List of in-progress task IDs
	LastHeartbeat   time.Time    `json:"last_heartbeat"`
	Version         string       `json:"version,omitempty"`
	RegisteredAt    time.Time    `json:"registered_at"`
	PodName         string       `json:"pod_name,omitempty"` // K8s pod name (from RUNPOD_POD_ID env)
}

// HeartbeatRequest heartbeat request
type HeartbeatRequest struct {
	WorkerID       string   `json:"worker_id" binding:"required"`
	JobsInProgress []string `json:"job_in_progress"` // Field name consistent with runpod
	Concurrency    int      `json:"concurrency"`
	Version        string   `json:"version,omitempty"`
}

// JobPullRequest job pull request
type JobPullRequest struct {
	WorkerID       string   `json:"worker_id" binding:"required"`
	JobsInProgress []string `json:"job_in_progress"` // Consistent with runpod
	BatchSize      int      `json:"batch_size"`      // Batch pull count
}

// JobPullResponse job pull response
type JobPullResponse struct {
	Jobs []JobInfo `json:"jobs,omitempty"`
}

// JobInfo job information (returned when pulling)
type JobInfo struct {
	ID    string                 `json:"id"`
	Input map[string]interface{} `json:"input"`
}

// JobResultRequest job result submission
type JobResultRequest struct {
	TaskID  string                 `json:"task_id"` // TaskID can be passed via JSON body or X-Request-ID header
	Output  map[string]interface{} `json:"output,omitempty"`
	Error   string                 `json:"error,omitempty"`
	StopPod bool                   `json:"stopPod,omitempty"` // Consistent with runpod
}

// StreamResultRequest stream result submission
type StreamResultRequest struct {
	TaskID string      `json:"task_id"` // TaskID can be passed via JSON body or X-Request-ID header
	Output interface{} `json:"output"`
}
