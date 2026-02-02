package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"waverless/internal/model"
	"waverless/pkg/config"
	"waverless/pkg/constants"
	"waverless/pkg/interfaces"
	"waverless/pkg/logger"
	"waverless/pkg/store/mysql"
	mysqlModel "waverless/pkg/store/mysql/model"
)

// WorkerService Worker service (MySQL-based)
type WorkerService struct {
	workerRepo         *mysql.WorkerRepository
	taskRepo           *mysql.TaskRepository
	taskService        *TaskService
	workerEventService *WorkerEventService
	deployProvider     interfaces.DeploymentProvider
}

// NewWorkerService creates a new Worker service
func NewWorkerService(workerRepo *mysql.WorkerRepository, taskRepo *mysql.TaskRepository, deployProvider interfaces.DeploymentProvider) *WorkerService {
	return &WorkerService{
		workerRepo:     workerRepo,
		taskRepo:       taskRepo,
		deployProvider: deployProvider,
	}
}

// SetWorkerEventService sets the worker event service
func (s *WorkerService) SetWorkerEventService(svc *WorkerEventService) {
	s.workerEventService = svc
}

// SetTaskService sets the task service (for circular dependency resolution)
func (s *WorkerService) SetTaskService(taskService *TaskService) {
	s.taskService = taskService
}

// HandleHeartbeat handles heartbeat requests
func (s *WorkerService) HandleHeartbeat(ctx context.Context, req *model.HeartbeatRequest, endpoint string) error {
	if endpoint == "" {
		endpoint = "default"
	}

	// Get existing worker to check previous status and job count
	existingWorker, _ := s.workerRepo.Get(ctx, req.WorkerID)
	previousJobs := 0
	wasStarting := false
	var coldStartMs *int64
	if existingWorker != nil {
		previousJobs = existingWorker.CurrentJobs
		wasStarting = existingWorker.Status == constants.WorkerStatusStarting.String()
		coldStartMs = existingWorker.ColdStartDurationMs
	}

	// Update heartbeat in MySQL
	if err := s.workerRepo.UpdateHeartbeat(ctx, req.WorkerID, endpoint, req.JobsInProgress, len(req.JobsInProgress), req.Version); err != nil {
		return fmt.Errorf("failed to update heartbeat: %w", err)
	}

	// Record WORKER_REGISTERED event when worker transitions from STARTING to ONLINE
	if wasStarting && s.workerEventService != nil {
		podName := req.WorkerID
		if existingWorker != nil {
			podName = existingWorker.PodName
		}
		s.workerEventService.RecordWorkerRegistered(ctx, req.WorkerID, endpoint, podName, coldStartMs)
	}

	// Update LastTaskTime when worker becomes idle (completed all tasks)
	currentJobs := len(req.JobsInProgress)
	if previousJobs > 0 && currentJobs == 0 {
		s.workerRepo.UpdateLastTaskTime(ctx, req.WorkerID)
	}

	logger.DebugCtx(ctx, "heartbeat received, worker_id: %s, endpoint: %s, jobs_count: %d, version: %s",
		req.WorkerID, endpoint, currentJobs, req.Version)

	return nil
}

