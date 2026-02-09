-- Migration: Add spot price tracking fields to workers table
-- Records the Spot instance price at worker creation time for cost analysis and billing

ALTER TABLE workers
    ADD COLUMN spot_price DECIMAL(10,6) DEFAULT NULL COMMENT 'Spot price (USD/hour) at worker creation time',
    ADD COLUMN spot_instance_type VARCHAR(64) DEFAULT NULL COMMENT 'Spot instance type (e.g., g4dn.xlarge)';
