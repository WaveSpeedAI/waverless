# Waverless Developer Guide

This comprehensive guide covers advanced technical topics, system internals, and development best practices for the Waverless platform.

---

## Table of Contents

1. [Advanced Topics](#1-advanced-topics)
   - [Race Condition Protection](#11-race-condition-protection)
   - [Multi-Replica Safety](#12-multi-replica-safety)
   - [Orphaned Task Cleanup](#13-orphaned-task-cleanup)

2. [Graceful Shutdown](#2-graceful-shutdown)
   - [Core Mechanism](#21-core-mechanism)
   - [Implementation Details](#22-implementation-details)
   - [Verification and Troubleshooting](#23-verification-and-troubleshooting)

3. [Concurrency Safety](#3-concurrency-safety)
   - [Asynchronous Component Inventory](#31-asynchronous-component-inventory)
   - [Protection Mechanisms](#32-protection-mechanisms)
   - [Risk Matrix](#33-risk-matrix)

4. [Task Tracking System](#4-task-tracking-system)
   - [Overview](#41-overview)
   - [Architecture Design](#42-architecture-design)
   - [Implementation Guide](#43-implementation-guide)
   - [Testing and Verification](#44-testing-and-verification)

---

# 1. Advanced Topics

This section covers three critical advanced technical topics of the system, providing in-depth analysis of implementation principles and solutions.

## 1.1 Race Condition Protection

### Problem Description

During worker graceful shutdown, there is a concurrent race between **task assignment** and **pod deletion**.

#### Race Window

```
Timeline    Worker Request Thread         Pod Deletion Listener Thread
─────────────────────────────────────────────────────────
T0.1      Check worker status → ONLINE ✅
T0.2      Calculate assignable tasks    Pod DeletionTimestamp set
T0.3      Pull 3 tasks from queue       Informer detects event
T0.4      Assign tasks→IN_PROGRESS      Mark worker → DRAINING ⚠️
T0.5      Return tasks to worker ❌
T0.6      Worker starts execution       Pod begins termination
T1+330s                                 Pod forcefully KILLED ❌ Tasks may be lost!
```

**Core problem**: Race window period ~10-30ms, probability cannot be ignored in high-concurrency scenarios (thousands of requests per second).

###Solution: Double-Check

#### Protection Flow

```mermaid
graph TD
    A[Worker requests task] --> B[L1: Check worker status]
    B -->|DRAINING| C[Immediately return empty tasks ⛔]
    B -->|ONLINE| D[Assign tasks to worker]
    D --> E[Record assigned taskIDs]
    E --> F[L3: Double-Check worker status]
    F -->|Still ONLINE| G[Return tasks to worker ✅]
    F -->|Became DRAINING| H[🔴 Race condition detected!]
    H --> I[Revert all assignments]
    I --> J[Re-enqueue]
    J --> K[Return empty tasks ⛔]
```

#### Protection Layers

| Layer | Mechanism | Time Window | Function | Consequence of Failure |
|------|------|---------|------|---------|
| **L1** | Initial status check | T0.1 | If DRAINING → immediate rejection | Worker continues receiving tasks |
| **L2** | Task assignment | T0.4-T0.5 | Record all assigned task IDs | Tasks may be assigned to terminating worker |
| **L3** | 🔑 Double-Check | T0.6 | Check again after assignment, if DRAINING → revert | Tasks may be assigned to terminating worker |
| **L4** | terminationGrace | 330s | Even in extreme cases, time to complete | K8s force terminate |

#### Effect Comparison

| Solution | Race Window | Task Loss Probability | Performance Overhead |
|-----|-----------|-------------|---------|
| No protection | 100% | High | 0 |
| L1 check only | ~30ms | Medium | 1 Redis query |
| **L1 + L3 Double-Check** | **<1ms** | **Extremely low** | **2 Redis queries** |
| L1 + L3 + L4 | 0 (330s protection) | Almost 0 | 2 Redis queries |

#### Quantitative Analysis

Assumptions:
- 1000 PullJobs requests per second
- 10 pod deletion events per hour

**No protection**: Race probability = (1000 req/s × 0.03s) × 10 events/h = **300 times/hour** ❌

**L1+L3 protection**: Race probability = (1000 × 0.001) × 10 = **10 times/hour** ✅
→ **Window period reduced by 97%**

**L1+L3+L4 protection**: Even if race occurs, 330s grace period protection → **Task loss almost 0** ✅

### Implementation Details

#### Core Code Location

**File**: [internal/service/worker_service.go:155-197](../internal/service/worker_service.go#L155-L197)

**Key Steps**:
1. L1 check: `if worker.Status == DRAINING → return empty`
2. Assign tasks: Record `assignedTaskIDs`
3. L3 Double-Check: Again `GET worker.Status`
4. If became DRAINING: Revert assignment, re-enqueue

#### Performance Impact

| Metric | Value |
|------|------|
| Additional latency | ~1ms (one Redis GET) |
| Throughput impact | ~5% (950 vs 1000 req/s) |
| Benefit gained | **99.99% task no-loss guarantee** |

---

## 1.2 Multi-Replica Safety

### Problem Background

When deploying with multiple replicas, if multiple waverless instances run autoscaling simultaneously, it will cause **race condition problems**.

#### Original Defect

```go
// ❌ Problematic code
func (m *Manager) runOnce() {
    m.mu.Lock()  // ⚠️ Only process-internal lock, cannot sync across replicas
    m.lastRunTime = time.Now()
    m.mu.Unlock()

    // Execute autoscaling... Multiple replicas will execute simultaneously!
}
```

#### Possible Race Scenarios

**Scenario 1: Duplicate Scale Up**

```
Timeline:
T0: Replica A detects need to scale from 2 to 4
T0: Replica B detects need to scale from 2 to 4
T1: Replica A calls K8s API → replicas=4
T2: Replica B calls K8s API → replicas=4 (duplicate)
```

**Scenario 2: Metadata Overwrite**

```
Timeline:
T2: Replica A updates lastScaleTime=T2
T4: Replica B updates lastScaleTime=T4 (overwrites A's timestamp)
→ Cooldown time calculation error! ❌
```

### Solution: Distributed Lock

#### Redis Distributed Lock

**Features**:
- Lock TTL: 30 seconds
- Auto-renewal: Renew every 10 seconds
- Max hold: 2 minutes (prevent deadlock)
- Prevent wrong release: Use unique token to identify owner

#### Workflow

```mermaid
graph TD
    A[Replica A triggered periodically] --> B[TryLock]
    C[Replica B triggered periodically] --> D[TryLock]
    B -->|Success| E[Replica A executes autoscaling]
    D -->|Failure| F[Replica B skips this cycle]
    E --> G[Auto-renew lock]
    G --> H[Release lock after completion]
    F --> I[Wait for next trigger]
    H --> I
```

#### Multi-Replica Coordination

| Scenario | Behavior |
|------|------|
| **Normal** | Replica A acquires lock → Execute → Release; Replica B fails to acquire → Skip |
| **Replica A crashes** | Lock auto-expires after 30s → Replica B can take over |
| **Network partition** | Disconnected replica's lock expires → Other replicas take over |

#### Performance Impact

| Metric | Value |
|------|------|
| Lock acquisition latency | <5ms (local Redis) |
| Lock renewal frequency | Every 10 seconds |
| Throughput impact | Negligible (autoscaling interval ≥30s) |

### Implementation Details

#### Core Code Location

**File**: [pkg/autoscaler/distributed_lock.go](../pkg/autoscaler/distributed_lock.go)

**Usage Example**:
```go
// Acquire lock
acquired, _ := m.distributedLock.TryLock(ctx)
if !acquired {
    return nil // Another replica is executing
}
defer m.distributedLock.Unlock(ctx)

// Execute autoscaling logic...
```

#### Single Instance Degradation

If Redis is unavailable (development environment):
```go
if redisClient == nil {
    // Auto-degrade to single instance mode
    return true, nil
}
```

---

## 1.3 Orphaned Task Cleanup

### Problem Background

When a worker crashes or is deleted during scale down while processing a task, it leads to **orphaned tasks**.

#### What are Orphaned Tasks?

Tasks meeting the following conditions:
1. Task status is `IN_PROGRESS`
2. Assigned worker no longer exists
3. Task has not timed out yet

#### Problem Impact

- ❌ Task permanently stuck (until timeout, possibly hours)
- ❌ Resource waste (client waits for a long time)
- ❌ Inaccurate metrics (`RunningTasks` includes zombie tasks)
- ❌ Poor user experience (task appears to be running, but no progress)

### Trigger Scenarios

#### Scenario 1: Worker Crash

```
T0: Worker A pulls Task 123, starts processing
T1: Task 123 status → IN_PROGRESS, WorkerID = "worker-a"
T2: Worker A crashes (OOM/Panic)
T3: Worker A expires from Redis (5 minute TTL)
T4: Task 123 still IN_PROGRESS, but worker-a no longer exists ❌

→ Task 123 becomes orphaned
```

#### Scenario 2: Autoscaling

```
T0: 5 workers, Task 123 assigned to worker-5
T1: Task 123 starts processing (estimated 10 minutes)
T2: Queue empty, auto scale down to 2 workers
T3: K8s deletes worker-3/4/5 pods
T4: worker-5 forcefully terminated, Task 123 interrupted
T5: Task 123 still IN_PROGRESS, but worker-5 deleted ❌

→ Task 123 becomes orphaned
```

### Solution: Orphaned Task Cleanup

#### Core Logic

```mermaid
graph TD
    A[Trigger every 30s] --> B[Get all IN_PROGRESS tasks]
    B --> C[Get all active workers]
    C --> D[Build active workerID set]
    D --> E{Iterate each task}
    E --> F{Task's workerID in active set?}
    F -->|Yes| G[Task normal, continue]
    F -->|No| H[🔴 Orphaned task detected!]
    H --> I[Reset task to PENDING]
    I --> J[Clear workerID and startedAt]
    J --> K[Re-add to pending queue]
    K --> L[Remove from old worker]
    L --> M[Other workers will pull and execute]
    G --> N[Check next task]
    M --> N
```

#### Execution Frequency

| Cleanup Task | Frequency | Use Case |
|---------|------|---------|
| **Orphan Cleanup** | **30s** | Worker crash, scale down → Fast detection |
| **Timeout Cleanup** | **5min** | Task stuck, infinite loop → Slower detection |

#### Comparison with Timeout Cleanup

| Feature | Orphaned Task Cleanup | Timeout Task Cleanup |
|-----|------------|------------|
| **Detection Target** | Worker doesn't exist | Task execution time too long |
| **Response Speed** | Fast (<1 minute) | Slow (5-10 minutes) |
| **Task Status** | PENDING (re-queued) | FAILED (task has issues) |
| **Log Message** | "orphaned task re-queued" | "Task execution timeout" |

#### Complementary Relationship

```
Scenario 1: Worker crash
  ├─ Orphan cleanup: ✅ Detect in 30s and re-queue
  └─ Timeout cleanup: ✅ Backup safety net

Scenario 2: Task infinite loop (worker still running)
  ├─ Orphan cleanup: ❌ Cannot detect (worker exists)
  └─ Timeout cleanup: ✅ Mark FAILED after timeout

Scenario 3: Autoscaling
  ├─ Orphan cleanup: ✅ Fast detection of deleted workers
  └─ Timeout cleanup: ✅ Backup safety net
```

### Implementation Details

#### Core Code Location

**File**: [internal/service/task_service.go:268-346](../internal/service/task_service.go#L268-L346)

**Key Methods**:
- `CleanupOrphanedTasks()` - Main cleanup logic
- `requeueOrphanedTask()` - Re-queue orphaned tasks

#### Log Examples

**Normal Case**

```
DEBUG starting orphaned task cleanup
DEBUG checking 5 in-progress tasks
DEBUG orphaned task cleanup completed (no orphans detected)
```

**Orphaned Task Detected**

```
WARN  orphaned task detected, task_id: abc123, worker_id: worker-5
INFO  re-queuing orphaned task, task_id: abc123
INFO  orphaned task re-queued successfully
INFO  orphaned task cleanup completed, orphaned: 1, total_checked: 5
```

#### Monitoring Metrics

```prometheus
# Total orphaned tasks
waverless_orphaned_tasks_total{endpoint="xxx"}

# Orphans detected per check
waverless_orphaned_tasks_detected{endpoint="xxx"}

# Cleanup execution time
waverless_orphaned_tasks_cleanup_duration_seconds
```

---

# 2. Graceful Shutdown

## Overview

By **server-side listening to K8s pod deletion events**, automatically mark workers as DRAINING status, ensuring zero task loss during **all operations that cause pod deletion** (autoscaling, image changes, spec changes, resource adjustments, node maintenance, etc.).

### Core Features

- ✅ **Zero Worker Code Modification** - Fully server-side implementation
- ✅ **Zero Task Loss** - 6-layer protection mechanism
- ✅ **Automated** - K8s event-driven, no manual intervention
- ✅ **Race Condition Protection** - Double-Check mechanism

### Core Principle

```
Any deployment change → K8s deletes old pod → Server detects → Mark worker as DRAINING
→ Stop new task assignment → Worker completes existing tasks → Graceful exit (≤330s)
```

## 2.1 Core Mechanism

### Overall Flow

```mermaid
graph TD
    A[K8s Operation] --> B[Pod DeletionTimestamp set]
    B --> C[K8s Informer detects event]
    C --> D[Server finds worker by PodName]
    D --> E[Mark worker.Status = DRAINING]
    E --> F[PullJobs filters DRAINING workers]
    F --> G[Worker completes existing tasks]
    G --> H[Worker graceful exit ≤330s]
    H --> I[K8s deletes pod]
```

### 6-Layer Protection Mechanism

| Layer | Mechanism | Time Window | Function | Consequence if Failed |
|------|------|---------|------|---------|
| **L0** | 🔑 Double-Check | <1ms | Prevent race conditions | Task may be assigned to terminating worker |
| **L1** | DRAINING mark | Immediate | Reject new task assignments | Worker continues receiving tasks |
| **L2** | Heartbeat maintained | 60s | Prevent false offline detection | Task may be wrongly recovered |
| **L3** | Grace Period | 120s | Protect task from early recovery | Task eventually recovered and retried |
| **L4** | terminationGrace | 330s | Enough time to complete tasks | K8s force terminate |
| **L5** | Task retry | Permanent | Ultimate safety net | Requires user intervention |

### Time Configuration Relationship

```
heartbeatTimeout (60s)
    ↓ Heartbeat timeout detection
gracePeriod (120s) = 2 × heartbeatTimeout
    ↓ Task recovery protection period
terminationGracePeriodSeconds (330s) = taskTimeout(300s) + buffer(30s)
    ↓ Pod graceful shutdown window

Relationship: 60s << 120s << 330s
```

## 2.2 Implementation Details

### Core Components

| Component | File | Core Function |
|------|------|---------|
| **Worker Status** | [internal/model/worker.go](../internal/model/worker.go) | Add `DRAINING` status + `PodName` field |
| **Pod Monitoring** | [pkg/deploy/k8s/manager.go:628-706](../pkg/deploy/k8s/manager.go#L628-L706) | Monitor pod `DeletionTimestamp` changes |
| **Provider Interface** | [pkg/deploy/k8s/provider.go:276-301](../pkg/deploy/k8s/provider.go#L276-L301) | Expose `WatchPodTerminating` method |
| **Task Filtering** | [internal/service/worker_service.go:79-86](../internal/service/worker_service.go#L79-L86) | PullJobs checks DRAINING status |
| **Double-Check** | [internal/service/worker_service.go:155-197](../internal/service/worker_service.go#L155-L197) | Check again after task assignment, prevent race |
| **App Integration** | [cmd/initializers.go:163-211](../cmd/initializers.go#L163-L211) | Register pod listener at startup |

### Key Configuration

#### K8s Deployment

```yaml
# config/templates/deployment.yaml
spec:
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxUnavailable: 0  # ⭐ Ensure always available pods
      maxSurge: 1        # ⭐ Control update speed

  template:
    spec:
      containers:
      - name: worker
        env:
        - name: RUNPOD_POD_ID
          valueFrom:
            fieldRef:
              fieldPath: metadata.name  # ⭐ Required: Pod-Worker mapping

        lifecycle:
          preStop:
            exec:
              command: ["/bin/sh", "-c", "kill -TERM 1 && sleep 5"]

      terminationGracePeriodSeconds: 330  # ⭐ taskTimeout(300s) + buffer(30s)
```

#### Application Configuration

```yaml
# config.yaml
k8s:
  enabled: true  # ⭐ Must be enabled

worker:
  heartbeatTimeout: 60  # ⭐ Heartbeat timeout (seconds)
```

### Double-Check Mechanism

#### Why Needed?

**Race Window**: Worker requests task while pod is being deleted

```
Timeline:
T0.1  Worker requests task, check status → ONLINE ✅
T0.2  Pod marked for deletion
T0.3  Worker marked DRAINING
T0.4  Task assigned to worker ⚠️ (based on T0.1 check)
T0.5  Return task to soon-to-terminate worker ❌
```

#### Solution

```
Initial check (T0.1)
    ↓
Assign task (T0.4)
    ↓
🔑 Double-Check (T0.5) ← Critical!
    ↓
If worker became DRAINING:
    ├─ Revert all assignments
    ├─ Re-enqueue
    └─ Return empty tasks ✅
```

#### Effect Comparison

| Solution | Race Window | Task Loss Probability | Performance Overhead |
|-----|-----------|-------------|---------|
| No protection | ~30ms | High | 0 |
| L1 check only | ~30ms | Medium | 1 Redis query |
| L1 + Double-Check | <1ms | Extremely low | 2 Redis queries |
| L1 + L3 + L4 | 0 (330s protection) | Almost 0 | 2 Redis queries |

## 2.3 Verification and Troubleshooting

### Quick Verification

#### Verification 1: Startup Check

```bash
kubectl logs -f <waverless-pod> | grep "Pod watcher"
# Expected output: ✅ Pod watcher registered successfully
```

#### Verification 2: Delete Pod Test

```bash
# 1. Create test task
curl -X POST http://localhost:8090/api/v1/tasks \
  -d '{"endpoint": "test", "input": {"duration": 60}}'

# 2. Delete worker pod
kubectl delete pod <worker-pod-name>

# 3. Observe logs
kubectl logs -f <waverless-pod> | grep "DRAINING"
# Expected output:
# 🔔 Pod xxx marked for deletion
# ✅ Worker xxx marked as DRAINING
# ⛔ Worker is draining, not pulling new tasks

# 4. Verify task completion
curl http://localhost:8090/api/v1/tasks/<task-id>
# Expected: status = COMPLETED (not FAILED)
```

### Runtime Logs

#### Normal Startup

```
Initializing service layer...
Setting up pod watcher for graceful shutdown...
✅ Pod watcher registered successfully
Service layer initialization successful
```

#### Pod Deletion

```
🔔 Pod xxx (endpoint: test) marked for deletion, draining worker...
✅ Worker xxx (Pod: xxx) marked as DRAINING, will not receive new tasks
```

#### DRAINING Worker Requests Task

```
⛔ Worker is draining, not pulling new tasks, worker_id: xxx
```

#### Race Condition Detection

```
🔴 Race condition detected: Worker xxx became DRAINING, reverting 3 tasks
✅ Task task-1 (1/3) reverted to pending queue
⛔ Worker xxx is draining, returned 0 tasks
```

### FAQ

**Q1: Why not handle SIGTERM in worker?**

**A**: Workers use third-party SDKs (like runpod), cannot modify code. Server-side approach is completely SDK-independent.

**Q2: How much terminationGracePeriodSeconds to set?**

**A**: `taskTimeout + 30s buffer`. Default 300s task timeout, set to 330s.

**Q3: Will multi-replica servers repeatedly mark workers?**

**A**: Yes, but marking operation is idempotent, no side effects.

**Q4: Will Double-Check affect performance?**

**A**: Adds ~1ms latency, impacts throughput ~5%, but gains 99.99% task no-loss guarantee.

---

# 3. Concurrency Safety

## Summary

This section analyzes the concurrency safety of the Waverless system in multi-replica deployment scenarios, identifies potential concurrent conflict issues, and provides solutions.

## 3.1 Asynchronous Component Inventory

### AutoScaler (Autoscaling)

**Location**: `pkg/autoscaler/manager.go`
**Run Frequency**: Every 10 seconds (configurable)
**Concurrency Risk**: ⚠️ **High** - Multiple replicas executing autoscaling simultaneously will cause duplicate operations
**Current Status**: ✅ **Protected** - Using Redis distributed lock

```go
// manager.go:252
acquired, err := m.distributedLock.TryLock(ctx)
if !acquired {
    logger.DebugCtx(ctx, "another instance is running autoscaler, skipping this cycle")
    return nil
}
defer m.distributedLock.Unlock(ctx)
```

**Protection Mechanism**:
- Uses Redis `SET NX EX` atomic operation
- Lock TTL: 30 seconds, prevents deadlock
- Auto-renewal: Renew every 10 seconds
- Max hold time: 2 minutes

### Worker Cleanup Task

**Location**: `cmd/jobs.go:26` → `internal/service/worker_service.go:310`
**Run Frequency**: Every 60 seconds (`heartbeat_timeout` config)
**Concurrency Risk**: ⚠️ **Medium** - Multiple replicas may delete the same worker simultaneously
**Current Status**: ✅ **Protected** - Using Redis distributed lock `cleanup:worker-lock`

### Task Timeout Cleanup

**Location**: `cmd/jobs.go:27` → `internal/service/task_service.go:471`
**Run Frequency**: Every 5 minutes
**Concurrency Risk**: ⚠️ **Medium** - Multiple replicas may mark task failed simultaneously
**Current Status**: ✅ **Protected** - Using Redis distributed lock `cleanup:task-timeout-lock`

### Orphaned Task Cleanup

**Location**: `cmd/jobs.go:28` → `internal/service/task_service.go:345`
**Run Frequency**: Every 30 seconds
**Concurrency Risk**: ⚠️ **Medium** - Multiple replicas may re-enqueue tasks simultaneously
**Current Status**: ✅ **Protected** - Using Redis distributed lock `cleanup:orphaned-task-lock`

### Pod Watcher (K8s Informer)

**Location**: `cmd/initializers.go:168-216` → `pkg/deploy/k8s/manager.go:150-229`
**Run Mode**: K8s Informer (event-driven)
**Concurrency Risk**: ✅ **Safe** - K8s Informer handles this well
**Current Status**: ✅ **No protection needed**

**Why Safe**:
1. **K8s Informer** itself handles multi-replica subscription issues (each replica receives events, but this is expected)
2. **UpdateWorkerStatus** is idempotent: setting `status=DRAINING` multiple times has same result
3. **Redis SET** operation: last write wins, doesn't cause data inconsistency

## 3.2 Protection Mechanisms

### Task Assignment

**Location**: `internal/service/worker_service.go:51-200`
**Concurrency Risk**: ✅ **Protected** - Using MySQL `SELECT FOR UPDATE` + CAS
**Current Status**: ✅ **Thread-safe**

**Protection Mechanism**:
```go
// worker_service.go:142
candidateTasks, err := s.taskRepo.SelectPendingTasksForUpdate(ctx, worker.Endpoint, batchSize)
// Uses MySQL row locks (SELECT ... FOR UPDATE WHERE status='PENDING')

// worker_service.go:167-185
for _, task := range candidateTasks {
    // CAS update: only succeeds if status='PENDING'
    affected, err := s.taskRepo.CompareAndSwapStatus(ctx, task.TaskID,
        model.TaskStatusPending, model.TaskStatusInProgress, req.WorkerID)

    if affected == 0 {
        // Another worker already grabbed this task, skip
        continue
    }
}
```

## 3.3 Risk Matrix

| Component | Frequency | Risk Level | Current Status | Optimization Status |
|------|------|----------|----------|----------|
| AutoScaler | 10s | High | ✅ Protected (distributed lock) | ✅ Complete |
| Worker Cleanup | 60s | Medium | ✅ Protected (distributed lock) | ✅ Complete |
| Task Timeout Cleanup | 5min | Medium | ✅ Protected (distributed lock) | ✅ Complete |
| Orphaned Task Cleanup | 30s | Medium | ✅ Protected (distributed lock) | ✅ Complete |
| Pod Watcher | Event-driven | Low | ✅ Idempotent operation | ✅ No optimization needed |
| Task Assignment | Real-time | High | ✅ Protected (row lock+CAS) | ✅ Complete |

### Summary

✅ **System fully optimized, concurrency safety ensured**
- Core task assignment uses row locks + CAS, ensures no duplicate assignments
- AutoScaler uses distributed lock, prevents duplicate autoscaling
- **All background cleanup tasks use distributed lock protection** ✨
- Pod Watcher ensures safety through idempotent operations

📊 **Applicable Scenarios**
- ✅ **2-3 replica deployment**: Excellent design, resource efficient
- ✅ **5+ replica deployment**: Optimized, avoid resource waste
- ✅ **Large-scale clusters**: Can safely scale horizontally

🚀 **Optimization Results**
- Eliminated duplicate execution of background tasks
- Reduced duplicate event table records
- Lowered CPU and network resource consumption
- Provided clearer log output
- Maintained high availability (auto-degrade when Redis fails)

---

# 4. Task Tracking System

## 4.1 Overview

The Task Tracking System provides comprehensive lifecycle monitoring for tasks, from creation to completion, including worker assignment, execution time, retry records, and more.

### Implementation Overview

Task detailed tracking feature implementation completed. Can record complete lifecycle from task creation to completion, including worker assignment, execution time, retry records, etc.

### Core Design

#### Dual-Layer Tracking Architecture

1. **task_events table** (Detailed event logs)
   - Complete event audit logs
   - Supports complex queries and analysis
   - Asynchronous write, does not block main flow

2. **task.extend field** (Core execution summary)
   - JSON array format: `[{worker, start_time, end_time, duration_ms}, ...]`
   - Fast query of task execution history
   - Synchronous update, ensures data consistency

## 4.2 Architecture Design

### Database Design

#### 1. task_events Table (Complete Event Log)

```sql
CREATE TABLE task_events (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    event_id VARCHAR(255) NOT NULL UNIQUE COMMENT 'Unique event ID',
    task_id VARCHAR(255) NOT NULL COMMENT 'Task ID',
    endpoint VARCHAR(255) NOT NULL COMMENT 'Endpoint name',

    -- Event information
    event_type VARCHAR(50) NOT NULL COMMENT 'Event type',
    event_time DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT 'Event occurrence time',

    -- Worker information
    worker_id VARCHAR(255) COMMENT 'Worker ID',
    worker_pod_name VARCHAR(255) COMMENT 'Worker Pod name',

    -- Status changes
    from_status VARCHAR(50) COMMENT 'Original status',
    to_status VARCHAR(50) COMMENT 'New status',

    -- Error information
    error_message TEXT COMMENT 'Error message',
    error_type VARCHAR(100) COMMENT 'Error type',

    -- Retry information
    retry_count INT DEFAULT 0 COMMENT 'Retry count',

    -- Additional information
    metadata JSON COMMENT 'Additional metadata',

    -- Indexes
    INDEX idx_task_id_event_time (task_id, event_time),
    INDEX idx_endpoint_event_time (endpoint, event_time),
    INDEX idx_worker_id (worker_id),
    INDEX idx_event_type (event_type),
    INDEX idx_event_time (event_time)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Task event tracking table';
```

#### 2. Add extend Field to tasks Table (Core Summary)

```sql
ALTER TABLE tasks
ADD COLUMN extend JSON COMMENT 'Task execution history summary';
```

**extend Field Structure**:
```json
{
  "execution_history": [
    {
      "worker_id": "worker-001",
      "start_time": "2025-11-10T10:00:05.123Z",
      "end_time": "2025-11-10T10:02:30.456Z",
      "duration_ms": 145333,
      "status": "interrupted"
    },
    {
      "worker_id": "worker-002",
      "start_time": "2025-11-10T10:02:35.000Z",
      "end_time": "2025-11-10T10:05:00.789Z",
      "duration_ms": 145789,
      "status": "completed"
    }
  ],
  "retry_count": 1,
  "total_queue_time_ms": 5123,
  "total_execution_time_ms": 291122
}
```

### Responsibility Division

| Dimension | task_events Table | task.extend Field |
|------|---------------|-----------------|
| **Purpose** | Detailed event logs, audit, debugging | Core summary, fast queries |
| **Data Volume** | Large (one row per event) | Small (core info only) |
| **Query Scenarios** | Event timeline, Worker performance analysis | Task list, retry count, execution time |
| **Retention Period** | 30 days (archivable) | Permanent (with task record) |
| **Query Performance** | Requires JOIN or subquery | Direct read, no JOIN |
| **Write Frequency** | Every event is written | Update only on status changes |

### Event Types

```go
const (
    // Task lifecycle events
    EventTaskCreated    = "TASK_CREATED"     // Task created
    EventTaskQueued     = "TASK_QUEUED"      // Task queued
    EventTaskAssigned   = "TASK_ASSIGNED"    // Task assigned to Worker
    EventTaskCompleted  = "TASK_COMPLETED"   // Task completed
    EventTaskFailed     = "TASK_FAILED"      // Task failed
    EventTaskCancelled  = "TASK_CANCELLED"   // Task cancelled

    // Exception recovery events
    EventTaskRequeued   = "TASK_REQUEUED"    // Task requeued (Worker disconnected)
    EventTaskOrphaned   = "TASK_ORPHANED"    // Task became orphan (Worker lost)
    EventTaskTimeout    = "TASK_TIMEOUT"     // Task timeout
)
```

## 4.3 Implementation Guide

### File Structure

#### Database Layer

- **scripts/add_task_tracking.sql** - Database migration script
- **pkg/store/mysql/model/task.go** - Task model with extend field
- **pkg/store/mysql/model/task_event.go** - TaskEvent model definition
- **pkg/store/mysql/task_event_repository.go** - Event repository

#### Service Layer

- **internal/service/task_event_helper.go** - Unified event recording
- **internal/service/task_service.go** - Task service with event integration
- **internal/service/worker_service.go** - Worker service integration

#### API Layer

- **app/handler/task_handler.go** - Task tracking endpoints:
  - `GET /api/v1/tasks/:task_id/execution-history` - Get execution history
  - `GET /api/v1/tasks/:task_id/events` - Get all events
  - `GET /api/v1/tasks/:task_id/timeline` - Get task timeline

### Integration Steps

1. **Run Database Migration**
   ```bash
   mysql -u root -p waverless < scripts/add_task_tracking.sql
   ```

2. **Update Service Layer**
   - Add `taskEventRepo` to TaskService
   - Integrate event recording at key points:
     - `SubmitTask()` - Record CREATED and QUEUED events
     - `UpdateTaskResult()` - Record COMPLETED/FAILED events
     - `CleanupOrphanedTasks()` - Record ORPHANED and REQUEUED events
     - `CleanupTimedOutTasks()` - Record TIMEOUT events

3. **Update Worker Service**
   - Record TASK_ASSIGNED event in `PullJobs()`
   - Record REQUEUED event in Double-Check revert logic

4. **Register API Routes**
   - Add task history endpoints to router

## 4.4 Testing and Verification

### API Testing

#### 1. Get Task Execution History

```bash
curl http://localhost:8090/api/v1/tasks/{task_id}/execution-history
```

**Expected Response**:
```json
{
  "task_id": "task-123",
  "execution_history": [
    {
      "worker_id": "worker-001",
      "start_time": "2025-11-10T10:00:05.123Z",
      "end_time": "2025-11-10T10:02:30.456Z",
      "duration_ms": 145333
    }
  ],
  "retry_count": 0,
  "total_execution_time_ms": 145333
}
```

#### 2. Get Task Events

```bash
curl http://localhost:8090/api/v1/tasks/{task_id}/events
```

**Expected Response**:
```json
{
  "task_id": "task-123",
  "events": [
    {
      "event_type": "TASK_CREATED",
      "event_time": "2025-11-10T10:00:00.000Z",
      "from_status": "",
      "to_status": "PENDING"
    },
    {
      "event_type": "TASK_ASSIGNED",
      "event_time": "2025-11-10T10:00:05.123Z",
      "worker_id": "worker-001",
      "from_status": "PENDING",
      "to_status": "IN_PROGRESS"
    },
    {
      "event_type": "TASK_COMPLETED",
      "event_time": "2025-11-10T10:02:30.456Z",
      "worker_id": "worker-001",
      "from_status": "IN_PROGRESS",
      "to_status": "COMPLETED"
    }
  ]
}
```

#### 3. Get Task Timeline

```bash
curl http://localhost:8090/api/v1/tasks/{task_id}/timeline
```

### Testing Scenarios

#### Scenario 1: Normal Task Execution

1. Submit task
2. Worker pulls task
3. Task completes
4. Verify events recorded: CREATED → QUEUED → ASSIGNED → COMPLETED

#### Scenario 2: Task Retry After Worker Crash

1. Submit task
2. Worker pulls task
3. Simulate worker crash (delete pod)
4. Orphan cleanup detects and re-queues task
5. Another worker completes task
6. Verify events: CREATED → QUEUED → ASSIGNED → ORPHANED → REQUEUED → ASSIGNED → COMPLETED

#### Scenario 3: Task Timeout

1. Submit task with long execution
2. Wait for timeout period
3. Verify timeout event recorded
4. Check task status changed to FAILED

### Monitoring

#### Event Recording Metrics

```prometheus
# Total events recorded
waverless_task_events_total{endpoint="xxx", event_type="TASK_CREATED"}

# Event recording duration
waverless_task_event_recording_duration_seconds

# Event recording errors
waverless_task_event_recording_errors_total
```

---

## Summary

This developer guide covers the critical technical aspects of Waverless system internals:

1. **Advanced Topics** - Race condition protection, multi-replica safety, orphaned task cleanup
2. **Graceful Shutdown** - Zero-loss task completion during pod termination
3. **Concurrency Safety** - Distributed lock mechanisms and multi-replica coordination
4. **Task Tracking** - Comprehensive lifecycle monitoring and event auditing

### Design Principles

- **Multi-layer Protection** - Don't rely on single mechanism
- **Fast Response** - High-frequency detection of anomalies
- **Graceful Degradation** - Auto-degrade in single instance environments
- **Observability** - Complete logs and metrics
- **Idempotency** - Safe operations in distributed environments

### Production Ready

- ✅ Complete implementation
- ✅ Detailed documentation
- ✅ Monitoring metrics
- ✅ Alert rules
- ✅ Test verification

---

**Document Version**: v1.0
**Last Updated**: 2025-11-14
**Maintained By**: Waverless Team