// PullJobs pulls tasks (by endpoint)
func (s *WorkerService) PullJobs(ctx context.Context, req *model.JobPullRequest, endpoint string) (*model.JobPullResponse, error) {
	if endpoint == "" {
		endpoint = "default"
	}

	// Update heartbeat (preserve existing version since PullJobs doesn't have version)
	if err := s.workerRepo.UpdateHeartbeat(ctx, req.WorkerID, endpoint, req.JobsInProgress, req.JobsInProgressCount, ""); err != nil {
		logger.ErrorCtx(ctx, "failed to update heartbeat: %v", err)
	}

	// Get worker information
	worker, err := s.workerRepo.Get(ctx, req.WorkerID)
	if err != nil {
		// Worker should exist after heartbeat update
		return nil, fmt.Errorf("failed to get worker: %w", err)
	}

	// Check if worker is draining
	if worker.Status == constants.WorkerStatusDraining.String() {
		logger.InfoCtx(ctx, "⛔ Worker is draining, not pulling new tasks, worker_id: %s", req.WorkerID)
		return &model.JobPullResponse{Jobs: []model.JobInfo{}}, nil
	}

	// Safety check: verify pod is not terminating
	if s.deployProvider != nil {
		isTerminating, err := s.deployProvider.IsPodTerminating(ctx, worker.PodName)
		if err == nil && isTerminating {
			logger.WarnCtx(ctx, "🛡️ Pod %s is terminating, marking as DRAINING", worker.PodName)
			s.workerRepo.UpdateStatus(ctx, worker.WorkerID, constants.WorkerStatusDraining.String())
			return &model.JobPullResponse{Jobs: []model.JobInfo{}}, nil
		}
	}

	// Calculate available slots
	batchSize := req.BatchSize
	if batchSize <= 0 {
		batchSize = 1
	}

	// concurrency := worker.Concurrency
	// if concurrency <= 0 {
	// 	concurrency = config.GlobalConfig.Worker.DefaultConcurrency
	// }
	// Calculate available slots: use JobsInProgressCount if JobsInProgress is empty
	// currentJobs := len(req.JobsInProgress)
	// if currentJobs == 0 && req.JobsInProgressCount > 0 {
	// 	currentJobs = req.JobsInProgressCount
	// }

	// availableSlots := concurrency - currentJobs
	// if availableSlots <= 0 {
	// 	return &model.JobPullResponse{Jobs: []model.JobInfo{}}, nil
	// }

	// if batchSize > availableSlots {
	// 	batchSize = availableSlots
	// }

	// Calculate idle duration before pulling tasks (if worker was idle)
	var idleDurationMs int64
	if worker.LastTaskTime != nil && len(req.JobsInProgress) == 0 {
		idleDurationMs = time.Since(*worker.LastTaskTime).Milliseconds()
	}

	// Select and assign tasks atomically in one transaction
	assignedTasks, err := s.taskRepo.SelectAndAssignTasks(ctx, endpoint, batchSize, req.WorkerID)
	if err != nil {
		return nil, fmt.Errorf("failed to select and assign tasks: %w", err)
	}

	if len(assignedTasks) == 0 {
		return &model.JobPullResponse{Jobs: []model.JobInfo{}}, nil
	}

	// Record WORKER_TASK_PULLED event (once per pull, with first task ID)
	if s.workerEventService != nil && idleDurationMs > 0 {
		s.workerEventService.RecordWorkerTaskPulled(ctx, req.WorkerID, endpoint, worker.PodName, assignedTasks[0].TaskID, idleDurationMs)
	}

	// Convert to response
	jobs := make([]model.JobInfo, 0, len(assignedTasks))
	for _, mysqlTask := range assignedTasks {
		// Record event
		if s.taskService != nil {
			s.taskService.recordTaskAssignedEventOnly(ctx, mysqlTask, req.WorkerID, worker.PodName)
		}

		task := mysql.ToTaskDomain(mysqlTask)
		jobs = append(jobs, model.JobInfo{
			ID:    task.ID,
			Input: task.Input,
		})
	}

	// Batch update statistics (once for all tasks, not per task)
	if s.taskService != nil && s.taskService.statisticsService != nil && len(assignedTasks) > 0 {
		go s.taskService.statisticsService.UpdateStatisticsOnTaskStatusChangeBatch(
			context.Background(), endpoint, "PENDING", "IN_PROGRESS", len(assignedTasks))
	}

	logger.InfoCtx(ctx, "jobs pulled, worker_id: %s, endpoint: %s, count: %d, task_ids: %v", req.WorkerID, endpoint, len(jobs), getTaskIDs(assignedTasks))
	return &model.JobPullResponse{Jobs: jobs}, nil
}

// ListWorkers lists all workers (optionally filtered by endpoint)
func (s *WorkerService) ListWorkers(ctx context.Context, endpoint string) ([]*model.Worker, error) {
	var mysqlWorkers []*mysqlModel.Worker
	var err error

	if endpoint != "" {
		mysqlWorkers, err = s.workerRepo.GetByEndpoint(ctx, endpoint)
	} else {
		mysqlWorkers, err = s.workerRepo.GetAll(ctx)
	}

	if err != nil {
		return nil, err
	}

	// Convert to domain model
	workers := make([]*model.Worker, 0, len(mysqlWorkers))
	for _, mw := range mysqlWorkers {
		workers = append(workers, s.toDomainWorker(mw))
	}
	return workers, nil
}

