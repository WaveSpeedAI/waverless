-- Migration: Add worker failure tracking fields
-- Description: Adds fields to track worker failure information for image validation and status transparency
-- Requirements: 3.3, 6.1, 6.2

-- Add failure tracking columns to workers table
ALTER TABLE workers ADD COLUMN failure_type VARCHAR(32) DEFAULT NULL COMMENT 'Type of failure: IMAGE_PULL_FAILED, CONTAINER_CRASH, RESOURCE_LIMIT, TIMEOUT, UNKNOWN';
ALTER TABLE workers ADD COLUMN failure_reason VARCHAR(512) DEFAULT NULL COMMENT 'Sanitized user-friendly failure message';
ALTER TABLE workers ADD COLUMN failure_details TEXT DEFAULT NULL COMMENT 'JSON with full failure details for debugging';
ALTER TABLE workers ADD COLUMN failure_occurred_at TIMESTAMP DEFAULT NULL COMMENT 'Timestamp when failure was detected';

-- Create index on failure_type for efficient querying of failed workers
CREATE INDEX idx_workers_failure_type ON workers(failure_type);
