-- Migration: Create status_events table
-- Description: Adds status_events table for tracking worker status and phase changes
-- Requirements: 6.3 - THE Waverless SHALL create a new status_events table with appropriate indexes
-- Created: 2026-02-04

-- ============================================================================
-- Status Events Table
-- ============================================================================

-- Create status_events table for tracking worker status and phase changes
CREATE TABLE IF NOT EXISTS `status_events` (
  `id` BIGINT AUTO_INCREMENT PRIMARY KEY,
  `worker_id` VARCHAR(64) NOT NULL COMMENT 'Worker unique ID',
  `endpoint` VARCHAR(255) NOT NULL COMMENT 'Endpoint name',
  `event_type` VARCHAR(32) NOT NULL COMMENT 'Event type: STATUS_CHANGE, PHASE_CHANGE, FAILURE, RECOVERY',
  `old_status` VARCHAR(32) DEFAULT NULL COMMENT 'Previous status before change',
  `new_status` VARCHAR(32) NOT NULL COMMENT 'New status after change',
  `phase` VARCHAR(32) DEFAULT NULL COMMENT 'Pending phase: SCHEDULING, WAITING_NODE, PULLING_IMAGE, INITIALIZING',
  `reason` VARCHAR(255) DEFAULT NULL COMMENT 'Reason for the status change',
  `message` TEXT DEFAULT NULL COMMENT 'Detailed message about the status change',
  `spot_status` JSON DEFAULT NULL COMMENT 'AWS Spot capacity status: {capacity, score, price, instanceType}',
  `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT 'Event timestamp',
  
  -- Indexes for efficient querying
  INDEX `idx_endpoint_created` (`endpoint`, `created_at`) COMMENT 'For querying events by endpoint with time ordering',
  INDEX `idx_worker_created` (`worker_id`, `created_at`) COMMENT 'For querying events by worker with time ordering',
  INDEX `idx_created_at` (`created_at`) COMMENT 'For time-based cleanup and queries'
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='Status change events for workers - tracks status and phase transitions';

-- ============================================================================
-- Rollback Script (for reference)
-- ============================================================================
-- To rollback this migration, execute:
-- DROP TABLE IF EXISTS `status_events`;