// ListWorkersWithPodInfo returns workers with pod runtime info (for API responses)
func (s *WorkerService) ListWorkersWithPodInfo(ctx context.Context, endpoint string) ([]*mysqlModel.Worker, error) {
	if endpoint != "" {
		return s.workerRepo.GetByEndpoint(ctx, endpoint)
	}
	return s.workerRepo.GetAll(ctx)
}

// ListWorkersForSync returns workers for Portal sync (includes recently terminated OFFLINE workers)
func (s *WorkerService) ListWorkersForSync(ctx context.Context, endpoint string) ([]*mysqlModel.Worker, error) {
	return s.workerRepo.GetByEndpointForSync(ctx, endpoint)
}

// GetWorker gets a worker by worker ID
func (s *WorkerService) GetWorker(ctx context.Context, workerID string) (*model.Worker, error) {
	mw, err := s.workerRepo.Get(ctx, workerID)
	if err != nil {
		return nil, err
	}
	return s.toDomainWorker(mw), nil
}

// GetWorkerByID gets a worker by database ID (regardless of status)
func (s *WorkerService) GetWorkerByID(ctx context.Context, id int64) (*mysqlModel.Worker, error) {
	return s.workerRepo.GetByID(ctx, id)
}
func (s *WorkerService) GetWorkerByWorkerID(ctx context.Context, workerID string) (*mysqlModel.Worker, error) {
	return s.workerRepo.Get(ctx, workerID)
}

// GetWorkerByPodName finds a worker by its pod name
func (s *WorkerService) GetWorkerByPodName(ctx context.Context, endpoint, podName string) (*model.Worker, error) {
	mw, err := s.workerRepo.GetByPodName(ctx, endpoint, podName)
	if err != nil {
		return nil, err
	}
	return s.toDomainWorker(mw), nil
}

// UpdateWorkerStatus updates the status of a worker
func (s *WorkerService) UpdateWorkerStatus(ctx context.Context, workerID string, status model.WorkerStatus) error {
	return s.workerRepo.UpdateStatus(ctx, workerID, string(status))
}

// CleanupOfflineWorkers cleans up offline Workers and reclaims their tasks
func (s *WorkerService) CleanupOfflineWorkers(ctx context.Context) error {
	timeout := time.Duration(config.GlobalConfig.Worker.HeartbeatTimeout) * time.Second
	threshold := time.Now().Add(-timeout)

	// Get workers that will be marked offline (for event recording)
	var staleWorkers []*mysqlModel.Worker
	if s.workerEventService != nil {
		staleWorkers, _ = s.workerRepo.GetStaleWorkers(ctx, threshold)
	}

	// Mark stale workers as offline
	affected, err := s.workerRepo.MarkOffline(ctx, timeout)
	if err != nil {
		return err
	}
	if affected > 0 {
		logger.InfoCtx(ctx, "marked %d stale workers as OFFLINE", affected)
		// Record WORKER_OFFLINE events
		if s.workerEventService != nil {
			for _, w := range staleWorkers {
				s.workerEventService.RecordWorkerOffline(ctx, w.WorkerID, w.Endpoint, w.PodName)
			}
		}
	}

	// Get all workers to check for task reclamation
	workers, err := s.workerRepo.GetAll(ctx)
	if err != nil {
		return err
	}

	now := time.Now()
	for _, mw := range workers {
		if now.Sub(mw.LastHeartbeat) > timeout {
			worker := s.toDomainWorker(mw)
			if err := s.reclaimWorkerTasks(ctx, worker); err != nil {
				logger.ErrorCtx(ctx, "failed to reclaim tasks for worker %s: %v", mw.WorkerID, err)
			}
		}
	}

	return nil
}

