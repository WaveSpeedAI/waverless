# Waverless Architecture

Waverless is a **Serverless GPU task scheduling system** providing RunPod API-compatible worker management, task distribution, autoscaling, and graceful shutdown.

## Core Features

- ✅ **RunPod Compatible API** - Zero-code migration from runpod SDK
- ✅ **Autoscaling** - Queue-depth and resource-aware scaling with priority
- ✅ **Graceful Shutdown** - Zero task loss during rolling updates
- ✅ **Multi-tenant** - Isolated endpoints with independent configurations
- ✅ **High Availability** - Multi-replica deployment with distributed locking

---

## System Architecture

```mermaid
graph TB
    subgraph "Client Layer"
        WebUI[Web UI]
        RestAPI[REST API]
        Worker[Worker]
        Webhook[Webhook]
    end

    subgraph "API Server Layer"
        Router[Router]
        Handler[Handler]
        Service[Service]
    end

    subgraph "Business Logic Layer"
        TaskSvc[Task Service]
        WorkerSvc[Worker Service]
        EndpointSvc[Endpoint Service]
        Autoscaler[Autoscaler Manager]
    end

    subgraph "Infrastructure Layer"
        MySQL[(MySQL)]
        Redis[(Redis)]
        K8sClient[K8s Client]
    end

    subgraph "K8s Cluster"
        Deployments[Worker Deployments]
        Informers[K8s Informers]
    end

    WebUI & RestAPI & Worker & Webhook --> Router
    Router --> Handler --> Service
    Service --> TaskSvc & WorkerSvc & EndpointSvc
    TaskSvc & WorkerSvc & EndpointSvc --> Autoscaler
    TaskSvc & WorkerSvc & EndpointSvc --> MySQL & Redis
    Autoscaler --> K8sClient
    K8sClient --> Deployments
    Informers --> WorkerSvc
```

---

## Core Components

### 1. Task Service

**Responsibilities**: Task lifecycle management

- Task creation and status management
- Task assignment and scheduling
- Task timeout detection
- Orphaned task cleanup

**Data Flow**:
```mermaid
flowchart LR
    A[Create Task] --> B[MySQL Insert]
    B --> C[Redis Queue]
    C --> D[Worker Pull]
    D --> E[Execute]
    E --> F[Write Result]
```

### 2. Worker Service

**Responsibilities**: Worker registration, heartbeat, task assignment

- Worker registration and status management
- Heartbeat detection and offline cleanup
- Task assignment with Double-Check pattern
- DRAINING status management

**Worker State Machine**:
```mermaid
stateDiagram-v2
    [*] --> STARTING: Pod Created
    STARTING --> ONLINE: First Heartbeat
    ONLINE --> BUSY: Task Assigned
    BUSY --> ONLINE: Task Completed
    ONLINE --> DRAINING: Termination Signal
    BUSY --> DRAINING: Termination Signal
    DRAINING --> OFFLINE: Tasks Done
    OFFLINE --> [*]
```

### 3. Endpoint Service

**Responsibilities**: Multi-tenant management

- Endpoint metadata management
- Autoscaling configuration per endpoint
- Statistics aggregation

### 4. Autoscaler

**Responsibilities**: Intelligent scaling decisions

```mermaid
flowchart TB
    subgraph "Control Loop (every 30s)"
        A[Collect Metrics] --> B[Calculate Resources]
        B --> C[Make Decisions]
        C --> D[Execute Scaling]
    end

    subgraph "Metrics"
        M1[Queue Depth]
        M2[Worker Count]
        M3[Idle Time]
        M4[Priority]
    end

    M1 & M2 & M3 & M4 --> A
```

**Decision Algorithm**:
1. **Scale up**: `pendingTasks >= scaleUpThreshold`
2. **Scale down**: `idleTime >= scaleDownIdleTime`
3. **Resource check**: Cluster capacity verification
4. **Priority sorting**: High-priority endpoints first
5. **Cooldown**: Prevent frequent scaling

### 5. Lifecycle Manager

**Responsibilities**: Worker lifecycle synchronization via K8s Informers

```mermaid
flowchart TB
    subgraph "K8s Events"
        E1[Pod Status Change]
        E2[Pod Deletion]
        E3[Termination Signal]
        E4[Container Failure]
    end

    subgraph "Callbacks"
        C1[OnWorkerStatusChange]
        C2[OnWorkerDelete]
        C3[OnWorkerDraining]
        C4[OnWorkerFailure]
    end

    subgraph "Actions"
        A1[Sync to MySQL]
        A2[Mark OFFLINE]
        A3[Stop Task Assignment]
        A4[Record Failure]
    end

    E1 --> C1 --> A1
    E2 --> C2 --> A2
    E3 --> C3 --> A3
    E4 --> C4 --> A4
```

---

## Data Model

### Core Entities

