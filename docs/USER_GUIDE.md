# Waverless User Guide

## Table of Contents

- [1. Quick Start](#1-quick-start)
- [2. Configuration](#2-configuration)
- [3. API Reference](#3-api-reference)
- [4. Autoscaling](#4-autoscaling)
- [5. Web UI](#5-web-ui)
- [6. Troubleshooting](#6-troubleshooting)

---

## 1. Quick Start

### Prerequisites

- Kubernetes Cluster (1.19+)
- kubectl configured with cluster access
- Docker (for building images)

### One-Click Deployment

```bash
# Clone repository
git clone https://github.com/wavespeedai/waverless.git
cd waverless

# Deploy complete environment
./deploy.sh install

# Check status
./deploy.sh status

# Access Web UI
kubectl port-forward -n wavespeed svc/waverless-web-svc 3000:80
```

Access http://localhost:3000 (default: admin/admin)

### Deployment Options

| Parameter | Description | Default |
|-----------|-------------|---------|
| `-n, --namespace` | K8s namespace | wavespeed |
| `-e, --environment` | Environment (dev/test/prod) | dev |
| `-t, --tag` | Image tag | latest |
| `-k, --api-key` | Worker auth key | - |

**Examples**:
```bash
# Production
./deploy.sh -n wavespeed-prod -e prod -t v1.0.0 install

# Test environment
./deploy.sh -n wavespeed-test -e test install
```

### Other Commands

```bash
./deploy.sh build          # Build API Server
./deploy.sh build-web      # Build Web UI
./deploy.sh logs waverless 50
./deploy.sh restart
./deploy.sh upgrade
./deploy.sh uninstall
```

### Local Development

```bash
# Start dependencies
docker run -d -p 6379:6379 redis:7-alpine
docker run -d -p 3306:3306 -e MYSQL_ROOT_PASSWORD=password -e MYSQL_DATABASE=waverless mysql:8.0

# Build and run
make build
./waverless

# Or with hot reload
go install github.com/cosmtrek/air@latest
air -c .air.toml
```

### Access Services

```bash
# Port forwarding
kubectl port-forward -n wavespeed svc/waverless-svc 8080:80
kubectl port-forward -n wavespeed svc/waverless-web-svc 3000:80

# Test API
curl http://localhost:8080/health
```

---

## 2. Configuration

### Main Configuration (`config/config.yaml`)

```yaml
server:
  port: 8080
  mode: release  # debug, release

mysql:
  host: localhost
  port: 3306
  user: root
  password: password
  database: waverless

redis:
  addr: localhost:6379
  password: ""
  db: 0

queue:
  task_timeout: 3600  # seconds

worker:
  heartbeat_interval: 10
  heartbeat_timeout: 60
  default_concurrency: 1

k8s:
  enabled: true
  namespace: wavespeed
  platform: generic  # generic, aliyun-ack, aws-eks

autoscaler:
  enabled: true
  interval: 30
  max_gpu_count: 100
  max_cpu_cores: 1000
  max_memory_gb: 2000
  starvation_time: 300
```

### Spec Configuration (`config/specs.yaml`)

```yaml
specs:
  - name: "gpu-a10"
    displayName: "NVIDIA A10 GPU"
    category: "gpu"
    resources:
      gpu: "1"
      gpuType: "nvidia.com/gpu"
      cpu: "8"
      memory: "32Gi"
    platforms:
      aws-eks:
        nodeSelector:
          karpenter.sh/nodepool: gpu-a10
        tolerations:
          - key: nvidia.com/gpu
            operator: Exists
            effect: NoSchedule
```

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `MYSQL_DSN` | MySQL connection | - |
| `REDIS_ADDR` | Redis address | localhost:6379 |
| `K8S_NAMESPACE` | K8s namespace | default |
| `K8S_PLATFORM` | Platform type | generic |
| `AUTOSCALER_ENABLED` | Enable autoscaler | true |

### RBAC Permissions

Waverless requires K8s permissions for:
- Deployments: create, view, update, delete
- Services: create, view, update, delete
- Pods: view, get logs
- ConfigMaps: read

### Production Recommendations

- **Resources**: Adjust limits based on load
- **HA**: Multiple API Server replicas
- **Security**: Use Secrets, Network Policies, HTTPS
- **Monitoring**: Prometheus + Grafana

---

## 3. API Reference

### Task Submission

#### Async Submit
```bash
POST /v1/:endpoint/run
Content-Type: application/json

{
  "input": {"prompt": "hello world"},
  "webhook": "https://callback.url"  # optional
}

# Response
{"id": "task-uuid", "status": "PENDING"}
```

#### Sync Submit
```bash
POST /v1/:endpoint/runsync
Content-Type: application/json

{"input": {"prompt": "hello world"}}

# Response (after completion)
{
  "id": "task-uuid",
  "status": "COMPLETED",
  "output": {...}
}
```

### Task Status

```bash
GET /v1/status/:task_id

# Response
{
  "id": "task-uuid",
  "status": "COMPLETED",  # PENDING, IN_PROGRESS, COMPLETED, FAILED, CANCELLED
  "output": {...},
  "delayTime": 1234,      # ms
  "executionTime": 5678   # ms
}
```

### Cancel Task

```bash
POST /v1/cancel/:task_id
```

### Endpoint Management

#### Create
```bash
POST /api/v1/endpoints
{
  "name": "my-model",
  "image": "registry/worker:latest",
  "specName": "gpu-a10",
  "replicas": 2,
  "env": {"MODEL_PATH": "/models"},
  "minReplicas": 1,
  "maxReplicas": 10,
  "priority": 50
}
```

#### Update
```bash
PUT /api/v1/endpoints/:name
{"image": "registry/worker:v2", "replicas": 5}
```

#### Delete
```bash
DELETE /api/v1/endpoints/:name
```

### Worker API (for Workers)

```bash
# Pull task
GET /v2/:endpoint/job-take/:worker_id

# Heartbeat
GET /v2/:endpoint/ping/:worker_id

# Submit result
POST /v2/:endpoint/job-done/:worker_id/:task_id
{"output": {...}}
```

---

## 4. Autoscaling

### Overview

Intelligent scaling based on queue depth, priority, and resource constraints.

### Configuration Parameters

| Parameter | Description | Default | Recommended |
|-----------|-------------|---------|-------------|
| `minReplicas` | Minimum replicas | 0 | Critical: ≥2, Normal: 1 |
| `maxReplicas` | Maximum replicas | - | Based on capacity |
| `scaleUpThreshold` | Queue depth to scale up | 1 | Critical: 1, Batch: ≥5 |
| `scaleDownIdleTime` | Idle seconds before scale down | 300 | 180-600 |
| `scaleUpCooldown` | Scale up cooldown (s) | 30 | 30-60 |
| `scaleDownCooldown` | Scale down cooldown (s) | 60 | 60-180 |
| `priority` | Priority (0-100) | 50 | Critical: 90-100 |

### Scale Up Conditions

All must be met:
1. `replicas < maxReplicas`
2. `pendingTasks >= scaleUpThreshold`
3. `timeSinceLastScale >= scaleUpCooldown`
4. Resources available (or can preempt)

### Scale Down Conditions

All must be met:
1. `replicas > minReplicas`
2. `pendingTasks = 0`
3. `idleTime >= scaleDownIdleTime`
4. `timeSinceLastScale >= scaleDownCooldown`

### Priority System

```
100       Critical production (payments)
80-90     Important production
60-70     General production
40-50     Non-critical services
20-30     Test/development
```

**Behaviors**:
- High priority gets resources first
- Can preempt from lower priority
- Starvation protection after 5min wait

### Typical Scenarios

#### High-Priority Production
```json
{
  "minReplicas": 4,
  "maxReplicas": 20,
  "scaleUpThreshold": 1,
  "scaleDownIdleTime": 600,
  "priority": 100
}
```

#### Batch Processing
```json
{
  "minReplicas": 0,
  "maxReplicas": 10,
  "scaleUpThreshold": 10,
  "scaleDownIdleTime": 180,
  "priority": 30
}
```

### API

```bash
# Get status
GET /api/v1/autoscaler/status

# Enable/Disable
POST /api/v1/autoscaler/enable
POST /api/v1/autoscaler/disable

# Update endpoint config
PUT /api/v1/endpoints/:name/config
{"minReplicas": 2, "priority": 80}

# View history
GET /api/v1/autoscaler/history/:endpoint?limit=20
```

---

## 5. Web UI

### Features

- **Dashboard**: Overview of endpoints, workers, tasks
- **Endpoints**: Create, update, scale, delete
- **Workers**: Monitor status and logs
- **Tasks**: View history and details
- **Autoscaler**: Configure and monitor

### Development

```bash
cd web-ui
pnpm install
pnpm run dev  # http://localhost:5173
```

### Build

```bash
docker build -t waverless-web:latest \
  --build-arg VITE_ADMIN_USERNAME=admin \
  --build-arg VITE_ADMIN_PASSWORD=admin \
  web-ui/
```

---

## 6. Troubleshooting

### Tasks Stuck in PENDING

1. Check if workers are available for the endpoint
2. Review worker logs for errors
3. Verify autoscaler is enabled and functioning

### Workers Not Starting

Common causes:
- Image pull errors
- Resource constraints (GPU/CPU/Memory)
- Node selector mismatch
- Missing tolerations

### Autoscaler Not Scaling

Verify:
- Autoscaler is enabled (not "disabled")
- `maxReplicas` > current replicas
- Cluster has available resources

### Task Timeout

- Check global timeout in `config.yaml` → `queue.task_timeout`
- Check per-endpoint `taskTimeout` setting
- Increase timeout if tasks legitimately need more time

### Graceful Shutdown Issues

Ensure `terminationGracePeriodSeconds` >= task timeout + 30s to allow tasks to complete before pod termination.

### Quick Diagnostics

1. Check API health endpoint
2. Review Kubernetes events for errors
3. Check pod status and logs
4. Verify database connectivity

---

**Document Version**: v3.0  
**Last Updated**: 2026-02
