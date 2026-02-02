-- Migration: Add health status fields to endpoints table
-- Requirements: 5.4, 6.3 - Endpoint health status tracking

-- Add health_status column with default value 'HEALTHY'
ALTER TABLE endpoints ADD COLUMN health_status VARCHAR(16) DEFAULT 'HEALTHY';

-- Add health_message column for storing health-related messages
ALTER TABLE endpoints ADD COLUMN health_message VARCHAR(512) DEFAULT NULL;

-- Add last_health_check_at column for tracking when health was last checked
ALTER TABLE endpoints ADD COLUMN last_health_check_at TIMESTAMP DEFAULT NULL;

-- Create index on health_status for efficient querying
CREATE INDEX idx_endpoints_health_status ON endpoints(health_status);
