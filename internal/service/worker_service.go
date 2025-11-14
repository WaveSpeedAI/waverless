package service

import (
	"context"
	"fmt"
	"time"

	"waverless/internal/model"
	"waverless/pkg/config"
	"waverless/pkg/deploy/k8s"
	"waverless/pkg/logger"
	"waverless/pkg/store/mysql"
	redisstore "waverless/pkg/store/redis"
)

// WorkerService Worker service
type WorkerService struct {
	workerRepo        *redisstore.WorkerRepository
	taskRepo          *mysql.TaskRepository
	taskService       *TaskService               // For task event recording
	k8sDeployProvider *k8s.K8sDeploymentProvider // For draining check
}

// NewWorkerService creates a new Worker service
func NewWorkerService(workerRepo *redisstore.WorkerRepository, taskRepo *mysql.TaskRepository, k8sDeployProvider *k8s.K8sDeploymentProvider) *WorkerService {
	return &WorkerService{
		workerRepo:        workerRepo,
		taskRepo:          taskRepo,
		k8sDeployProvider: k8sDeployProvider,
	}
}

// SetTaskService sets the task service (for circular dependency resolution)
func (s *WorkerService) SetTaskService(taskService *TaskService) {
	s.taskService = taskService
}

// HandleHeartbeat handles heartbeat requests
func (s *WorkerService) HandleHeartbeat(ctx context.Context, req *model.HeartbeatRequest, endpoint string) error {
	if err := s.workerRepo.UpdateHeartbeat(ctx, req.WorkerID, endpoint, req.JobsInProgress); err != nil {
		return fmt.Errorf("failed to update heartbeat: %w", err)
	}

	logger.DebugCtx(ctx, "heartbeat received, worker_id: %s, endpoint: %s, jobs_count: %d",
		req.WorkerID, endpoint, len(req.JobsInProgress))

	return nil
}

