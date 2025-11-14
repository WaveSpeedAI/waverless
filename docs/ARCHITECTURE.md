# Waverless System Architecture Design

## Overview

Waverless is a **Serverless task scheduling system** that provides RunPod API-compatible worker management, task distribution, autoscaling, and other features.

### Core Features

- ✅ **RunPod Compatible API** - Compatible with runpod SDK, zero-code migration
- ✅ **Autoscaling** - Intelligently adjusts replica count based on task queue and resource usage
- ✅ **Graceful Shutdown** - Zero task loss during rolling updates and scale down
- ✅ **Multi-tenant** - Isolate different applications through endpoints
- ✅ **High Availability** - Supports multi-replica deployment, no single point of failure

---

## System Architecture

### Overall Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────┐
│                         Client Layer                             │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐        │
│  │  Web UI  │  │ REST API │  │  Worker  │  │  Webhook │        │
│  └─────┬────┘  └─────┬────┘  └─────┬────┘  └─────┬────┘        │
└────────┼─────────────┼─────────────┼─────────────┼──────────────┘
         │             │             │             │
         ▼             ▼             ▼             ▼
┌─────────────────────────────────────────────────────────────────┐
│                       API Server Layer                           │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │  Gin HTTP Server (Port 8090)                             │   │
│  │  ┌─────────────┐  ┌─────────────┐  ┌──────────────┐     │   │
│  │  │   Router    │──│   Handler   │──│   Service    │     │   │
│  │  └─────────────┘  └─────────────┘  └──────────────┘     │   │
│  └──────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
         │             │             │             │
         ▼             ▼             ▼             ▼
┌─────────────────────────────────────────────────────────────────┐
│                      Business Logic Layer                        │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐          │
│  │Task Service  │  │Worker Service│  │Endpoint Svc  │          │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘          │
│         │                  │                  │                  │
│  ┌──────┴──────────────────┴──────────────────┴──────┐          │
│  │         Autoscaler Manager (Background)           │          │
│  │  • Metrics Collection                              │          │
│  │  • Resource Calculation                            │          │
│  │  • Scale Decision Making                           │          │
│  │  • K8s Deployment Control                          │          │
│  └────────────────────────────────────────────────────┘          │
└─────────────────────────────────────────────────────────────────┘
         │             │             │             │
         ▼             ▼             ▼             ▼
┌─────────────────────────────────────────────────────────────────┐
│                      Infrastructure Layer                        │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐          │
│  │   MySQL      │  │    Redis     │  │  K8s Client  │          │
│  │ (Metadata &  │  │  (Queue &    │  │  (Deployment │          │
│  │  Tasks)      │  │   Workers)   │  │   Control)   │          │
│  └──────────────┘  └──────────────┘  └──────┬───────┘          │
└─────────────────────────────────────────────┼──────────────────┘
                                               │
                                               ▼
