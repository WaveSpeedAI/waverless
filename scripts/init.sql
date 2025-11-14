-- Waverless MySQL Database Schema
-- Database: waverless
-- Character Set: utf8mb4

-- ============================================================================
-- Table: endpoints
-- Description: Stores endpoint metadata and deployment configuration
-- ============================================================================
CREATE TABLE IF NOT EXISTS `endpoints` (
    `id` BIGINT NOT NULL AUTO_INCREMENT,
    `endpoint` VARCHAR(255) NOT NULL COMMENT 'Endpoint name (unique identifier)',
    `spec_name` VARCHAR(100) NOT NULL COMMENT 'Resource spec name',
    `image` VARCHAR(500) NOT NULL COMMENT 'Docker image',
    `replicas` INT NOT NULL DEFAULT 1 COMMENT 'Target replica count',
    `task_timeout` INT NOT NULL DEFAULT 0 COMMENT 'Task execution timeout in seconds (0 = use global default)',
    `env` JSON NULL COMMENT 'Environment variables as JSON object',
    `labels` JSON NULL COMMENT 'Labels as JSON object',
    `status` VARCHAR(50) NOT NULL DEFAULT 'active' COMMENT 'Endpoint status: active, inactive, deleted',
    `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    `updated_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),

    PRIMARY KEY (`id`),
    UNIQUE INDEX `idx_endpoint_unique` (`endpoint`),
    INDEX `idx_status` (`status`),
    INDEX `idx_created_at` (`created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 
COMMENT='Endpoint metadata and deployment configuration';

-- ============================================================================
-- Table: tasks
-- Description: Task records (all statuses: PENDING, IN_PROGRESS, COMPLETED, FAILED)
-- ============================================================================
CREATE TABLE IF NOT EXISTS `tasks` (
    `id` BIGINT NOT NULL AUTO_INCREMENT,
    `task_id` VARCHAR(255) NOT NULL COMMENT 'Task unique ID (UUID)',
    `endpoint` VARCHAR(255) NOT NULL COMMENT 'Endpoint name',
    `input` JSON NOT NULL COMMENT 'Task input parameters as JSON',
    `status` VARCHAR(50) NOT NULL COMMENT 'Task status: PENDING, IN_PROGRESS, COMPLETED, FAILED, CANCELLED',
    `output` JSON NULL COMMENT 'Task output as JSON',
    `error` TEXT NULL COMMENT 'Error message if task failed',
    `worker_id` VARCHAR(255) NULL COMMENT 'Worker ID processing this task',
    `webhook_url` VARCHAR(1000) NULL COMMENT 'Webhook URL for completion notification',
    `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    `updated_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    `started_at` DATETIME(3) NULL COMMENT 'Time when task started processing',
    `completed_at` DATETIME(3) NULL COMMENT 'Time when task completed (success or failure)',

    PRIMARY KEY (`id`),
    UNIQUE INDEX `idx_task_id_unique` (`task_id`),
    INDEX `idx_endpoint_status` (`endpoint`, `status`),
    INDEX `idx_status` (`status`),
    INDEX `idx_worker_id` (`worker_id`),
    INDEX `idx_created_at` (`created_at`),
    INDEX `idx_completed_at` (`completed_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 
COMMENT='Task records with all statuses';

-- ============================================================================
-- Table: autoscaler_configs
-- Description: Autoscaler configuration per endpoint
-- ============================================================================
CREATE TABLE IF NOT EXISTS `autoscaler_configs` (
    `id` BIGINT NOT NULL AUTO_INCREMENT,
    `endpoint` VARCHAR(255) NOT NULL COMMENT 'Endpoint name',
    `display_name` VARCHAR(255) NULL COMMENT 'Display name',
    `spec_name` VARCHAR(100) NULL COMMENT 'Spec name for resource calculation',

    -- Replica configuration
    `min_replicas` INT NOT NULL DEFAULT 0 COMMENT 'Minimum replica count',
    `max_replicas` INT NOT NULL DEFAULT 10 COMMENT 'Maximum replica count',
    `replicas` INT NOT NULL DEFAULT 1 COMMENT 'Target replica count',

    -- Scaling thresholds
    `scale_up_threshold` INT NOT NULL DEFAULT 1 COMMENT 'Queue length threshold for scale up',
    `scale_down_idle_time` INT NOT NULL DEFAULT 300 COMMENT 'Idle time in seconds before scale down',

    -- Cooldown periods
    `scale_up_cooldown` INT NOT NULL DEFAULT 30 COMMENT 'Scale up cooldown in seconds',
    `scale_down_cooldown` INT NOT NULL DEFAULT 60 COMMENT 'Scale down cooldown in seconds',

    -- Priority configuration
    `priority` INT NOT NULL DEFAULT 50 COMMENT 'Base priority (0-100)',
    `enable_dynamic_prio` BOOLEAN NOT NULL DEFAULT TRUE COMMENT 'Enable dynamic priority adjustment',
    `high_load_threshold` INT NOT NULL DEFAULT 10 COMMENT 'High load threshold for priority boost',
    `priority_boost` INT NOT NULL DEFAULT 20 COMMENT 'Priority boost amount for high load',

    `enabled` BOOLEAN NOT NULL DEFAULT TRUE COMMENT 'Whether autoscaling is enabled for this endpoint',
    `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    `updated_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),

    PRIMARY KEY (`id`),
    UNIQUE INDEX `idx_endpoint_unique` (`endpoint`),
    INDEX `idx_enabled` (`enabled`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
COMMENT='Autoscaler configuration per endpoint';

-- ============================================================================
-- Table: scaling_events
-- Description: Autoscaling event history for monitoring and debugging
-- ============================================================================
CREATE TABLE IF NOT EXISTS `scaling_events` (
    `id` BIGINT NOT NULL AUTO_INCREMENT,
    `event_id` VARCHAR(255) NOT NULL COMMENT 'Event unique ID',
    `endpoint` VARCHAR(255) NOT NULL COMMENT 'Endpoint name',
    `timestamp` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    `action` VARCHAR(50) NOT NULL COMMENT 'Action: scale_up, scale_down, blocked, preempted',
    `from_replicas` INT NOT NULL COMMENT 'Original replica count',
    `to_replicas` INT NOT NULL COMMENT 'Target replica count',
    `reason` TEXT NOT NULL COMMENT 'Reason for this scaling action',
    `queue_length` BIGINT NOT NULL DEFAULT 0 COMMENT 'Pending task queue length',
    `priority` INT NOT NULL DEFAULT 50 COMMENT 'Effective priority at the time',
    `preempted_from` JSON NULL COMMENT 'List of endpoints this action preempted from',

    PRIMARY KEY (`id`),
    UNIQUE INDEX `idx_event_id_unique` (`event_id`),
    INDEX `idx_endpoint_timestamp` (`endpoint`, `timestamp`),
    INDEX `idx_action` (`action`),
    INDEX `idx_timestamp` (`timestamp`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
COMMENT='Autoscaling event history';

-- ============================================================================
-- Migration: Add autoscaler_enabled column for per-endpoint override
-- ============================================================================
-- This allows each endpoint to override the global autoscaler setting
-- NULL or empty string = follow global setting (default)
-- "disabled" = force autoscaler off for this endpoint
-- "enabled" = force autoscaler on for this endpoint
ALTER TABLE `autoscaler_configs`
ADD COLUMN `autoscaler_enabled` VARCHAR(20) NULL
COMMENT 'Autoscaler override: NULL/"" = follow global, "disabled" = force off, "enabled" = force on'
AFTER `enabled`;

-- ============================================================================
-- Insert default global autoscaler config (optional)
-- ============================================================================
-- This can be used as a template or default configuration
-- Actual endpoint configs will be created dynamically
