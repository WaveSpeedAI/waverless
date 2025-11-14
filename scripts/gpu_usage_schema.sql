-- ===============================================================================
-- GPU Usage Statistics Schema
-- ===============================================================================
-- Purpose: Create and maintain GPU usage tracking tables
-- Date: 2025-11-13
--
-- This schema tracks GPU usage at three levels:
-- 1. gpu_usage_records: Task-level detailed records
-- 2. gpu_usage_statistics_minute: Minute-level aggregated statistics
-- 3. gpu_usage_statistics_hourly: Hourly aggregated statistics
-- 4. gpu_usage_statistics_daily: Daily aggregated statistics
--
-- Key Features:
-- - UTC timezone for all timestamps
-- - Multi-scope support (global, endpoint, spec)
-- - Generated column for NULL-safe unique constraints
-- - Hierarchical aggregation (records → minute → hourly → daily)
-- ===============================================================================

-- Optional: Drop existing tables for clean reinstall (use with caution!)
-- DROP TABLE IF EXISTS gpu_usage_statistics_daily;
-- DROP TABLE IF EXISTS gpu_usage_statistics_hourly;
-- DROP TABLE IF EXISTS gpu_usage_statistics_minute;
-- DROP TABLE IF EXISTS gpu_usage_records;

-- ===============================================================================
-- Table: gpu_usage_records
-- ===============================================================================
-- Stores detailed GPU usage information for each completed task
-- ===============================================================================
CREATE TABLE IF NOT EXISTS gpu_usage_records (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    task_id VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL COMMENT 'Unique task identifier',
    endpoint VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL COMMENT 'Endpoint name',
    worker_id VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci DEFAULT NULL COMMENT 'Worker/Pod ID',
    spec_name VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci DEFAULT NULL COMMENT 'Spec name (e.g., h200-single)',
    gpu_count INT NOT NULL DEFAULT 1 COMMENT 'Number of GPUs used',
    gpu_type VARCHAR(100) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci DEFAULT NULL COMMENT 'GPU type (e.g., NVIDIA-H200)',
    gpu_memory_gb INT DEFAULT NULL COMMENT 'GPU memory per card (GB)',
    started_at DATETIME(3) NOT NULL COMMENT 'Task start time (UTC)',
    completed_at DATETIME(3) NOT NULL COMMENT 'Task completion time (UTC)',
    duration_seconds INT NOT NULL COMMENT 'Task duration in seconds',
    gpu_hours DECIMAL(10, 4) NOT NULL COMMENT 'GPU card-hours (gpu_count × duration_hours)',
    status VARCHAR(50) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL COMMENT 'Task status (COMPLETED, FAILED)',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),

    -- Indexes
    UNIQUE INDEX idx_task_id_unique (task_id),
    INDEX idx_endpoint (endpoint),
    INDEX idx_spec_name (spec_name),
    INDEX idx_started_at (started_at),
    INDEX idx_completed_at (completed_at),
    INDEX idx_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
  COMMENT='Task-level GPU usage records';

