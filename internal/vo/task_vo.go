// Package vo defines View Objects for API responses
// VO layer is responsible for converting internal data models to API response format
package vo

import (
	"time"

	"waverless/internal/model"
	"waverless/pkg/store/mysql"
)

// TaskResponse is the task response (RunPod format compatible)
// Used for /v1/status/{task_id} and similar endpoints
// Note: For API compatibility, this type is consistent with model.TaskResponse
type TaskResponse = model.TaskResponse

// TaskListResponse is the task list response
// Used for /api/v1/tasks and similar endpoints
type TaskListResponse struct {
	Tasks  []*TaskResponse `json:"tasks"`
	Total  int64           `json:"total"`
	Limit  int             `json:"limit"`
	Offset int             `json:"offset"`
}

// TaskSubmitResponse is the task submission response
// Used for /v1/{endpoint}/run and similar endpoints
// Note: For API compatibility, this type is consistent with model.SubmitResponse
type TaskSubmitResponse = model.SubmitResponse

// TaskStatsResponse is the task statistics response
// Used for /v1/{endpoint}/stats and similar endpoints
type TaskStatsResponse struct {
	Endpoint     string `json:"endpoint"`
	Pending      int64  `json:"pending"`
	InProgress   int    `json:"in_progress"`
	TotalWorkers int    `json:"total_workers"`
	BusyWorkers  int    `json:"busy_workers"`
}

// TaskEligibilityResponse is the task submission eligibility check response
// Used for /v1/{endpoint}/check and similar endpoints
type TaskEligibilityResponse struct {
	Endpoint        string `json:"endpoint"`
	CanSubmit       bool   `json:"can_submit"`
	PendingTasks    int64  `json:"pending_tasks"`
	MaxPendingTasks int    `json:"max_pending_tasks"`
	Message         string `json:"message"`
}

// TaskEventResponse is the task event response
type TaskEventResponse struct {
	TaskID string `json:"task_id"`
	Events []any  `json:"events"`
	Total  int    `json:"total"`
}

// TaskTimelineResponse is the task timeline response
type TaskTimelineResponse struct {
	TaskID   string `json:"task_id"`
	Timeline []any  `json:"timeline"`
	Total    int    `json:"total"`
}

// TaskExecutionHistoryResponse is the task execution history response
type TaskExecutionHistoryResponse struct {
	TaskID  string `json:"task_id"`
	History []any  `json:"history"`
}

// FromTask converts Task model to TaskResponse
func FromTask(task *model.Task) *TaskResponse {
	if task == nil {
		return nil
	}

	resp := &TaskResponse{
		ID:       task.ID,
		Status:   string(task.Status),
		Endpoint: task.Endpoint,
		WorkerID: task.WorkerID,
		Input:    task.Input,
		Output:   task.Output,
		Error:    task.Error,
	}

	if !task.CreatedAt.IsZero() {
		resp.CreatedAt = task.CreatedAt.Format(time.RFC3339)
	}

	// Calculate delay time
	if task.StartedAt != nil && !task.StartedAt.IsZero() {
		resp.DelayTime = task.StartedAt.Sub(task.CreatedAt).Milliseconds()
	}

	// Calculate execution time
	if task.CompletedAt != nil && task.StartedAt != nil {
		resp.ExecutionMS = task.CompletedAt.Sub(*task.StartedAt).Milliseconds()
	}

	return resp
}

// FromMySQLTask converts MySQL Task model to TaskResponse
func FromMySQLTask(task *mysql.Task) *TaskResponse {
	if task == nil {
		return nil
	}

	resp := &TaskResponse{
		ID:       task.TaskID,
		Status:   task.Status,
		Endpoint: task.Endpoint,
		WorkerID: task.WorkerID,
		Error:    task.Error,
	}

	if !task.CreatedAt.IsZero() {
		resp.CreatedAt = task.CreatedAt.Format(time.RFC3339)
	}

	// Calculate delay time
	if task.StartedAt != nil && !task.StartedAt.IsZero() {
		resp.DelayTime = task.StartedAt.Sub(task.CreatedAt).Milliseconds()
	}

	// Calculate execution time
	if task.CompletedAt != nil && task.StartedAt != nil {
		resp.ExecutionMS = task.CompletedAt.Sub(*task.StartedAt).Milliseconds()
	}

	return resp
}

// FromTasks batch converts Task list
func FromTasks(tasks []*model.Task) []*TaskResponse {
	result := make([]*TaskResponse, len(tasks))
	for i, task := range tasks {
		result[i] = FromTask(task)
	}
	return result
}