// PullJobs pulls tasks (by endpoint)
func (s *WorkerService) PullJobs(ctx context.Context, req *model.JobPullRequest, endpoint string) (*model.JobPullResponse, error) {
	// Process endpoint
	if endpoint == "" {
		endpoint = "default"
	}

	// Update heartbeat
	if err := s.workerRepo.UpdateHeartbeat(ctx, req.WorkerID, endpoint, req.JobsInProgress); err != nil {
		logger.ErrorCtx(ctx, "failed to update heartbeat: %v", err)
	}

	// Get worker information
	worker, err := s.workerRepo.Get(ctx, req.WorkerID)
	if err != nil {
		// Worker doesn't exist, create default worker
		worker = &model.Worker{
			ID:             req.WorkerID,
			Endpoint:       endpoint,
			Status:         model.WorkerStatusOnline,
			Concurrency:    config.GlobalConfig.Worker.DefaultConcurrency,
			JobsInProgress: req.JobsInProgress,
			RegisteredAt:   time.Now(),
			LastHeartbeat:  time.Now(),
			PodName:        req.WorkerID, // Worker ID == Pod Name (from RUNPOD_POD_ID env)
		}
		if err := s.workerRepo.Save(ctx, worker); err != nil {
			return nil, fmt.Errorf("failed to save worker: %w", err)
		}
	}

	// Use Worker's endpoint (allow dynamic update)
	if worker.Endpoint == "" {
		worker.Endpoint = endpoint
	}

	// Ensure PodName is set (for existing workers registered before this fix)
	if worker.PodName == "" {
		worker.PodName = req.WorkerID // Worker ID == Pod Name
		if err := s.workerRepo.Save(ctx, worker); err != nil {
			logger.WarnCtx(ctx, "failed to update worker pod name: %v", err)
		}
	}

	// Check if worker is draining (pod marked for deletion)
	// When a pod is marked for deletion, server marks the worker as draining
	// Draining workers should not receive new tasks
	if worker.Status == model.WorkerStatusDraining {
		logger.InfoCtx(ctx, "⛔ Worker is draining, not pulling new tasks, worker_id: %s, endpoint: %s",
			req.WorkerID, worker.Endpoint)
		return &model.JobPullResponse{Jobs: []model.JobInfo{}}, nil
	}

	// 🛡️ Safety Net: Check Pod status in real-time (in case Pod Watcher callback failed)
	// This prevents Terminating pods from pulling tasks even if Worker.Status is not updated
	if s.k8sDeployProvider != nil && worker.PodName != "" {
		isTerminating, err := s.k8sDeployProvider.IsPodTerminating(ctx, worker.PodName)
		if err != nil {
			logger.WarnCtx(ctx, "Failed to check pod status for worker %s: %v", req.WorkerID, err)
			// Continue anyway - don't block task pulling on K8s API errors
		} else if isTerminating {
			// Pod is terminating but Worker status not updated yet - mark it now and reject task
			logger.WarnCtx(ctx, "🛡️ Safety Net: Pod %s is terminating but Worker status is %s, marking as DRAINING now",
				worker.PodName, worker.Status)
			if err := s.UpdateWorkerStatus(ctx, worker.ID, model.WorkerStatusDraining); err != nil {
				logger.ErrorCtx(ctx, "Failed to mark worker %s as draining: %v", worker.ID, err)
			}
			return &model.JobPullResponse{Jobs: []model.JobInfo{}}, nil
		}
	}

	// Calculate number of tasks that can be pulled
	batchSize := req.BatchSize
	if batchSize <= 0 {
		batchSize = 1
	}

	availableSlots := worker.Concurrency - len(req.JobsInProgress)
	if availableSlots <= 0 {
		logger.DebugCtx(ctx, "worker busy, no available slots, worker_id: %s, endpoint: %s, concurrency: %d, current_jobs: %d",
			req.WorkerID, worker.Endpoint, worker.Concurrency, len(req.JobsInProgress))
		return &model.JobPullResponse{Jobs: []model.JobInfo{}}, nil
	}

	if batchSize > availableSlots {
		batchSize = availableSlots
	}

	// Step 1: Query and lock PENDING tasks (without updating status)
	logger.DebugCtx(ctx, "🔍 Step 1: Selecting PENDING tasks from MySQL, worker_id: %s, endpoint: %s, batch_size: %d",
		req.WorkerID, worker.Endpoint, batchSize)

	candidateTasks, err := s.taskRepo.SelectPendingTasksForUpdate(ctx, worker.Endpoint, batchSize)
	if err != nil {
		logger.ErrorCtx(ctx, "❌ Failed to select pending tasks: %v", err)
		return nil, fmt.Errorf("failed to select pending tasks: %w", err)
	}

	logger.DebugCtx(ctx, "📦 Selected and locked %d candidate tasks", len(candidateTasks))

	if len(candidateTasks) == 0 {
		logger.DebugCtx(ctx, "⚠️  No pending tasks available, worker_id: %s, endpoint: %s",
			req.WorkerID, worker.Endpoint)
		return &model.JobPullResponse{Jobs: []model.JobInfo{}}, nil
	}

	// Step 2: CRITICAL - Double-check worker status BEFORE updating tasks
	// This prevents assigning tasks to DRAINING workers (avoid rollback after task assignment)
	logger.InfoCtx(ctx, "🔍 Step 2: Double-checking worker status before assignment")
	workerBeforeAssignment, err := s.workerRepo.Get(ctx, req.WorkerID)
	if err == nil && workerBeforeAssignment.Status == model.WorkerStatusDraining {
		logger.WarnCtx(ctx, "🔴 Worker %s is DRAINING, skipping task assignment for %d tasks",
			req.WorkerID, len(candidateTasks))
		// Tasks remain in PENDING status, no need to revert
		return &model.JobPullResponse{Jobs: []model.JobInfo{}}, nil
	}

	// Step 3: Use CAS atomic update for task status (only PENDING status will be updated)
	logger.InfoCtx(ctx, "🔄 Step 3: Assigning %d tasks to worker using CAS update", len(candidateTasks))

	taskIDs := make([]string, len(candidateTasks))
	for i, task := range candidateTasks {
		taskIDs[i] = task.TaskID
	}

	assignedTasks, err := s.taskRepo.AssignTasksToWorker(ctx, taskIDs, req.WorkerID)
	if err != nil {
		logger.ErrorCtx(ctx, "❌ Failed to assign tasks to worker: %v", err)
		return nil, fmt.Errorf("failed to assign tasks: %w", err)
	}

	logger.InfoCtx(ctx, "✅ Successfully assigned %d/%d tasks to worker (CAS succeeded)",
		len(assignedTasks), len(candidateTasks))

	if len(assignedTasks) == 0 {
		logger.InfoCtx(ctx, "⚠️  No tasks were assigned (all CAS failed), worker_id: %s", req.WorkerID)
		return &model.JobPullResponse{Jobs: []model.JobInfo{}}, nil
	}

	// Step 4: Convert to domain model and return
	logger.InfoCtx(ctx, "🔄 Step 4: Converting %d assigned tasks to job responses", len(assignedTasks))
	jobs := make([]model.JobInfo, 0, len(assignedTasks))

	for i, mysqlTask := range assignedTasks {
		taskID := mysqlTask.TaskID
		logger.InfoCtx(ctx, "📝 Processing task %d/%d: %s", i+1, len(assignedTasks), taskID)

		// AssignTasksToWorker has completed all database updates:
		// - status: PENDING -> IN_PROGRESS (CAS)
		// - worker_id: set to current worker
		// - started_at: set to current time
		// - extend: added new ExecutionRecord

		// Asynchronously record TASK_ASSIGNED event to task_events table (without updating extend)
		if s.taskService != nil {
			s.taskService.recordTaskAssignedEventOnly(ctx, mysqlTask, req.WorkerID, worker.PodName)

			// Update statistics: PENDING -> IN_PROGRESS
			if s.taskService.statisticsService != nil {
				go s.taskService.statisticsService.UpdateStatisticsOnTaskStatusChange(
					context.Background(), mysqlTask.Endpoint, "PENDING", "IN_PROGRESS")
			}
		}

		// Convert to domain model
		task := mysql.ToTaskDomain(mysqlTask)

		jobs = append(jobs, model.JobInfo{
			ID:    task.ID,
			Input: task.Input,
		})

		logger.InfoCtx(ctx, "✅ Task assigned successfully, task_id: %s", taskID)
	}

	logger.InfoCtx(ctx, "jobs pulled, worker_id: %s, endpoint: %s, count: %d",
		req.WorkerID, worker.Endpoint, len(jobs))

	return &model.JobPullResponse{Jobs: jobs}, nil
}

