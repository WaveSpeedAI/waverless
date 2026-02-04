package vo

import (
	"encoding/json"
	"time"

	"waverless/internal/model"
	mysqlmodel "waverless/pkg/store/mysql/model"
)

// WorkerResponse represents the worker response structure
// Used for /api/v1/workers and similar endpoints
type WorkerResponse struct {
	ID             string   `json:"id"`
	Endpoint       string   `json:"endpoint"`
	PodName        string   `json:"pod_name,omitempty"`
	Status         string   `json:"status"`
	Concurrency    int      `json:"concurrency"`
	CurrentJobs    int      `json:"current_jobs"`
	JobsInProgress []string `json:"jobs_in_progress"`
	LastHeartbeat  string   `json:"last_heartbeat"`
	LastTaskTime   string   `json:"last_task_time,omitempty"`
	Version        string   `json:"version,omitempty"`
	RegisteredAt   string   `json:"registered_at"`
}

// WorkerWithPodInfoResponse represents the worker response with Pod information
// Used for /api/v1/endpoints/{name}/workers and similar endpoints
type WorkerWithPodInfoResponse struct {
	ID                string   `json:"id"`
	Endpoint          string   `json:"endpoint"`
	PodName           string   `json:"pod_name,omitempty"`
	Status            string   `json:"status"`
	Concurrency       int      `json:"concurrency"`
	CurrentJobs       int      `json:"current_jobs"`
	JobsInProgress    []string `json:"jobs_in_progress"`
	LastHeartbeat     string   `json:"last_heartbeat"`
	LastTaskTime      string   `json:"last_task_time,omitempty"`
	Version           string   `json:"version,omitempty"`
	RegisteredAt      string   `json:"registered_at"`
	PodPhase          string   `json:"podPhase,omitempty"`
	PodStatus         string   `json:"podStatus,omitempty"`
	PodReason         string   `json:"podReason,omitempty"`
	PodMessage        string   `json:"podMessage,omitempty"`
	PodIP             string   `json:"podIP,omitempty"`
	PodNodeName       string   `json:"podNodeName,omitempty"`
	PodCreatedAt      string   `json:"podCreatedAt,omitempty"`
	PodStartedAt      string   `json:"podStartedAt,omitempty"`
	PodRestartCount   int32    `json:"podRestartCount,omitempty"`
	DeletionTimestamp string   `json:"deletionTimestamp,omitempty"`
	TerminatedAt      string   `json:"terminatedAt,omitempty"`
	// Failure information
	FailureType       string `json:"failureType,omitempty"`
	FailureReason     string `json:"failureReason,omitempty"`
	FailureSuggestion string `json:"failureSuggestion,omitempty"`
	FailureOccurredAt string `json:"failureOccurredAt,omitempty"`
}

// WorkerListResponse represents the worker list response
type WorkerListResponse struct {
	Workers []*WorkerResponse `json:"workers"`
	Total   int               `json:"total"`
}

// FromWorker converts a Worker model to WorkerResponse
func FromWorker(worker *model.Worker) *WorkerResponse {
	if worker == nil {
		return nil
	}

	resp := &WorkerResponse{
		ID:             worker.ID,
		Endpoint:       worker.Endpoint,
		PodName:        worker.PodName,
		Status:         string(worker.Status),
		Concurrency:    worker.Concurrency,
		CurrentJobs:    worker.CurrentJobs,
		JobsInProgress: worker.JobsInProgress,
		Version:        worker.Version,
	}

	if !worker.LastHeartbeat.IsZero() {
		resp.LastHeartbeat = worker.LastHeartbeat.Format(time.RFC3339)
	}
	if !worker.LastTaskTime.IsZero() {
		resp.LastTaskTime = worker.LastTaskTime.Format(time.RFC3339)
	}
	if !worker.RegisteredAt.IsZero() {
		resp.RegisteredAt = worker.RegisteredAt.Format(time.RFC3339)
	}

	return resp
}

// FromWorkers converts a list of Worker models to WorkerResponse list
func FromWorkers(workers []*model.Worker) []*WorkerResponse {
	result := make([]*WorkerResponse, len(workers))
	for i, worker := range workers {
		result[i] = FromWorker(worker)
	}
	return result
}

// FromMySQLWorker converts a MySQL Worker model to WorkerWithPodInfoResponse
func FromMySQLWorker(worker *mysqlmodel.Worker) *WorkerWithPodInfoResponse {
	if worker == nil {
		return nil
	}

	// Parse jobs_in_progress JSON
	var jobsInProgress []string
	if worker.JobsInProgress != "" {
		json.Unmarshal([]byte(worker.JobsInProgress), &jobsInProgress)
	}
	if jobsInProgress == nil {
		jobsInProgress = []string{}
	}

	resp := &WorkerWithPodInfoResponse{
		ID:             worker.WorkerID,
		Endpoint:       worker.Endpoint,
		PodName:        worker.PodName,
		Status:         worker.Status,
		Concurrency:    worker.Concurrency,
		CurrentJobs:    worker.CurrentJobs,
		JobsInProgress: jobsInProgress,
		Version:        worker.Version,
		LastHeartbeat:  worker.LastHeartbeat.Format(time.RFC3339),
		RegisteredAt:   worker.CreatedAt.Format(time.RFC3339),
	}

	if worker.LastTaskTime != nil {
		resp.LastTaskTime = worker.LastTaskTime.Format(time.RFC3339)
	}
	if worker.PodStartedAt != nil {
		resp.PodStartedAt = worker.PodStartedAt.Format(time.RFC3339)
	}
	if worker.TerminatedAt != nil {
		resp.TerminatedAt = worker.TerminatedAt.Format(time.RFC3339)
	}

	// Failure information
	if worker.FailureType != "" {
		resp.FailureType = worker.FailureType
		resp.FailureReason = worker.FailureReason
		if worker.FailureOccurredAt != nil {
			resp.FailureOccurredAt = worker.FailureOccurredAt.Format(time.RFC3339)
		}
	}

	// Extract Pod info from runtime_state
	if rs := worker.RuntimeState; rs != nil {
		if v, ok := rs["phase"].(string); ok {
			resp.PodPhase = v
		}
		if v, ok := rs["status"].(string); ok {
			resp.PodStatus = v
		}
		if v, ok := rs["reason"].(string); ok {
			resp.PodReason = v
		}
		if v, ok := rs["message"].(string); ok {
			resp.PodMessage = v
		}
		if v, ok := rs["ip"].(string); ok {
			resp.PodIP = v
		}
		if v, ok := rs["nodeName"].(string); ok {
			resp.PodNodeName = v
		}
		if v, ok := rs["createdAt"].(string); ok {
			resp.PodCreatedAt = v
		}
		if v, ok := rs["startedAt"].(string); ok && resp.PodStartedAt == "" {
			resp.PodStartedAt = v
		}
	}

	return resp
}

// FromMySQLWorkers converts a list of MySQL Worker models to WorkerWithPodInfoResponse list
func FromMySQLWorkers(workers []*mysqlmodel.Worker) []*WorkerWithPodInfoResponse {
	result := make([]*WorkerWithPodInfoResponse, len(workers))
	for i, worker := range workers {
		result[i] = FromMySQLWorker(worker)
	}
	return result
}
