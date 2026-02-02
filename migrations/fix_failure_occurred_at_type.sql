-- Migration: Fix failure_occurred_at column type
-- Description: Change failure_occurred_at from TIMESTAMP to DATETIME to avoid automatic timezone conversion
-- TIMESTAMP type causes MySQL to convert times based on server timezone, which conflicts with GORM's loc=UTC setting
-- DATETIME stores the literal value without timezone conversion, consistent with other time columns

ALTER TABLE workers MODIFY COLUMN failure_occurred_at DATETIME DEFAULT NULL COMMENT 'Timestamp when failure was detected';