// GetWorker gets Worker information
func (s *WorkerService) GetWorker(ctx context.Context, workerID string) (*model.Worker, error) {
	return s.workerRepo.Get(ctx, workerID)
}

// GetWorkerList gets Worker list
func (s *WorkerService) GetWorkerList(ctx context.Context) ([]*model.Worker, error) {
	return s.workerRepo.GetAll(ctx)
}

// GetOnlineWorkerCount gets online Worker count
func (s *WorkerService) GetOnlineWorkerCount(ctx context.Context) (int, error) {
	return s.workerRepo.GetOnlineWorkerCount(ctx)
}

// SaveWorker saves worker information
func (s *WorkerService) SaveWorker(ctx context.Context, worker *model.Worker) error {
	return s.workerRepo.Save(ctx, worker)
}

// ListWorkers lists all workers (optionally filtered by endpoint)
func (s *WorkerService) ListWorkers(ctx context.Context, endpoint string) ([]*model.Worker, error) {
	workers, err := s.workerRepo.GetAll(ctx)
	if err != nil {
		return nil, err
	}

	// Filter by endpoint if specified
	if endpoint == "" {
		return workers, nil
	}

	filtered := make([]*model.Worker, 0, len(workers))
	for _, worker := range workers {
		if worker.Endpoint == endpoint {
			filtered = append(filtered, worker)
		}
	}
	return filtered, nil
}

// UpdateWorkerHeartbeat updates worker heartbeat
func (s *WorkerService) UpdateWorkerHeartbeat(ctx context.Context, workerID string, heartbeat time.Time) error {
	worker, err := s.workerRepo.Get(ctx, workerID)
	if err != nil {
		return s.workerRepo.UpdateHeartbeat(ctx, workerID, "", []string{})
	}
	return s.workerRepo.UpdateHeartbeat(ctx, workerID, worker.Endpoint, worker.JobsInProgress)
}

// DeleteWorker deletes worker
func (s *WorkerService) DeleteWorker(ctx context.Context, workerID string) error {
	return s.workerRepo.Delete(ctx, workerID)
}

// GetWorkerByPodName finds a worker by its pod name for a given endpoint
// This is used to identify workers when pods are marked for deletion
func (s *WorkerService) GetWorkerByPodName(ctx context.Context, endpoint, podName string) (*model.Worker, error) {
	workers, err := s.workerRepo.GetByEndpoint(ctx, endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to get workers for endpoint: %w", err)
	}

	for _, w := range workers {
		if w.PodName == podName {
			return w, nil
		}
	}

	return nil, fmt.Errorf("worker not found for pod %s in endpoint %s", podName, endpoint)
}

// UpdateWorkerStatus updates the status of a worker
// This is used to mark workers as draining when their pods are terminating
func (s *WorkerService) UpdateWorkerStatus(ctx context.Context, workerID string, status model.WorkerStatus) error {
	worker, err := s.workerRepo.Get(ctx, workerID)
	if err != nil {
		return fmt.Errorf("failed to get worker: %w", err)
	}

	worker.Status = status
	return s.workerRepo.Save(ctx, worker)
}