```mermaid
erDiagram
    ENDPOINT ||--o{ WORKER : has
    ENDPOINT ||--o{ TASK : receives
    WORKER ||--o{ TASK : executes

    ENDPOINT {
        string name PK
        string image
        string spec_name
        int replicas
        int min_replicas
        int max_replicas
        int priority
        string status
    }

    WORKER {
        string worker_id PK
        string endpoint FK
        string pod_name
        string status
        int current_jobs
        timestamp last_heartbeat
    }

    TASK {
        string task_id PK
        string endpoint FK
        string worker_id FK
        string status
        json input
        json output
        timestamp created_at
    }
```

### Storage Strategy

| Data | Storage | Reason |
|------|---------|--------|
| Task Metadata | MySQL | Persistence, transactions, queries |
| Task Queue | Redis List | High-performance, atomic operations |
| Worker State | MySQL | Persistence, complex queries |
| Endpoint Config | MySQL | Persistent configuration |
| Distributed Lock | Redis | Multi-instance coordination |

---

## Task Flow

### Task Creation
```mermaid
sequenceDiagram
    Client->>API: Submit Task
    API->>MySQL: Insert task (PENDING)
    API->>Redis: Push to queue
    API-->>Client: Return task_id
```

### Task Assignment (Double-Check Pattern)
```mermaid
sequenceDiagram
    Worker->>API: Pull Task
    API->>MySQL: Check worker status
    
    alt Worker DRAINING
        API-->>Worker: Empty response
    else Worker ONLINE
        API->>Redis: Pop from queue
        API->>MySQL: Update task (IN_PROGRESS)
        API->>MySQL: Re-check worker status
        
        alt Worker became DRAINING
            API->>MySQL: Revert task (PENDING)
            API->>Redis: Push back to queue
            API-->>Worker: Empty response
        else Still ONLINE
            API-->>Worker: Task assigned
        end
    end
```

---

## Graceful Shutdown

```mermaid
sequenceDiagram
    participant K8s
    participant Informer
    participant Lifecycle
    participant Worker
    participant MySQL

    K8s->>Informer: Pod Termination Signal
    Informer->>Lifecycle: OnWorkerDraining
    Lifecycle->>MySQL: Set DRAINING
    
    loop Until tasks complete
        Worker->>Worker: Complete current tasks
        Worker-->>Lifecycle: Heartbeat (no new tasks)
    end

    K8s->>Informer: Pod Deleted
    Informer->>Lifecycle: OnWorkerDelete
    Lifecycle->>MySQL: Set OFFLINE
```

### Rolling Update Optimization

When deployment spec changes:
1. Detect deployment change via Informer
2. Set `PodDeletionCost = -1000` for idle workers (delete first)
3. Set `PodDeletionCost = 1000` for busy workers (delete last)
4. K8s respects deletion cost during rolling update

---

## Dashboard Statistics

### Pre-aggregated Statistics

```mermaid
flowchart LR
    A[Task Status Change] --> B[Atomic Update]
    B --> C[task_statistics Table]
    C --> D[API Query O-1]
    D --> E[Dashboard]
```

**Benefits**:
- Accurate: Based on complete dataset
- Fast: O(1) query time
- Scalable: Independent of task count
- Real-time: Incremental updates

### Statistics API

| Endpoint | Description |
|----------|-------------|
| `GET /api/v1/statistics/overview` | Global statistics |
| `GET /api/v1/statistics/endpoints` | Per-endpoint statistics |
| `POST /api/v1/statistics/refresh` | Manual refresh |

---

## Performance

### Throughput
- Task Creation: ~1000 tasks/s
- Task Assignment: ~950 pulls/s
- Heartbeat Processing: ~5000/s

### Latency
- Task Creation: <10ms (p99)
- Task Assignment: <50ms (p99)
- Autoscaling Decision: 30s interval

### Scalability
- Endpoints: 100+
- Workers: 1000+
- Concurrent Tasks: 10000+

---

## Deployment Architecture

### Single Replica
```mermaid
graph TB
    subgraph "K8s Cluster"
        API[Waverless Pod]
        MySQL[(MySQL)]
        Redis[(Redis)]
        Workers[Worker Pods]
    end

    API --> MySQL & Redis
    Workers --> API
```

### High Availability
```mermaid
graph TB
    subgraph "K8s Cluster"
        API1[Waverless-1]
        API2[Waverless-2]
        Lock[Distributed Lock<br/>Redis]
        MySQL[(MySQL<br/>Primary + Replica)]
        Redis[(Redis<br/>Sentinel)]
    end

    API1 & API2 --> Lock
    API1 & API2 --> MySQL & Redis
```

---

## Technology Stack

| Component | Technology | Purpose |
|-----------|------------|---------|
| Language | Go 1.21+ | Performance, K8s ecosystem |
| Web Framework | Gin | HTTP server |
| Database | MySQL 8.0+ | Persistent storage |
| Cache/Queue | Redis 7.0+ | Task queue, locking |
| K8s Client | client-go | Kubernetes integration |
| ORM | GORM | Database operations |
| Logging | Zap | Structured logging |

---

**Document Version**: v3.0  
**Last Updated**: 2026-02
