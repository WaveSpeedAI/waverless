-- Migration: Add status_summary fields to endpoints table
-- Description: Extends endpoints table with status summary JSON field and timestamp
-- Requirements: 6.2 - Endpoint model shall include status_summary (JSON field containing aggregated status)
-- Created: 2026-02-04

-- Add status_summary JSON field to store aggregated worker status information
-- Contains: totalWorkers, workersByStatus, workersByPhase, pendingDetails, failureDetails, spotCapacity, lastUpdated
ALTER TABLE endpoints ADD COLUMN status_summary JSON DEFAULT NULL 
    COMMENT 'Aggregated worker status summary: {totalWorkers, workersByStatus, workersByPhase, pendingDetails, failureDetails, spotCapacity, lastUpdated}';

-- Add last_status_summary_at to track when the status summary was last computed
ALTER TABLE endpoints ADD COLUMN last_status_summary_at DATETIME(3) DEFAULT NULL 
    COMMENT 'Timestamp when status_summary was last updated';

-- ============================================================================
-- Rollback Script (for reference)
-- ============================================================================
-- To rollback this migration, execute:
-- ALTER TABLE endpoints DROP COLUMN status_summary;
-- ALTER TABLE endpoints DROP COLUMN last_status_summary_at;
