package service

import (
	"context"
	"time"

	"waverless/pkg/logger"
	mysqlModel "waverless/pkg/store/mysql/model"
)

// recordTaskEvent records a task event and updates task.extend field
// This function handles both:
// 1. Recording detailed event to task_events table (async)
// 2. Updating task.extend field with execution summary
func (s *TaskService) recordTaskEvent(
	ctx context.Context,
	task *mysqlModel.Task,
	eventType mysqlModel.TaskEventType,
	workerID string,
	workerPodName string,
	errorMsg string,
) {
	// 1. Record detailed event to task_events table (async, non-blocking)
	go func() {
		now := time.Now()
		event := &mysqlModel.TaskEvent{
			TaskID:        task.TaskID,
			Endpoint:      task.Endpoint,
			EventType:     string(eventType),
			EventTime:     now,
			WorkerID:      workerID,
			WorkerPodName: workerPodName,
			FromStatus:    task.Status,
			ErrorMessage:  errorMsg,
		}

		// Fill queue_wait_ms for TASK_ASSIGNED event
		if eventType == mysqlModel.EventTaskAssigned {
			queueMs := int(now.Sub(task.CreatedAt).Milliseconds())
			event.QueueWaitMs = &queueMs
		}

		// Fill execution_duration_ms for completion events
		if eventType == mysqlModel.EventTaskCompleted || eventType == mysqlModel.EventTaskFailed || eventType == mysqlModel.EventTaskTimeout {
			if task.StartedAt != nil {
				execMs := int(now.Sub(*task.StartedAt).Milliseconds())
				event.ExecutionDurationMs = &execMs
			}
			totalMs := int(now.Sub(task.CreatedAt).Milliseconds())
			event.TotalDurationMs = &totalMs
		}

		if err := s.taskEventRepo.RecordEvent(context.Background(), event); err != nil {
			logger.ErrorCtx(context.Background(), "failed to record task event: %v", err)
		}
	}()

	// 2. Update task.extend field (synchronous)
	s.updateTaskExtend(task, eventType, workerID)
}

// updateTaskExtend updates task.extend field based on event type
func (s *TaskService) updateTaskExtend(
	task *mysqlModel.Task,
	eventType mysqlModel.TaskEventType,
	workerID string,
) {
	now := time.Now()

	switch eventType {
	case mysqlModel.EventTaskAssigned:
		// Worker pulled task - add new execution record
		task.AddExecutionRecord(workerID, now)

	case mysqlModel.EventTaskCompleted, mysqlModel.EventTaskFailed, mysqlModel.EventTaskTimeout, mysqlModel.EventTaskOrphaned:
		// Task finished (completed/failed/timeout/orphaned) - complete current execution
		task.CompleteCurrentExecution()
	}
}

// Helper method to record TASK_CREATED event
func (s *TaskService) recordTaskCreated(ctx context.Context, task *mysqlModel.Task) {
	s.recordTaskEvent(ctx, task, mysqlModel.EventTaskCreated, "", "", "")
}

// Helper method to record TASK_QUEUED event
func (s *TaskService) recordTaskQueued(ctx context.Context, task *mysqlModel.Task) {
	s.recordTaskEvent(ctx, task, mysqlModel.EventTaskQueued, "", "", "")
}

// Helper method to record TASK_COMPLETED event
func (s *TaskService) recordTaskCompleted(ctx context.Context, task *mysqlModel.Task, workerID string) {
	s.recordTaskEvent(ctx, task, mysqlModel.EventTaskCompleted, workerID, "", "")
}

// Helper method to record TASK_FAILED event
func (s *TaskService) recordTaskFailed(ctx context.Context, task *mysqlModel.Task, workerID string, errorMsg string) {
	s.recordTaskEvent(ctx, task, mysqlModel.EventTaskFailed, workerID, "", errorMsg)
}

// Helper method to record TASK_ORPHANED event
func (s *TaskService) recordTaskOrphaned(ctx context.Context, task *mysqlModel.Task) {
	s.recordTaskEvent(ctx, task, mysqlModel.EventTaskOrphaned, task.WorkerID, "", "worker lost connection")
}

// Helper method to record TASK_TIMEOUT event
func (s *TaskService) recordTaskTimeout(ctx context.Context, task *mysqlModel.Task) {
	s.recordTaskEvent(ctx, task, mysqlModel.EventTaskTimeout, task.WorkerID, "", "task execution timeout")
}

// recordTaskAssignedEventOnly records only the TASK_ASSIGNED event without updating extend
// Used when task has already been fully updated via AssignTasksToWorker
func (s *TaskService) recordTaskAssignedEventOnly(ctx context.Context, task *mysqlModel.Task, workerID string, workerPodName string) {
	go func() {
		now := time.Now()
		queueMs := int(now.Sub(task.CreatedAt).Milliseconds())
		event := &mysqlModel.TaskEvent{
			TaskID:        task.TaskID,
			Endpoint:      task.Endpoint,
			EventType:     string(mysqlModel.EventTaskAssigned),
			EventTime:     now,
			WorkerID:      workerID,
			WorkerPodName: workerPodName,
			FromStatus:    "PENDING",
			QueueWaitMs:   &queueMs,
		}
		if err := s.taskEventRepo.RecordEvent(context.Background(), event); err != nil {
			logger.ErrorCtx(context.Background(), "failed to record task event: %v", err)
		}
	}()
}
