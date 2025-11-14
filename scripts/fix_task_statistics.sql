-- ================================================================
-- Fix task_statistics table structure and data
--
-- ISSUE:
-- 1. Multiple global records due to NULL in unique constraint
--    MySQL's UNIQUE KEY allows multiple NULL values!
-- 2. Negative numbers in statistics due to:
--    - Table truncated (all counts = 0)
--    - Task status changes from IN_PROGRESS -> COMPLETED
--    - Incremental update does: in_progress_count = 0 - 1 = -1
--
-- ROOT CAUSE:
-- 1. The unique constraint (scope_type, scope_value) does NOT prevent
--    multiple rows with scope_value=NULL, causing duplicate global records
-- 2. Incremental updates don't handle missing records properly
--
-- SOLUTION:
-- 1. Drop old unique constraint on (scope_type, scope_value)
-- 2. Update all NULL scope_value to 'global' string
-- 3. Add new unique constraint on (scope_type, scope_value)
-- 4. Truncate and refresh all statistics
--
-- USAGE:
--   mysql -h 127.0.0.1 -u root -pYOUR_PASSWORD waverless < fix_task_statistics.sql
--
--   Then restart waverless service or call:
--   curl -X POST http://localhost:8090/api/v1/statistics/refresh
-- ================================================================

USE waverless;

-- Step 1: Drop old unique constraint (it doesn't work for NULL values)
ALTER TABLE task_statistics DROP INDEX IF EXISTS uk_scope;

-- Step 2: Update all NULL scope_value to 'global' string
UPDATE task_statistics SET scope_value = 'global' WHERE scope_value IS NULL;

-- Step 3: Clean up duplicate records before adding unique constraint
-- Keep only the most recent record for each (scope_type, scope_value) combination
DELETE t1 FROM task_statistics t1
INNER JOIN task_statistics t2
WHERE t1.scope_type = t2.scope_type
  AND t1.scope_value = t2.scope_value
  AND t1.id < t2.id;

-- Step 4: Add new unique constraint on (scope_type, scope_value)
-- This will now properly enforce uniqueness for global records
ALTER TABLE task_statistics
ADD UNIQUE KEY uk_scope (scope_type, scope_value);

-- Step 5: Truncate table to fix negative numbers and inconsistent data
-- This will clear all statistics, which will be regenerated via API call
TRUNCATE TABLE task_statistics;

-- ================================================================
-- VERIFICATION QUERIES (Optional)
-- ================================================================

-- Verify table structure
SHOW CREATE TABLE task_statistics;

-- Verify data is clean (should return 0 rows after truncate)
SELECT * FROM task_statistics ORDER BY id;

-- Verify unique index exists on (scope_type, scope_value)
SHOW INDEX FROM task_statistics WHERE Key_name = 'uk_scope';

-- ================================================================
-- NEXT STEPS
-- ================================================================
-- 1. Restart waverless service to load new code
-- 2. Call API to regenerate statistics:
--    curl -X POST http://localhost:8090/api/v1/statistics/refresh
--
-- Or just restart the service and statistics will be auto-populated
-- on first query to /api/v1/statistics/overview
-- ================================================================