// CleanupOfflineWorkers cleans up offline Workers and reclaims their tasks
func (s *WorkerService) CleanupOfflineWorkers(ctx context.Context) error {
	workers, err := s.workerRepo.GetAll(ctx)
	if err != nil {
		return err
	}

	timeout := time.Duration(config.GlobalConfig.Worker.HeartbeatTimeout) * time.Second
	now := time.Now()

	for _, worker := range workers {
		if now.Sub(worker.LastHeartbeat) > timeout {
			logger.InfoCtx(ctx, "detected offline worker, worker_id: %s, endpoint: %s, last_heartbeat: %v ago",
				worker.ID, worker.Endpoint, now.Sub(worker.LastHeartbeat))

			// Reclaim tasks assigned to this worker
			if err := s.reclaimWorkerTasks(ctx, worker); err != nil {
				logger.ErrorCtx(ctx, "failed to reclaim tasks for worker, worker_id: %s, error: %v", worker.ID, err)
			}

			// Delete the worker
			if err := s.workerRepo.Delete(ctx, worker.ID); err != nil {
				logger.ErrorCtx(ctx, "failed to delete worker, worker_id: %s, error: %v", worker.ID, err)
			}
		}
	}

	return nil
}

// reclaimWorkerTasks reclaims all tasks assigned to an offline worker
func (s *WorkerService) reclaimWorkerTasks(ctx context.Context, worker *model.Worker) error {
	// Get all IN_PROGRESS tasks assigned to this worker from MySQL
	tasks, err := s.taskRepo.GetTasksByWorker(ctx, worker.ID)
	if err != nil {
		return fmt.Errorf("failed to get assigned tasks from MySQL: %w", err)
	}

	if len(tasks) == 0 {
		return nil
	}

	logger.InfoCtx(ctx, "reclaiming tasks from offline worker, worker_id: %s, task_count: %d",
		worker.ID, len(tasks))

	reclaimedCount := 0
	// Grace period: 2x heartbeat timeout to avoid premature reclamation
	// This prevents reclaiming tasks from workers that are slow but still alive
	gracePeriod := time.Duration(config.GlobalConfig.Worker.HeartbeatTimeout*2) * time.Second
	now := time.Now()

	for _, mysqlTask := range tasks {
		taskID := mysqlTask.TaskID

		// Only reclaim tasks that are IN_PROGRESS (query already filtered this)
		if mysqlTask.Status != string(model.TaskStatusInProgress) {
			continue
		}

		// Grace period check: Only reclaim if task has been running longer than grace period
		// This prevents reclaiming tasks from workers experiencing temporary network issues
		if mysqlTask.StartedAt != nil {
			taskRunningTime := now.Sub(*mysqlTask.StartedAt)
			if taskRunningTime < gracePeriod {
				logger.InfoCtx(ctx, "skipping task reclaim (within grace period), task_id: %s, running_time: %v, grace_period: %v",
					taskID, taskRunningTime, gracePeriod)
				continue
			}
			logger.InfoCtx(ctx, "reclaiming task (exceeded grace period), task_id: %s, running_time: %v, grace_period: %v",
				taskID, taskRunningTime, gracePeriod)
		}

		// Reset task status to pending
		now := time.Now()
		mysqlTask.Status = string(model.TaskStatusPending)
		mysqlTask.WorkerID = ""
		mysqlTask.StartedAt = nil
		mysqlTask.CompletedAt = nil
		mysqlTask.UpdatedAt = now

		if err := s.taskRepo.UpdateFields(ctx, mysqlTask.TaskID, map[string]interface{}{
			"status":       mysqlTask.Status,
			"worker_id":    mysqlTask.WorkerID,
			"started_at":   mysqlTask.StartedAt,
			"completed_at": mysqlTask.CompletedAt,
			"updated_at":   mysqlTask.UpdatedAt,
		}); err != nil {
			logger.ErrorCtx(ctx, "failed to update task status during reclaim, task_id: %s, error: %v", taskID, err)
			continue
		}

		// Update statistics: IN_PROGRESS -> PENDING
		if s.taskService != nil && s.taskService.statisticsService != nil {
			go s.taskService.statisticsService.UpdateStatisticsOnTaskStatusChange(
				context.Background(), mysqlTask.Endpoint, "IN_PROGRESS", "PENDING")
		}

		// Task status changed to PENDING, worker_id cleared, worker will automatically pull from MySQL
		reclaimedCount++
		logger.InfoCtx(ctx, "task reclaimed to PENDING status, task_id: %s, endpoint: %s", taskID, mysqlTask.Endpoint)
	}

	logger.InfoCtx(ctx, "task reclamation completed, worker_id: %s, reclaimed: %d, total: %d",
		worker.ID, reclaimedCount, len(tasks))

	return nil
}