-- ===============================================================================
-- Table: gpu_usage_statistics_minute
-- ===============================================================================
-- Minute-level aggregated GPU usage statistics
-- Supports three scope types: global, endpoint, spec
-- ===============================================================================
CREATE TABLE IF NOT EXISTS gpu_usage_statistics_minute (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    time_bucket DATETIME NOT NULL COMMENT 'Time bucket (minute granularity, e.g., 2025-11-12 10:30:00)',
    scope_type VARCHAR(50) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL COMMENT 'Scope type: global, endpoint, or spec',
    scope_value VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci DEFAULT NULL COMMENT 'Endpoint/Spec name (NULL for global)',

    -- Task metrics
    total_tasks INT DEFAULT 0 COMMENT 'Total tasks completed in this minute',
    completed_tasks INT DEFAULT 0 COMMENT 'Successfully completed tasks',
    failed_tasks INT DEFAULT 0 COMMENT 'Failed tasks',

    -- GPU usage metrics
    total_gpu_seconds DECIMAL(12, 2) DEFAULT 0 COMMENT 'Total GPU card-seconds used',
    total_gpu_hours DECIMAL(10, 4) DEFAULT 0 COMMENT 'Total GPU card-hours used',
    avg_gpu_count DECIMAL(10, 2) DEFAULT 0 COMMENT 'Average GPU count per task',
    max_gpu_count INT DEFAULT 0 COMMENT 'Max GPU count in any single task',

    -- Time range
    period_start DATETIME(3) NOT NULL COMMENT 'Period start time',
    period_end DATETIME(3) NOT NULL COMMENT 'Period end time',

    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),

    -- Generated column to handle NULL in unique constraint
    scope_value_key VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci
        AS (COALESCE(scope_value, '__GLOBAL__')) STORED,

    -- Indexes
    UNIQUE KEY uk_time_scope (time_bucket, scope_type, scope_value_key),
    INDEX idx_time_bucket (time_bucket),
    INDEX idx_scope (scope_type, scope_value),
    INDEX idx_updated_at (updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
  COMMENT='Minute-level aggregated GPU usage statistics';

-- ===============================================================================
-- Table: gpu_usage_statistics_hourly
-- ===============================================================================
-- Hourly aggregated GPU usage statistics
-- Includes peak minute tracking
-- ===============================================================================
CREATE TABLE IF NOT EXISTS gpu_usage_statistics_hourly (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    time_bucket DATETIME NOT NULL COMMENT 'Time bucket (hour granularity, e.g., 2025-11-12 10:00:00)',
    scope_type VARCHAR(50) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL COMMENT 'Scope type: global, endpoint, or spec',
    scope_value VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci DEFAULT NULL COMMENT 'Endpoint/Spec name (NULL for global)',

    -- Task metrics
    total_tasks INT DEFAULT 0 COMMENT 'Total tasks completed in this hour',
    completed_tasks INT DEFAULT 0 COMMENT 'Successfully completed tasks',
    failed_tasks INT DEFAULT 0 COMMENT 'Failed tasks',

    -- GPU usage metrics
    total_gpu_hours DECIMAL(10, 4) DEFAULT 0 COMMENT 'Total GPU card-hours used',
    avg_gpu_count DECIMAL(10, 2) DEFAULT 0 COMMENT 'Average GPU count per task',
    max_gpu_count INT DEFAULT 0 COMMENT 'Max GPU count in any single task',

    -- Peak minute info
    peak_minute DATETIME DEFAULT NULL COMMENT 'Minute with highest GPU usage',
    peak_gpu_hours DECIMAL(10, 4) DEFAULT NULL COMMENT 'GPU hours in peak minute',

    -- Time range
    period_start DATETIME(3) NOT NULL COMMENT 'Period start time',
    period_end DATETIME(3) NOT NULL COMMENT 'Period end time',

    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),

    -- Generated column to handle NULL in unique constraint
    scope_value_key VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci
        AS (COALESCE(scope_value, '__GLOBAL__')) STORED,

    -- Indexes
    UNIQUE KEY uk_time_scope (time_bucket, scope_type, scope_value_key),
    INDEX idx_time_bucket (time_bucket),
    INDEX idx_scope (scope_type, scope_value),
    INDEX idx_updated_at (updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
  COMMENT='Hourly aggregated GPU usage statistics';

-- ===============================================================================
-- Table: gpu_usage_statistics_daily
-- ===============================================================================
-- Daily aggregated GPU usage statistics
-- Includes peak hour tracking and utilization metrics
-- ===============================================================================
CREATE TABLE IF NOT EXISTS gpu_usage_statistics_daily (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    time_bucket DATE NOT NULL COMMENT 'Time bucket (day granularity, e.g., 2025-11-12)',
    scope_type VARCHAR(50) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci NOT NULL COMMENT 'Scope type: global, endpoint, or spec',
    scope_value VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci DEFAULT NULL COMMENT 'Endpoint/Spec name (NULL for global)',

    -- Task metrics
    total_tasks INT DEFAULT 0 COMMENT 'Total tasks completed in this day',
    completed_tasks INT DEFAULT 0 COMMENT 'Successfully completed tasks',
    failed_tasks INT DEFAULT 0 COMMENT 'Failed tasks',

    -- GPU usage metrics
    total_gpu_hours DECIMAL(10, 4) DEFAULT 0 COMMENT 'Total GPU card-hours used',
    avg_gpu_count DECIMAL(10, 2) DEFAULT 0 COMMENT 'Average GPU count per task',
    max_gpu_count INT DEFAULT 0 COMMENT 'Max GPU count in any single task',

    -- GPU utilization
    available_gpu_hours DECIMAL(10, 4) DEFAULT NULL COMMENT 'Available GPU hours (capacity)',
    utilization_rate DECIMAL(5, 2) DEFAULT NULL COMMENT 'GPU utilization rate (%)',

    -- Peak hour info
    peak_hour DATETIME DEFAULT NULL COMMENT 'Hour with highest GPU usage',
    peak_gpu_hours DECIMAL(10, 4) DEFAULT NULL COMMENT 'GPU hours in peak hour',

    -- Time range
    period_start DATETIME(3) NOT NULL COMMENT 'Period start time',
    period_end DATETIME(3) NOT NULL COMMENT 'Period end time',

    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),

    -- Generated column to handle NULL in unique constraint
    scope_value_key VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci
        AS (COALESCE(scope_value, '__GLOBAL__')) STORED,

    -- Indexes
    UNIQUE KEY uk_time_scope (time_bucket, scope_type, scope_value_key),
    INDEX idx_time_bucket (time_bucket),
    INDEX idx_scope (scope_type, scope_value),
    INDEX idx_updated_at (updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
  COMMENT='Daily aggregated GPU usage statistics';

-- ===============================================================================
-- Verification Queries
-- ===============================================================================

-- Check if tables were created successfully
SELECT 'GPU usage schema created successfully!' AS status;

-- Verify table existence and record counts
SELECT
    'gpu_usage_records' as table_name,
    COUNT(*) as record_count
FROM gpu_usage_records
UNION ALL
SELECT 'gpu_usage_statistics_minute', COUNT(*) FROM gpu_usage_statistics_minute
UNION ALL
SELECT 'gpu_usage_statistics_hourly', COUNT(*) FROM gpu_usage_statistics_hourly
UNION ALL
SELECT 'gpu_usage_statistics_daily', COUNT(*) FROM gpu_usage_statistics_daily;
