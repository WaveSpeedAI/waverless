-- Migration: Add pending-related fields to workers table
-- Description: Extends workers table with pending phase tracking fields
-- Requirements: 6.1 - Worker model shall include pending_phase, pending_phase_since, pending_reason, pending_message
-- Created: 2026-02-04

-- Add pending_phase field to track the specific phase within Pending status
-- Values: SCHEDULING, WAITING_NODE, PULLING_IMAGE, INITIALIZING
ALTER TABLE workers ADD COLUMN pending_phase VARCHAR(32) DEFAULT NULL 
    COMMENT 'Pending phase: SCHEDULING, WAITING_NODE, PULLING_IMAGE, INITIALIZING';

-- Add pending_phase_since to track when the current pending phase started
ALTER TABLE workers ADD COLUMN pending_phase_since DATETIME(3) DEFAULT NULL 
    COMMENT 'Timestamp when current pending phase started';

-- Add pending_reason to store the reason for the pending state
ALTER TABLE workers ADD COLUMN pending_reason VARCHAR(255) DEFAULT NULL 
    COMMENT 'Reason for pending state (e.g., Unschedulable, ContainerCreating)';

-- Add pending_message to store detailed message about the pending state
ALTER TABLE workers ADD COLUMN pending_message TEXT DEFAULT NULL 
    COMMENT 'Detailed message about the pending state';

-- Add index for querying workers by pending phase
ALTER TABLE workers ADD INDEX idx_workers_pending_phase (pending_phase);
