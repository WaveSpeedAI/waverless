-- Waverless Database Schema
-- Consolidated from all migrations
-- Last updated: 2026-02-04

-- ============================================================================
-- Core Tables
-- ============================================================================

-- Resource Specifications
CREATE TABLE IF NOT EXISTS `resource_specs` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `name` varchar(100) NOT NULL COMMENT 'Spec name (unique identifier)',
  `display_name` varchar(255) NOT NULL COMMENT 'Display name',
  `category` varchar(50) NOT NULL COMMENT 'Category: cpu, gpu',
  `cpu` varchar(50) DEFAULT NULL COMMENT 'CPU cores (e.g., "2", "4")',
  `memory` varchar(50) NOT NULL COMMENT 'Memory (e.g., "4Gi", "8Gi")',
  `gpu` varchar(50) DEFAULT NULL COMMENT 'GPU count (e.g., "1", "2")',
  `gpu_type` varchar(100) DEFAULT NULL COMMENT 'GPU type (e.g., "NVIDIA-H200", "NVIDIA-A100")',
  `ephemeral_storage` varchar(50) NOT NULL COMMENT 'Ephemeral storage (e.g., "30", "300")',
  `shm_size` varchar(50) DEFAULT NULL COMMENT 'Shared memory size (e.g., "1Gi", "512Mi")',
  `resource_type` varchar(20) NOT NULL DEFAULT 'serverless' COMMENT 'Resource type: fixed, serverless',
  `platforms` json DEFAULT NULL COMMENT 'Platform-specific configurations as JSON',
  `status` varchar(50) NOT NULL DEFAULT 'active' COMMENT 'Spec status: active, inactive, deprecated',
  `created_at` datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at` datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`),
  UNIQUE KEY `idx_spec_name_unique` (`name`),
  KEY `idx_category` (`category`),
  KEY `idx_status` (`status`),
  KEY `idx_created_at` (`created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Resource specifications for deployments';

-- Spec Capacity Status
CREATE TABLE IF NOT EXISTS `spec_capacity` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `spec_name` varchar(100) NOT NULL,
  `status` enum('available', 'limited', 'sold_out') NOT NULL DEFAULT 'available',
  `reason` varchar(255) DEFAULT NULL,
  `running_count` int NOT NULL DEFAULT 0,
  `pending_count` int NOT NULL DEFAULT 0,
  `failure_count` int NOT NULL DEFAULT 0,
  `spot_score` int DEFAULT NULL COMMENT 'Spot Placement Score (1-10)',
  `spot_price` decimal(10,6) DEFAULT NULL COMMENT 'Current Spot price (USD/hour)',
  `instance_type` varchar(50) DEFAULT NULL COMMENT 'Primary instance type',
  `last_success_at` datetime(3) DEFAULT NULL,
  `last_failure_at` datetime(3) DEFAULT NULL,
  `last_spot_check_at` datetime(3) DEFAULT NULL COMMENT 'Last Spot check time',
  `updated_at` datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`),
  UNIQUE KEY `idx_spec_name` (`spec_name`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Spec capacity status tracking';

-- Endpoints
CREATE TABLE IF NOT EXISTS `endpoints` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `endpoint` varchar(255) NOT NULL COMMENT 'Endpoint name (unique identifier)',
  `spec_name` varchar(100) NOT NULL COMMENT 'Resource spec name',
  `image` varchar(500) NOT NULL COMMENT 'Docker image',
  `image_prefix` varchar(500) NOT NULL DEFAULT '' COMMENT 'Image prefix for matching updates',
  `image_digest` varchar(255) NOT NULL DEFAULT '' COMMENT 'Current image digest from DockerHub',
  `latest_image` varchar(500) NOT NULL DEFAULT '' COMMENT 'Latest available image if update is detected',
  `image_last_checked` datetime(3) DEFAULT NULL COMMENT 'Last time image was checked for updates',
  `description` varchar(500) NOT NULL DEFAULT '',
  `replicas` int NOT NULL DEFAULT 1 COMMENT 'Target replica count',
  `gpu_count` int NOT NULL DEFAULT 1 COMMENT 'GPU count per replica',
  `task_timeout` int NOT NULL DEFAULT 0 COMMENT 'Task execution timeout in seconds (0 = use global default)',
  `env` json DEFAULT NULL COMMENT 'Environment variables as JSON object',
  `labels` json DEFAULT NULL COMMENT 'Labels as JSON object',
  `status` varchar(50) NOT NULL DEFAULT 'active' COMMENT 'Endpoint status: active, inactive, deleted',
  `enable_ptrace` tinyint(1) NOT NULL DEFAULT 0 COMMENT 'Enable SYS_PTRACE capability for debugging',
  `max_pending_tasks` int NOT NULL DEFAULT 1 COMMENT 'Maximum allowed pending tasks before warning clients',
  `runtime_state` json DEFAULT NULL COMMENT 'K8s runtime state: namespace, readyReplicas, availableReplicas, shmSize, volumeMounts',
  `health_status` varchar(16) DEFAULT 'HEALTHY' COMMENT 'Health status: HEALTHY, UNHEALTHY, DEGRADED',
  `health_message` varchar(512) DEFAULT NULL COMMENT 'Health-related messages',
  `last_health_check_at` datetime(3) DEFAULT NULL COMMENT 'Last health check timestamp',
  `created_at` datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at` datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`),
  UNIQUE KEY `idx_endpoint_unique` (`endpoint`),
  KEY `idx_status` (`status`),
  KEY `idx_created_at` (`created_at`),
  KEY `idx_endpoints_health_status` (`health_status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Endpoint metadata and deployment configuration';

-- Autoscaler Configurations
CREATE TABLE IF NOT EXISTS `autoscaler_configs` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `endpoint` varchar(255) NOT NULL COMMENT 'Endpoint name',
  `display_name` varchar(255) DEFAULT NULL COMMENT 'Display name',
  `spec_name` varchar(100) DEFAULT NULL COMMENT 'Spec name for resource calculation',
  `min_replicas` int NOT NULL DEFAULT 0 COMMENT 'Minimum replica count',
  `max_replicas` int NOT NULL DEFAULT 10 COMMENT 'Maximum replica count',
  `replicas` int NOT NULL DEFAULT 1 COMMENT 'Target replica count',
  `scale_up_threshold` int NOT NULL DEFAULT 1 COMMENT 'Queue length threshold for scale up',
  `scale_down_idle_time` int NOT NULL DEFAULT 300 COMMENT 'Idle time in seconds before scale down',
  `scale_up_cooldown` int NOT NULL DEFAULT 30 COMMENT 'Scale up cooldown in seconds',
  `scale_down_cooldown` int NOT NULL DEFAULT 60 COMMENT 'Scale down cooldown in seconds',
  `priority` int NOT NULL DEFAULT 50 COMMENT 'Base priority (0-100)',
  `enable_dynamic_prio` tinyint(1) NOT NULL DEFAULT 1 COMMENT 'Enable dynamic priority adjustment',
  `high_load_threshold` int NOT NULL DEFAULT 10 COMMENT 'High load threshold for priority boost',
  `priority_boost` int NOT NULL DEFAULT 20 COMMENT 'Priority boost amount for high load',
  `enabled` tinyint(1) NOT NULL DEFAULT 1 COMMENT 'Whether autoscaling is enabled for this endpoint',
  `autoscaler_enabled` varchar(20) DEFAULT NULL COMMENT 'Autoscaler override: NULL/"" = follow global, "disabled" = force off, "enabled" = force on',
  `created_at` datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at` datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  `last_task_time` datetime(3) DEFAULT NULL COMMENT 'Last task completion time (for idle time calculation)',
  `last_scale_time` datetime(3) DEFAULT NULL COMMENT 'Last scaling operation time (for cooldown)',
  `first_pending_time` datetime(3) DEFAULT NULL COMMENT 'First pending task time (for starvation prevention)',
  PRIMARY KEY (`id`),
  UNIQUE KEY `idx_endpoint_unique` (`endpoint`),
  KEY `idx_enabled` (`enabled`),
  KEY `idx_last_task_time` (`last_task_time`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Autoscaler configuration per endpoint';

-- ============================================================================
-- Worker Tables
-- ============================================================================

-- Workers
CREATE TABLE IF NOT EXISTS `workers` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `worker_id` varchar(255) NOT NULL COMMENT 'Worker unique ID (usually pod name)',
  `endpoint` varchar(255) NOT NULL COMMENT 'Endpoint name',
  `pod_name` varchar(255) DEFAULT NULL COMMENT 'K8s pod name',
  `status` varchar(50) NOT NULL DEFAULT 'ONLINE' COMMENT 'Worker status: ONLINE, OFFLINE, BUSY, DRAINING',
  `concurrency` int NOT NULL DEFAULT 1 COMMENT 'Maximum concurrency',
  `current_jobs` int NOT NULL DEFAULT 0 COMMENT 'Current number of jobs',
  `jobs_in_progress` text DEFAULT NULL COMMENT 'JSON array of task IDs currently being processed',
  `version` varchar(100) DEFAULT NULL COMMENT 'Worker version',
  `pod_created_at` datetime(3) DEFAULT NULL COMMENT 'Pod creation time',
  `pod_started_at` datetime(3) DEFAULT NULL COMMENT 'Pod started time (container running)',
  `pod_ready_at` datetime(3) DEFAULT NULL COMMENT 'Pod ready time',
  `cold_start_duration_ms` bigint DEFAULT NULL COMMENT 'Cold start duration in milliseconds',
  `last_heartbeat` datetime(3) NOT NULL COMMENT 'Last heartbeat time',
  `last_task_time` datetime(3) DEFAULT NULL COMMENT 'Last task completion time',
  `total_tasks_completed` bigint NOT NULL DEFAULT 0 COMMENT 'Total completed tasks',
  `total_tasks_failed` bigint NOT NULL DEFAULT 0 COMMENT 'Total failed tasks',
  `total_execution_time_ms` bigint NOT NULL DEFAULT 0 COMMENT 'Total execution time in milliseconds',
  `runtime_state` json DEFAULT NULL COMMENT 'Runtime state JSON',
  `failure_type` varchar(32) DEFAULT NULL COMMENT 'Type of failure: IMAGE_PULL_FAILED, CONTAINER_CRASH, RESOURCE_LIMIT, TIMEOUT, UNKNOWN',
  `failure_reason` varchar(512) DEFAULT NULL COMMENT 'Sanitized user-friendly failure message',
  `failure_details` text DEFAULT NULL COMMENT 'JSON with full failure details for debugging',
  `failure_occurred_at` datetime DEFAULT NULL COMMENT 'Timestamp when failure was detected',
  `created_at` datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at` datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`),
  UNIQUE KEY `idx_worker_id_unique` (`worker_id`),
  KEY `idx_endpoint` (`endpoint`),
  KEY `idx_status` (`status`),
  KEY `idx_last_heartbeat` (`last_heartbeat`),
  KEY `idx_workers_failure_type` (`failure_type`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Worker records';

-- Worker Events (lifecycle tracking)
CREATE TABLE IF NOT EXISTS `worker_events` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `event_id` varchar(255) NOT NULL COMMENT 'Unique event ID',
  `worker_id` varchar(255) NOT NULL COMMENT 'Worker ID',
  `endpoint` varchar(255) NOT NULL COMMENT 'Endpoint name',
  `event_type` varchar(50) NOT NULL COMMENT 'Event type: WORKER_STARTED, WORKER_REGISTERED, WORKER_TASK_PULLED, WORKER_TASK_COMPLETED, WORKER_OFFLINE, WORKER_IDLE',
  `event_time` datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT 'Event timestamp',
  `cold_start_duration_ms` bigint DEFAULT NULL COMMENT 'Cold start duration (for WORKER_REGISTERED event)',
  `idle_duration_ms` bigint DEFAULT NULL COMMENT 'Idle duration before this event (for WORKER_TASK_PULLED event)',
  `task_id` varchar(255) DEFAULT NULL COMMENT 'Related task ID (for task events)',
  `metadata` json DEFAULT NULL COMMENT 'Additional metadata',
  PRIMARY KEY (`id`),
  UNIQUE KEY `idx_event_id_unique` (`event_id`),
  KEY `idx_worker_event_time` (`worker_id`, `event_time`),
  KEY `idx_endpoint_event_time` (`endpoint`, `event_time`),
  KEY `idx_event_type_time` (`event_type`, `event_time`),
  KEY `idx_event_time` (`event_time`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Worker lifecycle events';

-- Worker Resource Snapshots
CREATE TABLE IF NOT EXISTS `worker_resource_snapshots` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `worker_id` varchar(255) NOT NULL,
  `endpoint` varchar(255) DEFAULT NULL,
  `snapshot_at` datetime(3) NOT NULL,
  `gpu_utilization` decimal(5,2) DEFAULT NULL,
  `gpu_memory_used_mb` int DEFAULT NULL,
  `gpu_memory_total_mb` int DEFAULT NULL,
  `gpu_temperature` int DEFAULT NULL,
  `cpu_utilization` decimal(5,2) DEFAULT NULL,
  `memory_used_mb` int DEFAULT NULL,
  `memory_total_mb` int DEFAULT NULL,
  `current_task_id` varchar(255) DEFAULT NULL,
  `is_idle` tinyint(1) NOT NULL DEFAULT 1,
  PRIMARY KEY (`id`),
  KEY `idx_worker_snapshot` (`worker_id`, `snapshot_at`),
  KEY `idx_snapshot_at` (`snapshot_at`),
  KEY `idx_endpoint_snapshot` (`endpoint`, `snapshot_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Worker resource usage snapshots';

-- ============================================================================
-- Task Tables
-- ============================================================================

-- Tasks
CREATE TABLE IF NOT EXISTS `tasks` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `task_id` varchar(255) NOT NULL COMMENT 'Task unique ID (UUID)',
  `endpoint` varchar(255) NOT NULL COMMENT 'Endpoint name',
  `input` json NOT NULL COMMENT 'Task input parameters as JSON',
  `status` varchar(50) NOT NULL COMMENT 'Task status: PENDING, IN_PROGRESS, COMPLETED, FAILED, CANCELLED',
  `output` json DEFAULT NULL COMMENT 'Task output as JSON',
  `error` text COMMENT 'Error message if task failed',
  `worker_id` varchar(255) DEFAULT NULL COMMENT 'Worker ID processing this task',
  `webhook_url` varchar(1000) DEFAULT NULL COMMENT 'Webhook URL for completion notification',
  `webhook_status` varchar(50) DEFAULT NULL COMMENT 'Webhook status: PENDING, SUCCESS, FAILED',
  `created_at` datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at` datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  `started_at` datetime(3) DEFAULT NULL COMMENT 'Time when task started processing',
  `completed_at` datetime(3) DEFAULT NULL COMMENT 'Time when task completed (success or failure)',
  `extend` json DEFAULT NULL COMMENT 'Execution history summary and extended info',
  PRIMARY KEY (`id`),
  UNIQUE KEY `idx_task_id_unique` (`task_id`),
  KEY `idx_endpoint_status` (`endpoint`, `status`),
  KEY `idx_status` (`status`),
  KEY `idx_worker_id` (`worker_id`),
  KEY `idx_created_at` (`created_at`),
  KEY `idx_completed_at` (`completed_at`),
  KEY `idx_endpoint_id` (`endpoint`, `id`),
  KEY `idx_endpoint_status_id` (`endpoint`, `status`, `id`),
  KEY `idx_status_id` (`status`, `id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Task records with all statuses';

-- Task Events
CREATE TABLE IF NOT EXISTS `task_events` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `event_id` varchar(255) NOT NULL COMMENT 'Event unique ID',
  `task_id` varchar(255) NOT NULL COMMENT 'Task ID (foreign key to tasks.task_id)',
  `endpoint` varchar(255) NOT NULL COMMENT 'Endpoint name',
  `event_type` varchar(50) NOT NULL COMMENT 'Event type: TASK_CREATED, TASK_ASSIGNED, TASK_COMPLETED, etc.',
  `event_time` datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT 'Event timestamp',
  `worker_id` varchar(255) DEFAULT NULL COMMENT 'Worker ID',
  `worker_pod_name` varchar(255) DEFAULT NULL COMMENT 'Worker Pod name in Kubernetes',
  `from_status` varchar(50) DEFAULT NULL COMMENT 'Original status',
  `to_status` varchar(50) DEFAULT NULL COMMENT 'New status after this event',
  `error_message` text COMMENT 'Error message if event is failure-related',
  `error_type` varchar(100) DEFAULT NULL COMMENT 'Error type classification',
  `retry_count` int NOT NULL DEFAULT 0 COMMENT 'Retry count at the time of this event',
  `queue_wait_ms` int DEFAULT NULL COMMENT 'Queue wait time in milliseconds',
  `execution_duration_ms` int DEFAULT NULL COMMENT 'Execution duration in milliseconds',
  `total_duration_ms` int DEFAULT NULL COMMENT 'Total duration in milliseconds',
  `metadata` json DEFAULT NULL COMMENT 'Additional event metadata',
  PRIMARY KEY (`id`),
  UNIQUE KEY `idx_event_id_unique` (`event_id`),
  KEY `idx_task_id_event_time` (`task_id`, `event_time`),
  KEY `idx_endpoint_event_time` (`endpoint`, `event_time`),
  KEY `idx_endpoint_event_type` (`endpoint`, `event_type`, `event_time`),
  KEY `idx_worker_id` (`worker_id`),
  KEY `idx_event_type` (`event_type`),
  KEY `idx_event_time` (`event_time`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Task event log for detailed tracking and auditing';

-- Task Statistics
CREATE TABLE IF NOT EXISTS `task_statistics` (
  `id` int NOT NULL AUTO_INCREMENT,
  `scope_type` varchar(50) NOT NULL COMMENT 'Statistics scope: global or endpoint',
  `scope_value` varchar(255) DEFAULT NULL COMMENT 'Endpoint name (NULL for global scope)',
  `pending_count` int DEFAULT 0 COMMENT 'Number of PENDING tasks',
  `in_progress_count` int DEFAULT 0 COMMENT 'Number of IN_PROGRESS tasks',
  `completed_count` int DEFAULT 0 COMMENT 'Number of COMPLETED tasks',
  `failed_count` int DEFAULT 0 COMMENT 'Number of FAILED tasks',
  `cancelled_count` int DEFAULT 0 COMMENT 'Number of CANCELLED tasks',
  `total_count` int DEFAULT 0 COMMENT 'Total number of tasks',
  `updated_at` datetime(3) NOT NULL COMMENT 'Last update timestamp',
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_scope` (`scope_type`, `scope_value`),
  KEY `idx_updated_at` (`updated_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Task statistics for dashboard';

-- ============================================================================
-- Monitoring & Statistics Tables
-- ============================================================================

-- Endpoint Minute Statistics
CREATE TABLE IF NOT EXISTS `endpoint_minute_stats` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `endpoint` varchar(255) NOT NULL,
  `stat_minute` datetime NOT NULL COMMENT 'Minute timestamp',
  `active_workers` int DEFAULT 0,
  `idle_workers` int DEFAULT 0,
  `avg_worker_utilization` decimal(5,2) DEFAULT 0,
  `tasks_submitted` int DEFAULT 0,
  `tasks_completed` int DEFAULT 0,
  `tasks_failed` int DEFAULT 0,
  `tasks_timeout` int DEFAULT 0,
  `tasks_retried` int DEFAULT 0,
  `avg_queue_wait_ms` decimal(10,2) DEFAULT 0,
  `avg_execution_ms` decimal(10,2) DEFAULT 0,
  `p50_execution_ms` decimal(10,2) DEFAULT 0,
  `p95_execution_ms` decimal(10,2) DEFAULT 0,
  `avg_gpu_utilization` decimal(5,2) DEFAULT 0,
  `max_gpu_utilization` decimal(5,2) DEFAULT 0,
  `avg_idle_duration_sec` decimal(10,2) DEFAULT 0,
  `max_idle_duration_sec` int DEFAULT 0,
  `total_idle_time_sec` int DEFAULT 0,
  `idle_count` int DEFAULT 0,
  `workers_created` int DEFAULT 0,
  `workers_terminated` int DEFAULT 0,
  `cold_starts` int DEFAULT 0,
  `avg_cold_start_ms` decimal(10,2) DEFAULT 0,
  `created_at` datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_endpoint_minute` (`endpoint`, `stat_minute`),
  KEY `idx_stat_minute` (`stat_minute`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Endpoint minute-level statistics';

-- Endpoint Hourly Statistics
CREATE TABLE IF NOT EXISTS `endpoint_hourly_stats` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `endpoint` varchar(255) NOT NULL,
  `stat_hour` datetime NOT NULL COMMENT 'Hour timestamp',
  `active_workers` int DEFAULT 0,
  `idle_workers` int DEFAULT 0,
  `avg_worker_utilization` decimal(5,2) DEFAULT 0,
  `tasks_submitted` int DEFAULT 0,
  `tasks_completed` int DEFAULT 0,
  `tasks_failed` int DEFAULT 0,
  `tasks_timeout` int DEFAULT 0,
  `tasks_retried` int DEFAULT 0,
  `avg_queue_wait_ms` decimal(10,2) DEFAULT 0,
  `avg_execution_ms` decimal(10,2) DEFAULT 0,
  `p50_execution_ms` decimal(10,2) DEFAULT 0,
  `p95_execution_ms` decimal(10,2) DEFAULT 0,
  `avg_gpu_utilization` decimal(5,2) DEFAULT 0,
  `max_gpu_utilization` decimal(5,2) DEFAULT 0,
  `avg_idle_duration_sec` decimal(10,2) DEFAULT 0,
  `max_idle_duration_sec` int DEFAULT 0,
  `total_idle_time_sec` bigint DEFAULT 0,
  `idle_count` int DEFAULT 0,
  `workers_created` int DEFAULT 0,
  `workers_terminated` int DEFAULT 0,
  `cold_starts` int DEFAULT 0,
  `avg_cold_start_ms` decimal(10,2) DEFAULT 0,
  `created_at` datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_endpoint_hour` (`endpoint`, `stat_hour`),
  KEY `idx_stat_hour` (`stat_hour`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Endpoint hourly statistics';

-- Endpoint Daily Statistics
CREATE TABLE IF NOT EXISTS `endpoint_daily_stats` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `endpoint` varchar(255) NOT NULL,
  `stat_date` date NOT NULL COMMENT 'Date',
  `active_workers` int DEFAULT 0,
  `idle_workers` int DEFAULT 0,
  `avg_worker_utilization` decimal(5,2) DEFAULT 0,
  `tasks_submitted` int DEFAULT 0,
  `tasks_completed` int DEFAULT 0,
  `tasks_failed` int DEFAULT 0,
  `tasks_timeout` int DEFAULT 0,
  `tasks_retried` int DEFAULT 0,
  `avg_queue_wait_ms` decimal(10,2) DEFAULT 0,
  `avg_execution_ms` decimal(10,2) DEFAULT 0,
  `p50_execution_ms` decimal(10,2) DEFAULT 0,
  `p95_execution_ms` decimal(10,2) DEFAULT 0,
  `avg_gpu_utilization` decimal(5,2) DEFAULT 0,
  `max_gpu_utilization` decimal(5,2) DEFAULT 0,
  `avg_idle_duration_sec` decimal(10,2) DEFAULT 0,
  `max_idle_duration_sec` int DEFAULT 0,
  `total_idle_time_sec` bigint DEFAULT 0,
  `idle_count` int DEFAULT 0,
  `workers_created` int DEFAULT 0,
  `workers_terminated` int DEFAULT 0,
  `cold_starts` int DEFAULT 0,
  `avg_cold_start_ms` decimal(10,2) DEFAULT 0,
  `created_at` datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_endpoint_date` (`endpoint`, `stat_date`),
  KEY `idx_stat_date` (`stat_date`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Endpoint daily statistics';

-- Scaling Events
CREATE TABLE IF NOT EXISTS `scaling_events` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `event_id` varchar(255) NOT NULL COMMENT 'Event unique ID',
  `endpoint` varchar(255) NOT NULL COMMENT 'Endpoint name',
  `timestamp` datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `action` varchar(50) NOT NULL COMMENT 'Action: scale_up, scale_down, blocked, preempted',
  `from_replicas` int NOT NULL COMMENT 'Original replica count',
  `to_replicas` int NOT NULL COMMENT 'Target replica count',
  `reason` text NOT NULL COMMENT 'Reason for this scaling action',
  `queue_length` bigint NOT NULL DEFAULT 0 COMMENT 'Pending task queue length',
  `priority` int NOT NULL DEFAULT 50 COMMENT 'Effective priority at the time',
  `preempted_from` json DEFAULT NULL COMMENT 'List of endpoints this action preempted from',
  PRIMARY KEY (`id`),
  UNIQUE KEY `idx_event_id_unique` (`event_id`),
  KEY `idx_endpoint_timestamp` (`endpoint`, `timestamp`),
  KEY `idx_action` (`action`),
  KEY `idx_timestamp` (`timestamp`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Autoscaling event history';
