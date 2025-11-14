# Waverless User Guide

## Table of Contents

- [1. Quick Start](#1-quick-start)
  - [Overview](#overview)
  - [Prerequisites](#prerequisites)
  - [One-Click Deployment](#one-click-deployment)
  - [Multi-Environment Deployment](#multi-environment-deployment)
  - [Access Services](#access-services)
  - [Local Development](#local-development)
  - [Docker Deployment](#docker-deployment)
  - [Monitoring and Debugging](#monitoring-and-debugging)
- [2. Configuration](#2-configuration)
  - [Main Configuration](#main-configuration)
  - [RBAC Permissions](#rbac-permissions)
  - [Production Environment Recommendations](#production-environment-recommendations)
  - [Graceful Shutdown](#graceful-shutdown)
- [3. Autoscaling](#3-autoscaling)
  - [Overview](#autoscaling-overview)
  - [Core Concepts](#core-concepts)
  - [Quick Start](#autoscaling-quick-start)
  - [Typical Scenarios](#typical-scenarios)
  - [Resource Allocation Strategy](#resource-allocation-strategy)
  - [Best Practices](#autoscaling-best-practices)
- [4. Web UI](#4-web-ui)
  - [Overview](#web-ui-overview)
  - [Key Features](#key-features)
  - [API Integration](#api-integration)
  - [Deployment Architecture](#deployment-architecture)
  - [Security Considerations](#security-considerations)
  - [Development](#web-ui-development)
- [5. Troubleshooting](#5-troubleshooting)
  - [Autoscaling Issues](#autoscaling-issues)
  - [K8s Informer Issues](#k8s-informer-issues)
  - [Worker Graceful Shutdown Issues](#worker-graceful-shutdown-issues)
  - [Task Execution Issues](#task-execution-issues)
  - [Database Issues](#database-issues)
  - [Performance Issues](#performance-issues)
  - [Quick Diagnostic Tools](#quick-diagnostic-tools)

---

## 1. Quick Start

### Overview

This guide will help you quickly deploy and use the Waverless system. Waverless is a high-performance Serverless GPU task orchestration system that supports Kubernetes native deployment and RunPod-compatible Worker protocol.

### Prerequisites

- **Kubernetes Cluster** (1.19+)
- **kubectl** configured with cluster access
- **Docker** (for building images, optional)
- **Redis** or use built-in Redis deployment

### One-Click Deployment

#### Using Deployment Script (Recommended)

```bash
# Clone repository
git clone https://github.com/wavespeedai/waverless.git
cd waverless

# Deploy complete environment (including Redis, API Server, Web UI)
./deploy.sh install

# Check deployment status
./deploy.sh status

# Access Web UI (port forwarding)
kubectl port-forward -n wavespeed svc/waverless-web-svc 3000:80
```

Access http://localhost:3000 to use Web UI (default username/password: admin/admin).

#### Deployment Script Options

| Parameter | Description | Default |
|------|------|--------|
| `-n, --namespace` | K8s namespace | wavespeed |
| `-e, --environment` | Environment identifier (dev/test/prod) | dev |
| `-t, --tag` | Image tag | latest |
| `-k, --api-key` | Worker authentication key | - |
| `-b, --backend-url` | Web UI backend API URL | http://waverless-svc:80 |

**Examples**:
```bash
# Deploy to production environment
./deploy.sh -n wavespeed-prod -e prod -t v1.0.0 install

# Deploy to test environment
./deploy.sh -n wavespeed-test -e test install

# Custom Web UI backend URL
./deploy.sh -b http://api.example.com install
```

#### Other Deployment Commands

```bash
# Build images
./deploy.sh build              # Build API Server
./deploy.sh build-web          # Build Web UI

# Deploy Web UI only (requires existing API Server)
./deploy.sh install-web

# View logs
./deploy.sh logs waverless 50  # View last 50 lines

# Restart service
./deploy.sh restart

# Upgrade deployment
./deploy.sh -t v1.0.1 upgrade

# Uninstall
./deploy.sh uninstall
```

### Multi-Environment Deployment

Use different namespaces to isolate environments:

```bash
# Development environment
./deploy.sh -n wavespeed-dev install

# Test environment
./deploy.sh -n wavespeed-test -e test install

# Production environment
./deploy.sh -n wavespeed-prod -e prod -t v1.0.0 install
```

### Access Services

#### Port Forwarding (Development/Testing)

```bash
# API Server
kubectl port-forward -n wavespeed svc/waverless-svc 8080:80

# Web UI
kubectl port-forward -n wavespeed svc/waverless-web-svc 7860:7860

# Test API
curl http://localhost:8080/health

# Access Web UI
open http://localhost:7860
```

#### Using Ingress (Production)

Configure Ingress to expose services externally:

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: waverless-ingress
  namespace: wavespeed
spec:
  rules:
  - host: waverless.example.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: waverless-web-svc
            port:
              number: 80
```

### Local Development

#### 1. Start Dependency Services

```bash
# Start Redis
docker run -d -p 6379:6379 redis:7-alpine

# Start MySQL (optional)
docker run -d -p 3306:3306 \
  -e MYSQL_ROOT_PASSWORD=password \
  -e MYSQL_DATABASE=waverless \
  mysql:8.0
```

#### 2. Build

```bash
# Using Makefile
make build

# Or use go build directly
go build -o waverless ./cmd/server
```

#### 3. Run

```bash
# Run directly
./waverless

# Or use make
make run

# Or use go run
go run ./cmd/server
```

#### 4. Development Mode (Auto-restart)

```bash
# Install air
go install github.com/cosmtrek/air@latest

# Run with air
air -c .air.toml

# Or use make
make dev
```

Create `.air.toml`:
```toml
root = "."
tmp_dir = "tmp"

[build]
  bin = "./tmp/waverless"
  cmd = "go build -o ./tmp/waverless ./cmd/server"
  delay = 1000
  exclude_dir = ["tmp", "vendor", "node_modules"]
  include_ext = ["go"]
  log = "air.log"
```

#### 5. Test API

```bash
# Health check
curl http://localhost:8080/health

# View endpoints
curl http://localhost:8080/api/v1/endpoints

# View tasks
curl http://localhost:8080/v1/tasks

# Submit task
curl -X POST http://localhost:8080/v1/test-endpoint/run \
  -H "Content-Type: application/json" \
  -d '{"input": {"prompt": "test"}}'
```

#### 6. Run Web UI (Development Mode)

```bash
cd web-ui
npm install -g pnpm
pnpm install
pnpm run dev
```

Access http://localhost:5173 (Vite default port).

### Docker Deployment

#### Build Images

```bash
# Build API Server
docker build -t waverless:latest .

# Build Web UI
cd web-ui
docker build -t waverless-web:latest \
  --build-arg VITE_ADMIN_USERNAME=admin \
  --build-arg VITE_ADMIN_PASSWORD=admin \
  .
```

#### Run Containers

```bash
# Run API Server
docker run -d -p 8080:8080 \
  -e REDIS_ADDR=redis:6379 \
  --name waverless \
  waverless:latest

# Run Web UI
docker run -d -p 7860:80 \
  -e API_BACKEND_URL=http://waverless:8080 \
  --name waverless-web \
  waverless-web:latest
```

### Monitoring and Debugging

#### View Logs

```bash
# API Server logs
kubectl logs -n wavespeed deployment/waverless -f

# Web UI logs
kubectl logs -n wavespeed deployment/waverless-web -f

# Worker logs
kubectl logs -n wavespeed deployment/<worker-name> -f
```

#### View Resources

```bash
# All resources
kubectl get all -n wavespeed

# Pod details
kubectl describe pod -n wavespeed <pod-name>

# Events
kubectl get events -n wavespeed --sort-by='.lastTimestamp'
```

#### Debug Containers

```bash
# Enter API Server
kubectl exec -it -n wavespeed deployment/waverless -- sh

# Enter Redis
kubectl exec -it -n wavespeed deployment/waverless-redis -- redis-cli

# View configuration
kubectl get configmap -n wavespeed waverless-config -o yaml
```

---

## 2. Configuration

### Main Configuration

Main configuration file: `config/config.yaml`

```yaml
server:
  port: 8080
  mode: release

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
  concurrency: 10
  max_retry: 3
  task_timeout: 3600

worker:
  heartbeat_interval: 10
  heartbeat_timeout: 60
  default_concurrency: 1

k8s:
  enabled: true
  namespace: wavespeed
  platform: aliyun-ack
  config_dir: /app/config
```

### RBAC Permissions

Waverless requires the following K8s permissions to manage applications:

- **Deployments**: create, view, update, delete
- **Services**: create, view, update, delete
- **Pods**: view, get logs
- **ConfigMaps**: read

Permission configuration is in `k8s/waverless-rbac.yaml`, including:
- ServiceAccount: `waverless`
- Role: `waverless-manager`
- RoleBinding: `waverless-manager-binding`

### Production Environment Recommendations

#### Resource Configuration

Adjust resource limits based on load:

```yaml
resources:
  requests:
    memory: "512Mi"
    cpu: "500m"
  limits:
    memory: "1Gi"
    cpu: "1000m"
```

#### High Availability

- Increase API Server replica count
- Use Redis Cluster (for large-scale scenarios)
- Configure HPA (Horizontal Pod Autoscaler)
- Configure LoadBalancer or Ingress

#### Security Configuration

- Use Secrets to store sensitive information
- Configure Network Policies
- Regularly update images
- Enable HTTPS (production environment)

#### Monitoring and Alerting

- Deploy Prometheus + Grafana
- Monitor queue depth, worker count
- Set alerts for critical metrics
- Regularly backup Redis/MySQL data

### Graceful Shutdown

#### Local Development

```bash
# Send SIGTERM (recommended)
kill -TERM <pid>

# Or press Ctrl+C
```

#### Kubernetes

Waverless supports graceful shutdown. Workers are marked as DRAINING when pods are deleted and no longer receive new tasks. Ensure `terminationGracePeriodSeconds` is configured appropriately (recommended: task timeout + 30 seconds).

---

## 3. Autoscaling

### Autoscaling Overview

Intelligently schedules replica counts for multiple endpoints, ensuring:
- Endpoints with tasks scale up promptly
- Idle endpoints automatically scale down to release resources
- When resources are insufficient, allocate by priority to avoid task starvation

#### Core Features

- Fair Allocation - Minimum guarantee + priority-based allocation
- Dynamic Priority - Auto-elevation under high load + starvation protection
- Graceful Scale Down - Integrated with worker graceful shutdown, no task interruption
- Resource Preemption - High priority can preempt from low priority
- Complete Monitoring - Event recording + status queries

### Core Concepts

#### Configuration Parameters

| Parameter | Description | Default | Recommended |
|------|------|--------|--------|
| `minReplicas` | Minimum replica count (can be 0) | 0 | Critical services ≥2, general services =1 |
| `maxReplicas` | Maximum replica count | - | Based on cluster capacity |
| `scaleUpThreshold` | Queued task count threshold, triggers scale up | 1 | Critical services =1, batch services ≥5 |
| `scaleDownIdleTime` | Idle time (seconds), triggers scale down | 300 | Fast response =600, save resources =180 |
| `scaleUpCooldown` | Scale up cooldown time (seconds) | 30 | 30-60 seconds |
| `scaleDownCooldown` | Scale down cooldown time (seconds) | 60 | 60-180 seconds |
| `priority` | Priority (0-100) | 50 | Critical services =90-100, testing =20-30 |

#### Scale Up Decision Conditions

Must meet all:
1. Current replica count < maxReplicas
2. Queued task count ≥ scaleUpThreshold
3. Time since last scaling ≥ scaleUpCooldown
4. Cluster resources sufficient (or priority high enough to preempt)

#### Scale Down Decision Conditions

Must meet all:
1. Current replica count > minReplicas
2. Queued task count = 0
3. Idle time ≥ scaleDownIdleTime
4. Time since last scaling ≥ scaleDownCooldown

### Autoscaling Quick Start

#### 1. Global Configuration

```yaml
# config/config.yaml
autoscaler:
  enabled: true           # Enable autoscaling
  interval: 10            # Run every 10 seconds
  max_gpu_count: 8        # Total cluster GPU count
  max_cpu_cores: 128
  max_memory_gb: 512
  starvation_time: 300    # 5 minutes without allocation considered starvation
```

#### 2. Endpoint Configuration

##### Method 1: Via API

```bash
curl -X POST http://localhost:8080/api/v1/endpoints \
  -H "Content-Type: application/json" \
  -d '{
    "endpoint": "stable-diffusion",
    "specName": "gpu-4090-single",
    "image": "my-image:latest",
    "minReplicas": 1,
    "maxReplicas": 8,
    "scaleUpThreshold": 2,
    "scaleDownIdleTime": 300,
    "scaleUpCooldown": 30,
    "scaleDownCooldown": 60,
    "priority": 70
  }'
```

##### Method 2: Via Web UI

Configure autoscaling parameters on the endpoint edit page.

#### 3. API Usage

##### View Status

```bash
curl http://localhost:8080/api/v1/autoscaler/status
```

Response Example:
```json
{
  "enabled": true,
  "clusterResources": {
    "total": {"gpuCount": 8},
    "used": {"gpuCount": 5},
    "available": {"gpuCount": 3}
  },
  "endpoints": [{
    "name": "stable-diffusion",
    "currentReplicas": 3,
    "desiredReplicas": 3,
    "pendingTasks": 5,
    "priority": 70,
    "effectivePrio": 90,  // After dynamic elevation
    "idleTime": 0,
    "waitingTime": 120
  }]
}
```

##### Update Configuration

```bash
curl -X PUT http://localhost:8080/api/v1/autoscaler/endpoints/stable-diffusion \
  -d '{"minReplicas": 2, "maxReplicas": 10, "priority": 80}'
```

##### View History

```bash
curl http://localhost:8080/api/v1/autoscaler/history/stable-diffusion?limit=20
```

##### Enable/Disable

```bash
# Enable
curl -X POST http://localhost:8080/api/v1/autoscaler/enable

# Disable
curl -X POST http://localhost:8080/api/v1/autoscaler/disable
```

### Typical Scenarios

#### Scenario 1: High-Priority Production Service

Fast response, always available:

```json
{
  "endpoint": "production-api",
  "minReplicas": 4,          // 4 hot standby replicas
  "maxReplicas": 20,
  "scaleUpThreshold": 1,     // Scale up on any task
  "scaleDownIdleTime": 600,  // 10 minutes idle before scale down
  "priority": 100            // Highest priority
}
```

#### Scenario 2: Batch Processing Service

Save resources, can tolerate cold start:

```json
{
  "endpoint": "batch-processing",
  "minReplicas": 0,          // Can scale to 0
  "maxReplicas": 10,
  "scaleUpThreshold": 10,    // Accumulate 10 tasks before scale up
  "scaleDownIdleTime": 180,  // 3 minutes idle then scale down
  "priority": 30             // Low priority
}
```

#### Scenario 3: Multi-Tenant Environment

```
Cluster: 8 GPUs

Endpoint A (VIP customer)     - priority: 70, maxReplicas: 6
Endpoint B (Regular customer) - priority: 50, maxReplicas: 5
Endpoint C (Test environment) - priority: 30, maxReplicas: 4

Behavior:
1. Normal: Allocate on demand, minimum guarantee of at least 1 replica per endpoint with tasks
2. Resource shortage: Allocate by priority A > B > C
3. A under high load: Preempt resources from B and C
4. C long wait: Starvation protection, temporarily elevate priority
```

### Resource Allocation Strategy

#### Priority-Based Allocation

```
Sufficient resources:
  └─ All endpoints scale up on demand

Insufficient resources:
  ├─ 1. Sort scale-up requests by priority
  ├─ 2. Minimum guarantee: At least 1 replica per endpoint with tasks
  ├─ 3. Remaining resources allocated by priority
  └─ 4. Record blocked scale-up requests
```

#### Preemption Mechanism

**Trigger condition**: High-priority endpoint scale-up blocked, and low-priority idle endpoints exist

**Flow**:
1. Find all blocked high-priority scale-up requests
2. Find preemptable low-priority endpoints (currently no tasks + replica count > minReplicas)
3. Scale down low-priority endpoints, release resources
4. Allocate resources to high-priority endpoints

#### Starvation Protection

**Trigger condition**: Endpoint queued task wait time > 5 minutes (configurable)

**Behavior**: Temporarily elevate priority to avoid prolonged resource starvation

### Autoscaling Best Practices

#### Priority Allocation Recommendations

```
100       Critical production services (payments, orders)
80-90     Important production services
60-70     General production services
40-50     Non-critical services
20-30     Test/development environments
```

#### Cooldown Time Recommendations

```
Scale up cooldown: 30-60s   (Fast response to load)
Scale down cooldown: 60-180s (Avoid flapping)
```

#### MinReplicas Recommendations

```
Critical services: minReplicas ≥ 2  (High availability)
General services: minReplicas = 1   (Keep warm standby)
Batch tasks: minReplicas = 0        (Save resources)
```

#### Regular Review

**Weekly/Monthly**:
1. Review autoscaling history, identify patterns
2. Adjust priority and maxReplicas
3. Check resource utilization
4. Optimize scaleUpThreshold and scaleDownIdleTime

---

## 4. Web UI

### Web UI Overview

Waverless Web UI is a modern React-based management interface for Kubernetes GPU workload orchestration. It provides visual deployment, monitoring, and management capabilities for the Waverless serverless platform.

#### Technology Stack

- **Frontend Framework**: React 18 + TypeScript
- **UI Library**: Ant Design 5.x
- **State Management**: React Query for server state
- **Routing**: React Router v6
- **HTTP Client**: Axios
- **Build Tool**: Vite
- **Styling**: Ant Design CSS-in-JS

### Key Features

#### 1. Authentication System

- **Login Protection**: All routes are protected by authentication
- **Credentials**: Configurable via build-time environment variables
  - `VITE_ADMIN_USERNAME` (default: admin)
  - `VITE_ADMIN_PASSWORD` (default: admin)
- **Session Management**: LocalStorage-based token storage
- **Logout**: Manual session termination

#### 2. Dashboard

- **Application Metrics**: Total, running, pending, failed applications
- **Task Statistics**: Queue depth, completion rate, execution metrics
- **Replica Health**: Real-time pod status monitoring
- **Recent Activity**: Latest applications and tasks
- **Endpoint Statistics**: Per-endpoint task volume and success rates

#### 3. Application Deployment

- **GPU Spec Selection**: Pre-configured hardware specifications
  - CPU instances (4 cores, 8GB)
  - GPU instances (RTX 4090, A100, etc.)
- **Configuration Options**:
  - Docker image specification
  - Replica count
  - Environment variables
  - Resource limits
- **Deployment Preview**: YAML configuration preview before deployment

#### 4. Application Management

- **List View**: All deployed applications with status
- **Real-time Updates**: Auto-refresh every 10 seconds
- **Operations**:
  - Update deployment (image, replicas)
  - Edit metadata (display name, description, task timeout)
  - View logs (last N lines)
  - Delete application
  - View task queue
- **Detailed Information**:
  - Basic info (name, status, created date)
  - Deployment config (image, spec, replicas)
  - Environment variables
  - Kubernetes labels
  - Timestamps

#### 5. Task Queue Monitoring

- **Global View**: All tasks across endpoints
- **Filtering**: By endpoint and status
- **Task States**: Pending, In Progress, Completed, Failed, Cancelled
- **Statistics**:
  - Overall metrics (total, by status)
  - Per-endpoint breakdown
  - Execution time tracking
- **Task Details**:
  - Input/output data
  - Error messages
  - Worker assignment
  - Timing metrics (delay time, execution time)

#### 6. GPU Specifications

- **Spec Catalog**: View available hardware configurations
- **Platform Support**: Multiple platform configurations (Alibaba Cloud, AWS, etc.)
- **Resource Details**:
  - CPU/Memory allocation
  - GPU type and count
  - Node selectors
  - Tolerations

### API Integration

#### Base Configuration

```typescript
// API client automatically uses backend URL from Nginx proxy
const API_BASE_URL = '/api/v1';  // Proxied by Nginx to waverless-svc
```

#### Key Endpoints

- `GET /api/v1/endpoints` - List endpoints (metadata-first)
- `POST /api/v1/endpoints` - Create/deploy endpoint
- `GET /api/v1/endpoints/:name` - Get endpoint details
- `PUT /api/v1/endpoints/:name` - Update endpoint metadata
- `PATCH /api/v1/endpoints/:name/deployment` - Update deployment (image/replicas)
- `DELETE /api/v1/endpoints/:name` - Delete endpoint
- `GET /api/v1/specs` - List GPU specifications
- `GET /v1/tasks` - List tasks
- `POST /v1/cancel/:task_id` - Cancel task

### Deployment Architecture

#### Docker Multi-Stage Build

```dockerfile
# Stage 1: Build React App
FROM node:20-alpine AS builder
WORKDIR /app
RUN npm install -g pnpm && pnpm install
ARG VITE_ADMIN_USERNAME=admin
ARG VITE_ADMIN_PASSWORD=admin
ENV VITE_ADMIN_USERNAME=$VITE_ADMIN_USERNAME
ENV VITE_ADMIN_PASSWORD=$VITE_ADMIN_PASSWORD
RUN pnpm run build

# Stage 2: Serve with Nginx
FROM nginx:alpine
COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx.conf.template /etc/nginx/conf.d/default.conf.template
COPY docker-entrypoint.sh /docker-entrypoint.sh
ENV API_BACKEND_URL=http://waverless-svc:80
ENTRYPOINT ["/docker-entrypoint.sh"]
```

#### Nginx Configuration

- **Static Files**: Served from `/usr/share/nginx/html`
- **API Proxy**: `/api/*`, `/v1/*`, `/v2/*` → `$API_BACKEND_URL`
- **SPA Routing**: All unmatched routes → `/index.html`
- **Health Check**: `/health` → Backend health endpoint
- **Security Headers**:
  - X-Frame-Options: SAMEORIGIN
  - X-Content-Type-Options: nosniff
  - X-XSS-Protection: 1; mode=block

#### Environment Configuration

Backend URL is configurable at runtime via `API_BACKEND_URL` environment variable:

```bash
# Default: same namespace service
API_BACKEND_URL=http://waverless-svc:80

# External API
API_BACKEND_URL=https://api.example.com

# Different namespace
API_BACKEND_URL=http://waverless-svc.production:80
```

### Security Considerations

#### Authentication

- Build-time credential embedding (credentials compiled into JS bundle)
- Session tokens stored in LocalStorage
- No server-side session management
- **Note**: Suitable for internal/trusted networks only

#### Recommendations for Production

1. **Use External Authentication**:
   - OAuth 2.0 / OIDC providers
   - LDAP/Active Directory integration
   - JWT-based authentication

2. **Network Security**:
   - Deploy behind VPN or private network
   - Use Kubernetes NetworkPolicies
   - Enable mTLS for service-to-service communication

3. **Credentials Management**:
   - Store credentials in Kubernetes Secrets
   - Use external secret management (Vault, AWS Secrets Manager)
   - Rotate credentials regularly

4. **HTTPS**:
   - Use TLS termination at Ingress level
   - Enforce HTTPS redirects
   - Use valid certificates

### Web UI Development

#### Local Setup

```bash
cd web-ui
npm install -g pnpm
pnpm install
pnpm run dev
```

#### Build with Custom Credentials

```bash
ADMIN_USERNAME=myuser ADMIN_PASSWORD=mypass ./build-and-push.sh latest web-ui
```

#### Testing

```bash
# TypeScript type checking
pnpm run type-check

# Build
pnpm run build
```

#### Auto-Refresh Strategy

- **Application List**: 10-second intervals
- **Task Queue**: 5-second intervals
- **Dashboard Metrics**: 10-second intervals
- **Background Refresh**: Non-blocking updates (no loading spinners)

---

## 5. Troubleshooting

### Autoscaling Issues

#### Problem 1: Autoscaler Not Started

**Symptoms**:
- No "autoscaler started" in logs
- Replica count doesn't change

**Troubleshooting Steps**:
```bash
# 1. Check configuration
kubectl exec <waverless-pod> -- cat /app/config/config.yaml | grep -A5 "autoscaler"

# Expected output:
# autoscaler:
#   enabled: true
#   interval: 30
```

**Solution**:
1. Ensure `config.yaml` has `autoscaler.enabled: true`
2. Ensure `k8s.enabled: true`
3. Restart application

#### Problem 2: Endpoint Not Being Scaled

**Symptoms**:
- Has pending tasks but replica count doesn't increase
- No tasks but replica count doesn't decrease

**Troubleshooting Steps**:
```bash
# 1. Check endpoint configuration
curl http://localhost:8090/api/v1/endpoints/<endpoint> | jq '.autoscalerConfig'

# 2. Check autoscaler status
curl http://localhost:8090/api/v1/autoscaler/status | jq '.endpoints[] | select(.name == "<endpoint>")'

# 3. View decision logs
kubectl logs <waverless-pod> | grep "scale decision" | grep "<endpoint>"
```

**Common Causes**:
1. `maxReplicas = 0` → Autoscaling not configured
2. `autoscalerEnabled = "disabled"` → Endpoint autoscaling force-disabled
3. `minReplicas = maxReplicas` → Fixed replica count
4. Cooldown period not passed → Wait for cooldown period

**Solution**:
```bash
# Update endpoint configuration
curl -X PUT http://localhost:8090/api/v1/endpoints/<endpoint> \
  -H "Content-Type: application/json" \
  -d '{
    "maxReplicas": 10,
    "minReplicas": 1,
    "scaleUpThreshold": 1,
    "scaleDownIdleTime": 300
  }'
```

#### Problem 3: Scale Decision Rejected

**Symptoms**:
- Logs show "scale decision rejected"
- Has pending tasks but doesn't scale up

**Troubleshooting Steps**:
```bash
# View reason for decision rejection
kubectl logs <waverless-pod> | grep "scale decision rejected" -A5
```

**Common Causes**:
1. **Insufficient Resources** - GPU/CPU/memory quota full
2. **Priority Too Low** - High-priority endpoints occupying resources
3. **Cooldown Period** - Just performed scaling operation

**Solution**:
```bash
# 1. Check resource usage
curl http://localhost:8090/api/v1/autoscaler/status | jq '.clusterResources'

# 2. Increase endpoint priority
curl -X PUT http://localhost:8090/api/v1/endpoints/<endpoint> \
  -H "Content-Type: application/json" \
  -d '{"priority": 80}'  # Increase to 80 (default 50)

# 3. Scale down low-priority endpoints to release resources
```

#### Problem 4: Worker with Running Tasks Deleted During Scale Down

**Symptoms**:
- Tasks interrupted
- Logs show "task failed: worker killed"

**Troubleshooting Steps**:
```bash
# View scale down logs
kubectl logs <waverless-pod> | grep "scale down" -B5 -A5

# Check if intelligent scale down logic exists
kubectl logs <waverless-pod> | grep "selecting pods to delete"
```

**Solution**:
- Intelligent scale down implemented: Prioritize deleting idle workers
- Graceful shutdown implemented: DRAINING workers no longer receive new tasks
- If still problematic, check `terminationGracePeriodSeconds` configuration

#### Problem 5: Multi-Replica Server Causing Duplicate Scaling

**Symptoms**:
- Multiple scaling operations at the same time
- Logs show "already scaling" warnings

**Troubleshooting Steps**:
```bash
# Check distributed lock status
kubectl exec <waverless-pod> -- \
  redis-cli GET "autoscaler:lock:<endpoint>"
```

**Solution**:
- Distributed lock mechanism implemented
- If still problematic, check if Redis connection is normal

### K8s Informer Issues

#### Problem 1: Informer Not Synced

**Symptoms**:
- Pod status inaccurate
- Replica count statistics incorrect
- Deployment status delayed

**Troubleshooting Steps**:
```bash
# 1. Check informer sync status
kubectl logs <waverless-pod> | grep "informers synced successfully"

# 2. Check for errors
kubectl logs <waverless-pod> | grep -i "informer error"

# 3. View K8s API connection
kubectl logs <waverless-pod> | grep "k8s client"
```

**Common Causes**:
1. K8s API Server connection issues
2. Insufficient RBAC permissions
3. Informer cache not ready

**Solution**:
```bash
# 1. Check RBAC permissions
kubectl auth can-i list deployments --as=system:serviceaccount:default:waverless

# 2. Check ServiceAccount and RoleBinding
kubectl get sa waverless
kubectl get rolebinding waverless-binding

# 3. Restart application to wait for informer re-sync
kubectl rollout restart deployment/waverless
```

#### Problem 2: Pod Status Not Updating

**Symptoms**:
- Pod deleted but worker still ONLINE
- Pod created but worker not registered

**Troubleshooting Steps**:
```bash
# 1. Check pod informer events
kubectl logs <waverless-pod> | grep "pod.*event"

# 2. Verify pod labels
kubectl get pods -l app=<endpoint> --show-labels

# 3. Check worker PodName
curl http://localhost:8090/api/v1/workers | jq '.[] | {id, pod_name}'
```

**Solution**:
1. Ensure pods have `app` label
2. Ensure worker sets PodName during registration
3. Check informer resync period configuration

#### Problem 3: High Informer Memory Usage

**Symptoms**:
- Memory usage continuously growing
- OOMKilled

**Troubleshooting Steps**:
```bash
# Monitor memory usage
kubectl top pod <waverless-pod>

# Check informer cache size
kubectl logs <waverless-pod> | grep "informer cache size"
```

**Solution**:
1. Reduce `resyncPeriod` (default 10 minutes)
2. Use label selector to limit monitoring scope
3. Increase pod memory limit

### Worker Graceful Shutdown Issues

#### Problem 1: Pod Deleted but Worker Not Marked DRAINING

**Symptoms**:
- Pod being deleted
- Worker still receiving new tasks

**Troubleshooting Steps**:
```bash
# 1. Check if pod watcher is registered
kubectl logs <waverless-pod> | grep "Pod watcher registered successfully"

# 2. Check worker PodName
curl http://localhost:8090/api/v1/workers | jq '.[] | {id, pod_name, status}'

# 3. Check pod labels
kubectl get pod <worker-pod> -o jsonpath='{.metadata.labels}'
```

**Solution**:
1. Ensure application startup logs show "Pod watcher registered successfully"
2. Ensure worker has PodName field
3. Ensure pod has `app=<endpoint>` label

#### Problem 2: DRAINING Worker Still Receiving Tasks

**Symptoms**:
- Worker status is DRAINING
- Still assigning new tasks

**Troubleshooting Steps**:
```bash
# Check PullJobs logic
kubectl logs <waverless-pod> | grep "Worker is draining"
```

**Solution**:
- Check if code correctly implements DRAINING filtering
- Ensure DRAINING workers are excluded from task assignment

#### Problem 3: Tasks Unexpectedly Interrupted

**Symptoms**:
- Task status becomes FAILED
- Worker terminated during execution

**Troubleshooting Steps**:
```bash
# 1. Check terminationGracePeriod
kubectl get deployment <endpoint> -o jsonpath='{.spec.template.spec.terminationGracePeriodSeconds}'

# 2. Check task execution time
curl http://localhost:8090/api/v1/tasks/<task-id> | jq '.startedAt, .updatedAt'

# 3. View pod deletion logs
kubectl logs <waverless-pod> | grep "Pod.*marked for deletion" -A10
```

**Solution**:
1. Ensure `terminationGracePeriodSeconds >= taskTimeout + 30`
2. Check if task timed out
3. Verify Double-Check mechanism is working

#### Problem 4: Race Condition Causing Task Assignment to DRAINING Worker

**Symptoms**:
- Logs show "Race condition detected"
- Tasks being reverted

**This is normal behavior!** Double-Check mechanism detected race and automatically reverted assignment.

**Verification**:
```bash
# View revert logs
kubectl logs <waverless-pod> | grep "Race condition detected" -A5

# Expected output:
# Race condition detected: Worker xxx became DRAINING during task assignment
# Task xxx reverted to pending queue
```

**If occurs frequently**:
- Normal: Expected behavior in high-concurrency scenarios
- Monitor frequency: If > 0.1 times/s, may need optimization

### Task Execution Issues

#### Problem 1: Task Always PENDING

**Symptoms**:
- Task status remains PENDING after creation
- Worker online but not pulling tasks

**Troubleshooting Steps**:
```bash
# 1. Check pending queue
kubectl exec <waverless-pod> -- redis-cli LLEN "pending:<endpoint>"

# 2. Check worker status
curl http://localhost:8090/api/v1/workers | jq '.[] | select(.endpoint == "<endpoint>")'

# 3. Check worker pull logs
kubectl logs <worker-pod> | grep "pulling jobs"
```

**Common Causes**:
1. No available workers
2. Worker concurrency full
3. Worker in DRAINING status
4. Redis connection issues

#### Problem 2: Task Executed Multiple Times

**Symptoms**:
- Same task executed by multiple workers
- Duplicate task IDs

**Troubleshooting Steps**:
```bash
# Check task assignment records
kubectl logs <waverless-pod> | grep "task.*assigned" | grep "<task-id>"
```

**Solution**:
- Check if Redis transactions are correct
- Verify task status update logic

#### Problem 3: Orphaned Task Cleanup

**Symptoms**:
- Task IN_PROGRESS for long time
- Worker no longer exists

**Troubleshooting Steps**:
```bash
# Check orphaned task cleanup logs
kubectl logs <waverless-pod> | grep "orphaned task cleanup"
```

**Solution**:
- Auto-cleanup mechanism implemented (runs every 30 seconds)
- Orphaned tasks are automatically requeued or marked as failed

### Database Issues

#### Problem 1: MySQL Connection Failed

**Symptoms**:
- Application startup failed
- Logs show "MySQL connection failed"

**Troubleshooting Steps**:
```bash
# Check MySQL configuration
kubectl exec <waverless-pod> -- cat /app/config/config.yaml | grep -A10 "mysql"

# Test connection
kubectl exec <waverless-pod> -- \
  mysql -h <host> -u <user> -p<password> -e "SELECT 1"
```

#### Problem 2: Redis Connection Failed

**Symptoms**:
- Cannot create tasks
- Worker cannot register

**Troubleshooting Steps**:
```bash
# Test Redis connection
kubectl exec <waverless-pod> -- redis-cli -h <redis-host> PING
```

### Performance Issues

#### Problem 1: Slow Autoscaling Decisions

**Symptoms**:
- Has pending tasks but takes long time to scale up

**Troubleshooting Steps**:
```bash
# Check autoscaler interval
kubectl exec <waverless-pod> -- \
  cat /app/config/config.yaml | grep "interval"
```

**Solution**:
- Reduce `interval` (default 30 seconds, can change to 10-15 seconds)
- Note: Too small interval increases K8s API pressure

#### Problem 2: High Task Assignment Latency

**Symptoms**:
- Worker frequently requests but rarely gets tasks
- High latency

**Troubleshooting Steps**:
```bash
# Check pending queue length
kubectl exec <waverless-pod> -- redis-cli LLEN "pending:<endpoint>"

# Check worker concurrency situation
curl http://localhost:8090/api/v1/workers | jq '.[] | {id, concurrency, current_jobs}'
```

**Optimization Recommendations**:
1. Increase worker concurrency
2. Optimize task size
3. Check Redis performance

### Quick Diagnostic Tools

#### One-Click Check Script

```bash
#!/bin/bash
# check-waverless.sh

WAVERLESS_POD=$(kubectl get pods -l app=waverless -o jsonpath='{.items[0].metadata.name}')

echo "=== Waverless Health Check ==="

# 1. Check application status
echo "1. Application status:"
kubectl get pod $WAVERLESS_POD

# 2. Check Autoscaler
echo "2. Autoscaler status:"
kubectl logs $WAVERLESS_POD | grep -E "(autoscaler|Pod watcher)" | tail -3

# 3. Check Workers
echo "3. Worker statistics:"
kubectl exec $WAVERLESS_POD -- \
  curl -s http://localhost:8090/api/v1/workers | \
  jq '[.[] | .status] | group_by(.) | map({status: .[0], count: length})'

# 4. Check tasks
echo "4. Task statistics:"
kubectl exec $WAVERLESS_POD -- redis-cli --scan --pattern "task:*" | wc -l

# 5. Check Pending queues
echo "5. Pending queues:"
kubectl exec $WAVERLESS_POD -- redis-cli --scan --pattern "pending:*" | \
  while read key; do
    echo "$key: $(kubectl exec $WAVERLESS_POD -- redis-cli LLEN $key)"
  done

echo "=== Check Complete ==="
```

#### Log Collection

```bash
# Collect complete logs
kubectl logs <waverless-pod> > waverless.log
kubectl logs <waverless-pod> --previous > waverless-previous.log

# Collect worker logs
kubectl logs <worker-pod> > worker.log
```

#### Configuration Export

```bash
# Export configuration
kubectl exec <waverless-pod> -- cat /app/config/config.yaml > config.yaml

# Export endpoint configuration
curl http://localhost:8090/api/v1/endpoints > endpoints.json
```

### Getting Help

When submitting issues, please provide:
1. Waverless version
2. K8s version
3. Complete logs
4. Configuration files
5. Reproduction steps

**Issue Feedback**: https://github.com/wavespeedai/waverless/issues

---

**Document Version**: v1.0
**Last Updated**: 2025-11-14
**Maintained By**: Waverless Team