┌─────────────────────────────────────────────────────────────────┐
│                     K8s Cluster Layer                            │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │  Deployments (Worker Pods)                               │   │
│  │  ┌────────┐  ┌────────┐  ┌────────┐  ┌────────┐         │   │
│  │  │Worker 1│  │Worker 2│  │Worker 3│  │  ...   │         │   │
│  │  │endpoint│  │endpoint│  │endpoint│  │        │         │   │
│  │  │  = A   │  │  = A   │  │  = B   │  │        │         │   │
│  │  └────────┘  └────────┘  └────────┘  └────────┘         │   │
│  └──────────────────────────────────────────────────────────┘   │
│                                                                  │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │  K8s Informers (Event Watching)                          │   │
│  │  • Pod Events → Worker DRAINING                          │   │
│  │  • Deployment Events → Replica Status                    │   │
│  └──────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
```

---

## Core Components

### 1. API Server

**Responsibilities**: HTTP request handling, routing distribution, authentication and authorization

**Tech Stack**:
- Gin framework
- RESTful API
- RunPod compatible endpoints

**Key Endpoints**:
- `/api/v1/tasks` - Task management
- `/api/v1/workers` - Worker management
- `/api/v1/endpoints` - Endpoint configuration
- `/api/v1/autoscaler` - Autoscaling control
- `/v2/{endpoint}/*` - RunPod compatible endpoints

### 2. Task Service

**Responsibilities**: Task lifecycle management

**Core Functions**:
- Task creation and status management
- Task assignment and scheduling
- Task timeout detection
- Orphaned task cleanup

**Data Flow**:
```
Create task → MySQL storage → Redis queue → Worker pull → Execute → Write result
```

### 3. Worker Service

**Responsibilities**: Worker registration, heartbeat, task assignment

**Core Functions**:
- Worker registration and status management
- Heartbeat detection and offline cleanup
- Task assignment (with Double-Check)
- DRAINING status management

**Worker State Machine**:
```
ONLINE → BUSY → ONLINE
   ↓
DRAINING → (Complete tasks) → Cleanup
   ↓
OFFLINE → Cleanup + Task recovery
```

### 4. Endpoint Service

**Responsibilities**: Multi-tenant management, configuration management

**Core Functions**:
- Endpoint metadata management
- Autoscaling configuration
- Statistics aggregation

### 5. Autoscaler

**Responsibilities**: Autoscaling decision-making and execution

**Architecture**:
```
┌────────────────────────────────────────────┐
│          Autoscaler Manager                │
│                                            │
│  ┌──────────────────────────────────┐     │
│  │  Control Loop (every 30s)        │     │
│  │  1. Collect Metrics              │     │
│  │  2. Make Decisions               │     │
│  │  3. Execute Scaling              │     │
│  └──────────────────────────────────┘     │
│         ↓          ↓          ↓            │
│  ┌──────────┐ ┌────────┐ ┌──────────┐     │
│  │ Metrics  │ │Decision│ │ Executor │     │
│  │Collector │ │ Maker  │ │          │     │
│  └──────────┘ └────────┘ └──────────┘     │
└────────────────────────────────────────────┘
         │              │            │
         ▼              ▼            ▼
    EndpointSvc   ResourceCalc   K8s Client
```

**Decision Algorithm**:
1. **Scale up trigger**: `pendingTasks >= scaleUpThreshold`
2. **Scale down trigger**: `idleTime >= scaleDownIdleTime`
3. **Resource check**: Whether cluster resources are sufficient
4. **Priority sorting**: High-priority endpoints get resources first
5. **Cooldown period**: Avoid frequent scaling

**See**: [Autoscaling Design](autoscaling-design.md)

### 6. K8s Integration

**Responsibilities**: K8s resource management and event monitoring

**Core Functions**:
- Deployment CRUD
- Pod event listening (Informer)
- Worker graceful shutdown

**Informer Mechanism**:
```
K8s API Server
    ↓ (Watch)
Informer Cache
    ↓ (Event)
Event Handler
    ↓
Business Logic
```

**Graceful Shutdown Flow**:
```
Pod deletion → Informer detects → Mark Worker DRAINING → Stop new tasks → Complete existing tasks → Exit
```

**See**: [Graceful Shutdown Design](graceful-shutdown-design.md)

---

## Data Model

### Core Entities

#### Task
```go
type Task struct {
    ID          string                 // Task ID
    Endpoint    string                 // Belonging endpoint
    Status      TaskStatus             // Status: PENDING, IN_PROGRESS, COMPLETED, FAILED
    Input       map[string]interface{} // Input parameters
    Output      map[string]interface{} // Output result
    WorkerID    string                 // Executing worker ID
    CreatedAt   time.Time
    StartedAt   *time.Time
    CompletedAt *time.Time
}
```

#### Worker
```go
type Worker struct {
    ID             string       // Worker ID
    Endpoint       string       // Belonging endpoint
    Status         WorkerStatus // Status: ONLINE, BUSY, DRAINING, OFFLINE
    PodName        string       // K8s Pod name
    Concurrency    int          // Max concurrency
    JobsInProgress []string     // List of tasks being executed
    LastHeartbeat  time.Time    // Last heartbeat time
}
```

#### Endpoint
```go
type Endpoint struct {
    Name             string    // Unique identifier
    DisplayName      string    // Display name
    Image            string    // Docker image
    Replicas         int       // Desired replica count
    SpecName         string    // Spec name
    AutoscalerConfig          // Autoscaling configuration
}

type AutoscalerConfig struct {
    MinReplicas       int    // Minimum replica count
    MaxReplicas       int    // Maximum replica count
    ScaleUpThreshold  int    // Scale up threshold (queued task count)
    ScaleDownIdleTime int    // Scale down idle time (seconds)
    Priority          int    // Priority (0-100)
    AutoscalerEnabled string // Force enable/disable ("enabled"/"disabled"/"")
}
```

### Data Storage

| Data Type | Storage | Reason |
|---------|------|------|
| **Task Metadata** | MySQL | Persistence, transactions, queries |
| **Worker Info** | Redis | Temporary state, fast access, TTL |
| **Task Queue** | Redis List | High-performance queue, atomic operations |
| **Endpoint Configuration** | MySQL | Persistent configuration |

### Data Flow

#### Task Creation Flow
```
Client → POST /api/v1/tasks
    ↓
TaskService.CreateTask()
    ↓
MySQL.Insert(task) ← Persistence
    ↓
Redis.LPUSH(pending:<endpoint>, taskID) ← Enqueue
    ↓
Return task ID
```

#### Task Assignment Flow
```
Worker → POST /v2/{endpoint}/job-take
    ↓
WorkerService.PullJobs()
    ↓
L1: Check Worker.Status (DRAINING?)
    ↓ NO
Redis.RPOP(pending:<endpoint>) ← Dequeue
    ↓
MySQL.Update(task, status=IN_PROGRESS)
    ↓
L0: Double-Check Worker.Status (DRAINING?)
    ↓ NO
Return task list
    ↓ (If YES: Revert assignment, re-enqueue)
```

---

## Technology Stack

| Component | Technology | Version | Reason |
|------|------|------|------|
| **Language** | Go | 1.21+ | High performance, concurrency-friendly, K8s ecosystem |
| **Web Framework** | Gin | v1.9+ | High performance, easy to use, active community |
| **Database** | MySQL | 8.0+ | Transaction support, mature and stable |
| **Cache/Queue** | Redis | 7.0+ | High performance, rich data structures |
| **K8s Client** | client-go | v0.28+ | Official library, complete functionality |
| **ORM** | GORM | v1.25+ | Easy to use, good performance |
| **Logging** | Zap | v1.26+ | High performance, structured |

---

## Deployment Architecture

### Single Replica Deployment
```
┌───────────────────────────────────┐
│  K8s Cluster                       │
│  ┌─────────────────────────────┐  │
│  │  waverless-pod              │  │
│  │  • API Server               │  │
│  │  • Autoscaler               │  │
│  │  • Background Jobs          │  │
│  └─────────────────────────────┘  │
│                                    │
│  ┌─────────────┐  ┌────────────┐  │
│  │  MySQL      │  │   Redis    │  │
│  └─────────────┘  └────────────┘  │
│                                    │
│  ┌─────────────────────────────┐  │
│  │  Worker Deployments         │  │
│  │  (Dynamic autoscaling)      │  │
│  └─────────────────────────────┘  │
└───────────────────────────────────┘
```

### High Availability Deployment (Recommended)
```
┌────────────────────────────────────────┐
│  K8s Cluster                            │
│  ┌───────────┐  ┌───────────┐          │
│  │waverless-1│  │waverless-2│          │
│  │  replica  │  │  replica  │          │
│  └───────────┘  └───────────┘          │
│         ↓              ↓                │
│    ┌────────────────────────┐          │
│    │  Distributed Lock      │          │
│    │  (Redis)               │          │
│    └────────────────────────┘          │
│                                         │
│  ┌────────────┐  ┌────────────┐        │
│  │MySQL Master│  │ Redis      │        │
│  │+ Replica   │  │ (Sentinel) │        │
│  └────────────┘  └────────────┘        │
└────────────────────────────────────────┘
```

**Multi-replica Safety**: [autoscaler-multi-replica-safety.md](autoscaler-multi-replica-safety.md)

---

## Performance Metrics

### Throughput
- **Task Creation**: ~1000 tasks/s
- **Task Assignment**: ~950 pulls/s (with Double-Check)
- **Heartbeat Processing**: ~5000 heartbeats/s

### Latency
- **Task Creation**: <10ms (p99)
- **Task Assignment**: <50ms (p99)
- **Autoscaling Decision**: 30s interval

### Scalability
- **Supported Endpoints**: 100+
- **Supported Workers**: 1000+
- **Concurrent Tasks**: 10000+

---

## Security

### Authentication and Authorization
- API Key authentication
- K8s RBAC

### Data Security
- Database connection encryption
- Sensitive information masking
- Log security

### Network Security
- Internal network isolation
- Service Mesh (optional)

---

## Monitoring and Observability

### Logging
- Structured logging (Zap)
- Log level control
- Key operation auditing

### Metrics (Prometheus)
```
# Task metrics
task_created_total{endpoint}
task_completed_total{endpoint, status}
task_duration_seconds{endpoint}

# Worker metrics
worker_status_total{status, endpoint}
worker_draining_current{endpoint}

# Autoscaler metrics
autoscaler_scale_decision_total{endpoint, action}
autoscaler_resource_usage{resource_type}

# Race condition protection metrics
task_assignment_race_condition_detected_total{endpoint}
```

### Tracing
- Request ID tracking
- Task lifecycle tracking

---

## Extensibility Design

### Plugin Architecture
- Provider interface (K8s/Docker/...)
- Queue interface (Redis/RabbitMQ/...)
- Store interface (MySQL/PostgreSQL/...)

### Configuration
- Dynamic configuration hot reload
- Endpoint-level configuration override

---

## Dashboard Statistics & Monitoring

### Problem

Original dashboard statistics had several issues:
1. **Inaccurate Data**: Statistics calculated from limited task query results (max 100)
2. **Poor Performance**: Full task list fetched for simple statistics
3. **Heavy Frontend Load**: Complex calculations performed client-side

### Solution: Pre-aggregated Statistics

#### Architecture

```
Task Lifecycle → Real-time Updates → Statistics Tables → API → Dashboard
                        ↓
                 task_statistics
                 (global/per-endpoint)
```

#### Database Design

**task_statistics Table**:
```sql
CREATE TABLE task_statistics (
    id INT AUTO_INCREMENT PRIMARY KEY,
    scope_type VARCHAR(50) NOT NULL,              -- 'global' or 'endpoint'
    scope_value VARCHAR(255) DEFAULT NULL,         -- endpoint name (NULL for global)
    pending_count INT DEFAULT 0,
    in_progress_count INT DEFAULT 0,
    completed_count INT DEFAULT 0,
    failed_count INT DEFAULT 0,
    cancelled_count INT DEFAULT 0,
    total_count INT DEFAULT 0,
    updated_at DATETIME(3) NOT NULL,

    UNIQUE KEY uk_scope (scope_type, scope_value),
    INDEX idx_updated_at (updated_at)
);
```

#### Update Strategy

1. **Incremental Updates**: When task status changes, atomically update statistics
2. **Periodic Refresh**: Background job validates and corrects drift
3. **On-demand Refresh**: Manual refresh API for immediate updates

#### Statistics API

| Endpoint | Description |
|----------|-------------|
| `GET /api/v1/statistics/overview` | Global task statistics |
| `GET /api/v1/statistics/endpoints` | Top N endpoints statistics |
| `GET /api/v1/statistics/endpoints/:endpoint` | Specific endpoint statistics |
| `POST /api/v1/statistics/refresh` | Manually refresh all statistics |

#### Benefits

- ✅ **Accurate Statistics**: Based on complete dataset, not limited queries
- ✅ **High Performance**: O(1) query time, no complex aggregations
- ✅ **Scalable**: Performance independent of total task count
- ✅ **Real-time**: Incremental updates on every status change

---

## GPU Usage Tracking

### Overview

GPU usage tracking provides comprehensive monitoring of GPU resource consumption, aggregated at minute/hourly/daily levels, supporting statistics by endpoint and spec dimensions.

### Architecture Design

#### Data Flow

```
Task Complete → Record GPU Usage → Aggregate Statistics → API → Frontend
     ↓              ↓                      ↓
  Get Spec      gpu_usage_records    gpu_usage_statistics_*
  from Endpoint
```

#### Data Tables

1. **gpu_usage_records**: Task-level GPU usage records
   - Task details: task_id, endpoint, worker_id, spec_name
   - GPU config: gpu_count, gpu_type, gpu_memory_gb
   - Timing: started_at, completed_at, duration_seconds
   - Metrics: gpu_hours (gpu_count * duration_hours)

2. **gpu_usage_statistics_minute**: Minute-level aggregation
   - time_bucket (e.g., "2025-11-12 10:30:00")
   - scope: global/endpoint/spec
   - Aggregates: total_tasks, gpu_hours, avg_gpu_count

3. **gpu_usage_statistics_hourly**: Hourly aggregation
   - Aggregated from minute-level data
   - Includes peak minute information

4. **gpu_usage_statistics_daily**: Daily aggregation
   - Aggregated from hourly data
   - Includes peak hour information

### GPU Information Extraction

GPU information is automatically extracted from spec definitions:

```yaml
# config/specs.yaml
specs:
  - name: gpu-h200-x1
    resources:
      gpu: "1"                    # GPU count
      gpuType: "H200-80GB"        # GPU type with memory
      memory: "100Gi"
      cpu: "10"
```

**Parsing Logic**:
- GPU count: from `resources.gpu` field
- GPU type: from `resources.gpuType` field
- GPU memory: extracted from gpuType if contains memory info (e.g., "H200-80GB" → 80GB)

### Recording Workflow

```go
// Automatically called on task completion
func (s *TaskService) UpdateTaskResult() {
    // ... update task status ...

    // Record GPU usage if task has GPU allocation
    if task.StartedAt != nil && task.CompletedAt != nil {
        s.recordGPUUsage(ctx, task)  // Async, non-blocking
    }
}

func (s *TaskService) recordGPUUsage(task *Task) {
    // 1. Get endpoint → spec_name
    endpoint := s.endpointService.GetEndpoint(task.Endpoint)

    // 2. Get spec details → GPU config
    spec := s.deploymentProvider.GetSpec(endpoint.SpecName)

    // 3. Calculate GPU hours
    duration := task.CompletedAt.Sub(*task.StartedAt)
    gpuHours := spec.GPUCount * (duration.Hours())

    // 4. Save record
    s.gpuUsageRepo.RecordGPUUsage(...)
}
```

### Aggregation System

#### Background Jobs

1. **Minute Aggregation**: Every minute
   - Aggregate records from previous minute
   - Create entries in `gpu_usage_statistics_minute`

2. **Hourly Aggregation**: Every hour
   - Aggregate from minute-level statistics
   - Calculate peak minute

3. **Daily Aggregation**: Daily at midnight
   - Aggregate from hourly statistics
   - Calculate peak hour

#### Aggregation Dimensions

- **Global**: All endpoints combined
- **Per-Endpoint**: Grouped by endpoint name
- **Per-Spec**: Grouped by spec name (GPU type)

### GPU Usage API

| Endpoint | Description |
|----------|-------------|
| `GET /api/v1/gpu-usage/overview` | Current GPU usage overview |
| `GET /api/v1/gpu-usage/trend` | Historical trend (minute/hour/day) |
| `GET /api/v1/gpu-usage/endpoints/:endpoint` | Endpoint-specific usage |
| `GET /api/v1/gpu-usage/specs` | Usage breakdown by GPU spec |
| `POST /api/v1/gpu-usage/backfill` | Backfill historical data |

### Monitoring & Visualization

#### Key Metrics

```prometheus
# GPU usage metrics
gpu_hours_total{endpoint, spec_name}
gpu_task_count{endpoint, spec_name, status}
gpu_utilization_rate{endpoint}
gpu_cost_total{endpoint, spec_name}
```

#### Dashboard Displays

- **Real-time**: Current GPU allocation and usage
- **Trends**: GPU hours over time (last 24h, 7d, 30d)
- **Breakdown**: By endpoint, by spec type
- **Cost Analysis**: GPU hours × cost per hour

### Data Retention

| Table | Retention | Cleanup Strategy |
|-------|-----------|------------------|
| gpu_usage_records | 30 days | Delete old records, keep aggregates |
| statistics_minute | 7 days | Roll up to hourly, then delete |
| statistics_hourly | 90 days | Roll up to daily, then delete |
| statistics_daily | 1 year | Archive to cold storage |

---

## Future Plans

- [ ] Multi-cluster support
- [ ] GPU scheduling optimization
- [ ] Cost optimization recommendations
- [ ] Task DAG support
- [ ] Webhook enhancements
- [ ] Advanced GPU analytics (utilization heatmaps, cost forecasting)
- [ ] Real-time dashboard streaming updates

---

**Document Version**: v2.0
**Last Updated**: 2025-11-14
