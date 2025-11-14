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
		event := &mysqlModel.TaskEvent{
			TaskID:        task.TaskID,
			Endpoint:      task.Endpoint,
			EventType:     string(eventType),
			EventTime:     time.Now(),
			WorkerID:      workerID,
			WorkerPodName: workerPodName,
			FromStatus:    task.Status,
			ErrorMessage:  errorMsg,
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

// Helper method to record TASK_ASSIGNED event
func (s *TaskService) recordTaskAssigned(ctx context.Context, task *mysqlModel.Task, workerID string, workerPodName string) {
	s.recordTaskEvent(ctx, task, mysqlModel.EventTaskAssigned, workerID, workerPodName, "")
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

// Helper method to record TASK_REQUEUED event
func (s *TaskService) recordTaskRequeued(ctx context.Context, task *mysqlModel.Task, reason string) {
	s.recordTaskEvent(ctx, task, mysqlModel.EventTaskRequeued, "", "", reason)
}

// Helper method to record TASK_TIMEOUT event
func (s *TaskService) recordTaskTimeout(ctx context.Context, task *mysqlModel.Task) {
	s.recordTaskEvent(ctx, task, mysqlModel.EventTaskTimeout, task.WorkerID, "", "task execution timeout")
}

// recordTaskEventOnly 只记录事件到 task_events 表，不更新 task.extend 字段
// 用于 extend 字段已经在其他地方更新的情况（如 AssignTasksToWorker 中）
func (s *TaskService) recordTaskEventOnly(
	ctx context.Context,
	task *mysqlModel.Task,
	eventType mysqlModel.TaskEventType,
	workerID string,
	workerPodName string,
	fromStatus string,
	errorMsg string,
) {
	// 只异步记录事件到 task_events 表
	go func() {
		event := &mysqlModel.TaskEvent{
			TaskID:        task.TaskID,
			Endpoint:      task.Endpoint,
			EventType:     string(eventType),
			EventTime:     time.Now(),
			WorkerID:      workerID,
			WorkerPodName: workerPodName,
			FromStatus:    fromStatus,
			ErrorMessage:  errorMsg,
		}

		if err := s.taskEventRepo.RecordEvent(context.Background(), event); err != nil {
			logger.ErrorCtx(context.Background(), "failed to record task event: %v", err)
		}
	}()
}

// recordTaskAssignedEventOnly 只记录 TASK_ASSIGNED 事件，不更新 extend
// 用于任务已经通过 AssignTasksToWorker 完成所有更新的情况
func (s *TaskService) recordTaskAssignedEventOnly(ctx context.Context, task *mysqlModel.Task, workerID string, workerPodName string) {
	s.recordTaskEventOnly(ctx, task, mysqlModel.EventTaskAssigned, workerID, workerPodName, "PENDING", "")
}