// RecordTaskCompletion records task completion stats for a worker
func (s *WorkerService) RecordTaskCompletion(ctx context.Context, workerID, endpoint, taskID string, completed bool, executionTimeMs int64, completedAt time.Time) {
	if err := s.workerRepo.IncrementTaskStatsAt(ctx, workerID, completed, executionTimeMs, completedAt); err != nil {
		logger.WarnCtx(ctx, "failed to record task completion stats for worker %s: %v", workerID, err)
	}
	// Record WORKER_TASK_COMPLETED event (for both success and failure)
	if s.workerEventService != nil {
		s.workerEventService.RecordWorkerTaskCompleted(ctx, workerID, endpoint, taskID, executionTimeMs)
	}
}

// toDomainWorker converts MySQL model to domain model
func (s *WorkerService) toDomainWorker(mw *mysqlModel.Worker) *model.Worker {
	var jobsInProgress []string
	if mw.JobsInProgress != "" {
		json.Unmarshal([]byte(mw.JobsInProgress), &jobsInProgress)
	}

	var lastTaskTime time.Time
	if mw.LastTaskTime != nil {
		lastTaskTime = *mw.LastTaskTime
	}

	return &model.Worker{
		ID:             mw.WorkerID,
		Endpoint:       mw.Endpoint,
		Status:         model.WorkerStatus(mw.Status),
		Concurrency:    mw.Concurrency,
		CurrentJobs:    mw.CurrentJobs,
		JobsInProgress: jobsInProgress,
		LastHeartbeat:  mw.LastHeartbeat,
		LastTaskTime:   lastTaskTime,
		Version:        mw.Version,
		RegisteredAt:   mw.CreatedAt,
		PodName:        mw.PodName,
	}
}

// reclaimWorkerTasks reclaims all tasks assigned to an offline worker
func (s *WorkerService) reclaimWorkerTasks(ctx context.Context, worker *model.Worker) error {
	tasks, err := s.taskRepo.GetTasksByWorker(ctx, worker.ID)
	if err != nil {
		return fmt.Errorf("failed to get assigned tasks: %w", err)
	}

	if len(tasks) == 0 {
		return nil
	}

	logger.InfoCtx(ctx, "reclaiming tasks from offline worker, worker_id: %s, task_count: %d", worker.ID, len(tasks))

	gracePeriod := time.Duration(config.GlobalConfig.Worker.HeartbeatTimeout*2) * time.Second
	now := time.Now()
	reclaimedCount := 0

	for _, mysqlTask := range tasks {
		if mysqlTask.Status != string(model.TaskStatusInProgress) {
			continue
		}

		// Grace period check
		if mysqlTask.StartedAt != nil {
			if now.Sub(*mysqlTask.StartedAt) < gracePeriod {
				continue
			}
		}

		// Reset task to pending
		if err := s.taskRepo.UpdateFields(ctx, mysqlTask.TaskID, map[string]interface{}{
			"status":       string(model.TaskStatusPending),
			"worker_id":    "",
			"started_at":   nil,
			"completed_at": nil,
			"updated_at":   now,
		}); err != nil {
			logger.ErrorCtx(ctx, "failed to reclaim task %s: %v", mysqlTask.TaskID, err)
			continue
		}

		if s.taskService != nil && s.taskService.statisticsService != nil {
			go s.taskService.statisticsService.UpdateStatisticsOnTaskStatusChange(
				context.Background(), mysqlTask.Endpoint, "IN_PROGRESS", "PENDING")
		}

		reclaimedCount++
		logger.InfoCtx(ctx, "task reclaimed, task_id: %s", mysqlTask.TaskID)
	}

	logger.InfoCtx(ctx, "task reclamation completed, worker_id: %s, reclaimed: %d", worker.ID, reclaimedCount)
	return nil
}

// GetAllWorkers returns all active workers
func (s *WorkerService) GetAllWorkers(ctx context.Context) ([]*mysqlModel.Worker, error) {
	return s.workerRepo.GetAll(ctx)
}

func getTaskIDs(tasks []*mysql.Task) []string {
	ids := make([]string, len(tasks))
	for i, t := range tasks {
		ids[i] = t.TaskID
	}
	return ids
}
