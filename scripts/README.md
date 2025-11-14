# Waverless Database Scripts

This directory contains SQL scripts for managing the Waverless database schema.

## GPU Usage Statistics Schema

### File: `gpu_usage_schema.sql`

Complete schema for GPU usage tracking system, including:

- **gpu_usage_records**: Task-level detailed GPU usage records
- **gpu_usage_statistics_minute**: Minute-level aggregated statistics
- **gpu_usage_statistics_hourly**: Hourly aggregated statistics
- **gpu_usage_statistics_daily**: Daily aggregated statistics

### Usage

#### Create Tables

```bash
mysql -h <host> -P <port> -u <user> -p <database> < scripts/gpu_usage_schema.sql
```

#### Example

```bash
mysql -h sg-cdb-qmk985xh.sql.tencentcdb.com -P 63938 -u wavespeed -p waverless < scripts/gpu_usage_schema.sql
```

### Schema Features

#### 1. Multi-Level Aggregation
- Records → Minute → Hourly → Daily
- Hierarchical aggregation reduces data volume while preserving detail

#### 2. Multi-Scope Support
Each statistics table supports three scope types:
- `global`: System-wide statistics (scope_value = NULL)
- `endpoint`: Per-endpoint statistics (scope_value = endpoint name)
- `spec`: Per-spec statistics (scope_value = spec name)

#### 3. UTC Timezone
All timestamps are stored in UTC format to avoid timezone conversion issues.

#### 4. NULL-Safe Unique Constraints
Uses generated column `scope_value_key = COALESCE(scope_value, '__GLOBAL__')` to handle NULL values in unique constraints.

#### 5. Optimized Indexes
- Time bucket indexes for range queries
- Scope indexes for filtering by endpoint/spec
- Composite unique index for preventing duplicates

### Key Metrics

#### Task Metrics
- `total_tasks`: Total number of tasks completed
- `completed_tasks`: Number of successfully completed tasks
- `failed_tasks`: Number of failed tasks

#### GPU Usage Metrics
- `total_gpu_seconds`: Total GPU card-seconds used (minute-level only)
- `total_gpu_hours`: Total GPU card-hours used
- `avg_gpu_count`: Average GPU count per task
- `max_gpu_count`: Maximum GPU count in any single task

#### Peak Tracking
- `peak_minute`: Minute with highest GPU usage (hourly level)
- `peak_hour`: Hour with highest GPU usage (daily level)
- `peak_gpu_hours`: GPU hours in peak period

### Data Flow

```
Task Completion
    ↓
gpu_usage_records (created by service)
    ↓
Minute Aggregation (every 1 minute)
    ↓
gpu_usage_statistics_minute
    ↓
Hourly Aggregation (every 5 minutes)
    ↓
gpu_usage_statistics_hourly
    ↓
Daily Aggregation (every 1 hour)
    ↓
gpu_usage_statistics_daily
```

### Background Jobs

The system runs the following background jobs:

1. **Minute Aggregation**: Runs every 1 minute
   - Aggregates the previous minute's GPU usage records
   - Creates/updates minute-level statistics

2. **Hourly Aggregation**: Runs every 5 minutes
   - Aggregates minute-level statistics into hourly summaries
   - Tracks peak minute within each hour

3. **Daily Aggregation**: Runs every 1 hour
   - Aggregates hourly statistics into daily summaries
   - Tracks peak hour within each day

4. **Data Cleanup**: Runs every 24 hours
   - Removes old data according to retention policies (see below)

### Data Retention Policy

The system automatically cleans up old data to manage storage:

| Table | Retention Period | Cleanup Job |
|-------|-----------------|-------------|
| `gpu_usage_records` | 90 days | Daily cleanup job |
| `gpu_usage_statistics_minute` | 3 days | Daily cleanup job |
| `gpu_usage_statistics_hourly` | 30 days | Daily cleanup job |
| `gpu_usage_statistics_daily` | Indefinite | Not cleaned |

The cleanup job runs once per day and uses distributed locks to prevent duplicate execution across multiple replicas.

### API Endpoints

- `POST /api/v1/gpu-usage/aggregate?granularity={minute|hourly|daily|all}` - Trigger aggregation
- `POST /api/v1/gpu-usage/backfill` - Backfill historical data
- `GET /api/v1/gpu-usage/minute` - Get minute-level statistics
- `GET /api/v1/gpu-usage/hourly` - Get hourly statistics
- `GET /api/v1/gpu-usage/daily` - Get daily statistics

### Maintenance

#### Automatic Cleanup

The system automatically cleans up old data according to the retention policy (see above). The cleanup job runs once per day.

To manually trigger cleanup or adjust retention periods, you can modify the cleanup logic in:
- Service layer: `internal/service/gpu_usage_service.go` - `CleanupOldStatistics()` method
- Background job: `cmd/jobs.go` - `gpuDataCleanupJob` struct

#### Manual Cleanup (if needed)

If you need to manually clean up data outside the automatic retention policy:

```sql
-- Delete records older than 90 days (same as automatic cleanup)
DELETE FROM gpu_usage_records WHERE completed_at < DATE_SUB(NOW(), INTERVAL 90 DAY);

-- Delete minute-level stats older than 3 days (same as automatic cleanup)
DELETE FROM gpu_usage_statistics_minute WHERE time_bucket < DATE_SUB(NOW(), INTERVAL 3 DAY);

-- Delete hourly stats older than 30 days (same as automatic cleanup)
DELETE FROM gpu_usage_statistics_hourly WHERE time_bucket < DATE_SUB(NOW(), INTERVAL 30 DAY);

-- Daily stats are kept indefinitely by default
```

#### Re-aggregate Statistics

If data becomes inconsistent, you can re-aggregate:

```bash
# Via API
curl -X POST "http://waverless-svc/api/v1/gpu-usage/aggregate?granularity=all"

# Or via kubectl
kubectl -n wavespeed-test exec -it deployment/waverless -- \
  curl -X POST "http://localhost:8080/api/v1/gpu-usage/aggregate?granularity=all"
```

### Troubleshooting

#### Empty Statistics Tables

1. Check if records exist:
   ```sql
   SELECT COUNT(*) FROM gpu_usage_records;
   ```

2. Manually trigger aggregation via Web UI or API

3. Check application logs for errors:
   ```bash
   kubectl -n wavespeed-test logs deployment/waverless
   ```

#### Duplicate Key Errors

If you see duplicate key errors on `uk_time_scope`, verify:
1. The `scope_value_key` generated column exists
2. The unique constraint uses `scope_value_key` not `scope_value`

#### Timezone Issues

All times should be stored in UTC. If you see timezone-related issues:
1. Verify database timezone: `SELECT @@time_zone;`
2. Check that Go code converts times to UTC before querying
3. Ensure frontend displays times in user's local timezone
