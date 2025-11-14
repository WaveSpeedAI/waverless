-- ============================================================================
-- Migration: Add Task Tracking System
-- Date: 2025-11-10
-- Description:
--   1. Create task_events table for detailed event tracking
--   2. Add extend JSON field to tasks table for execution history summary
-- ============================================================================

-- ============================================================================
-- Table: task_events
-- Description: Detailed task event log for auditing and debugging
-- ============================================================================
CREATE TABLE IF NOT EXISTS `task_events` (
    `id` BIGINT NOT NULL AUTO_INCREMENT,
    `event_id` VARCHAR(255) NOT NULL COMMENT 'Event unique ID',
    `task_id` VARCHAR(255) NOT NULL COMMENT 'Task ID (foreign key to tasks.task_id)',
    `endpoint` VARCHAR(255) NOT NULL COMMENT 'Endpoint name',

    -- Event information
    `event_type` VARCHAR(50) NOT NULL COMMENT 'Event type: TASK_CREATED, TASK_ASSIGNED, TASK_COMPLETED, etc.',
    `event_time` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT 'Event timestamp',

    -- Worker information
    `worker_id` VARCHAR(255) NULL COMMENT 'Worker ID',
    `worker_pod_name` VARCHAR(255) NULL COMMENT 'Worker Pod name in Kubernetes',

    -- Status transition
    `from_status` VARCHAR(50) NULL COMMENT 'Original status',
    `to_status` VARCHAR(50) NULL COMMENT 'New status after this event',

    -- Error information (if applicable)
    `error_message` TEXT NULL COMMENT 'Error message if event is failure-related',
    `error_type` VARCHAR(100) NULL COMMENT 'Error type classification',

    -- Retry information
    `retry_count` INT NOT NULL DEFAULT 0 COMMENT 'Retry count at the time of this event',

    -- Additional metadata (flexible JSON field)
    `metadata` JSON NULL COMMENT 'Additional event metadata',

    PRIMARY KEY (`id`),
    UNIQUE INDEX `idx_event_id_unique` (`event_id`),
    INDEX `idx_task_id_event_time` (`task_id`, `event_time`),
    INDEX `idx_endpoint_event_time` (`endpoint`, `event_time`),
    INDEX `idx_worker_id` (`worker_id`),
    INDEX `idx_event_type` (`event_type`),
    INDEX `idx_event_time` (`event_time`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
COMMENT='Task event log for detailed tracking and auditing';

-- ============================================================================
-- Migration: Add extend field to tasks table
-- Description: Add JSON field to store execution history summary
-- ============================================================================
ALTER TABLE `tasks`
ADD COLUMN `extend` JSON NULL COMMENT 'Execution history summary and extended info'
AFTER `completed_at`;

-- ============================================================================
-- Event Types Documentation
-- ============================================================================
-- TASK_CREATED       - Task created by client
-- TASK_QUEUED        - Task enqueued to Redis
-- TASK_ASSIGNED      - Task assigned to worker (pulled by worker)
-- TASK_STARTED       - Worker notified task started
-- TASK_COMPLETED     - Task completed successfully
-- TASK_FAILED        - Task failed with error
-- TASK_CANCELLED     - Task cancelled by user
-- TASK_REQUEUED      - Task re-queued (e.g., after worker lost)
-- TASK_ORPHANED      - Task orphaned (worker lost connection)
-- TASK_RECOVERED     - Task recovered from error state
-- TASK_TIMEOUT       - Task execution timeout
-- WORKER_CHANGED     - Worker reassignment
-- WORKER_LOST        - Worker lost connection during task execution

-- ============================================================================
-- Example extend field structure:
-- ============================================================================
-- {
--   "execution_history": [
--     {
--       "worker_id": "worker-001",
--       "worker_pod_name": "wan22-deployment-abc123",
--       "start_time": "2025-11-10T10:00:05.123Z",
--       "end_time": "2025-11-10T10:02:30.456Z",
--       "duration_ms": 145333,
--       "status": "interrupted",
--       "reason": "worker_lost"
--     },
--     {
--       "worker_id": "worker-002",
--       "worker_pod_name": "wan22-deployment-def456",
--       "start_time": "2025-11-10T10:02:35.000Z",
--       "end_time": "2025-11-10T10:05:00.789Z",
--       "duration_ms": 145789,
--       "status": "completed",
--       "reason": null
--     }
--   ],
--   "retry_count": 1,
--   "first_queued_time": "2025-11-10T10:00:00.000Z",
--   "first_assigned_time": "2025-11-10T10:00:05.123Z",
--   "total_queue_time_ms": 5123,
--   "total_execution_time_ms": 291122
-- }

-- ============================================================================
-- Optional: Initialize extend field for existing tasks
-- ============================================================================
-- This is optional and can be run separately if needed
-- It initializes the extend field for existing completed tasks

-- UPDATE `tasks`
-- SET `extend` = JSON_OBJECT(
--     'execution_history', JSON_ARRAY(
--         JSON_OBJECT(
--             'worker_id', `worker_id`,
--             'start_time', `started_at`,
--             'end_time', `completed_at`,
--             'duration_ms', TIMESTAMPDIFF(MICROSECOND, `started_at`, `completed_at`) / 1000,
--             'status', CASE
--                 WHEN `status` = 'COMPLETED' THEN 'completed'
--                 WHEN `status` = 'FAILED' THEN 'failed'
--                 ELSE 'interrupted'
--             END
--         )
--     ),
--     'retry_count', 0,
--     'first_queued_time', `created_at`,
--     'first_assigned_time', `started_at`,
--     'total_queue_time_ms', TIMESTAMPDIFF(MICROSECOND, `created_at`, `started_at`) / 1000,
--     'total_execution_time_ms', TIMESTAMPDIFF(MICROSECOND, `started_at`, `completed_at`) / 1000
-- )
-- WHERE `worker_id` IS NOT NULL
--   AND `started_at` IS NOT NULL
--   AND `extend` IS NULL;

-- ============================================================================
-- Data Retention Policy (Optional)
-- ============================================================================
-- Recommendation: Keep task_events for 30 days, then archive or delete
-- This can be implemented as a scheduled job

-- Example cleanup query (to be run periodically):
-- DELETE FROM `task_events`
-- WHERE `event_time` < DATE_SUB(NOW(), INTERVAL 30 DAY);

-- Or archive before deletion:
-- INSERT INTO `task_events_archive`
-- SELECT * FROM `task_events`
-- WHERE `event_time` < DATE_SUB(NOW(), INTERVAL 30 DAY);
--
-- DELETE FROM `task_events`
-- WHERE `event_time` < DATE_SUB(NOW(), INTERVAL 30 DAY);
