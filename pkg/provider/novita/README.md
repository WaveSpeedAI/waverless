# Novita Serverless Provider

This package implements the `DeploymentProvider` interface for [Novita AI Serverless](https://novita.ai) platform.

## Features

### ✅ Implemented Core Features

- **Deploy**: Create serverless endpoints with auto-scaling workers
- **GetApp**: Retrieve endpoint details and status
- **ListApps**: List all deployed endpoints
- **DeleteApp**: Delete endpoints and clean up resources
- **ScaleApp**: Scale worker count (min/max replicas)
- **GetAppStatus**: Get real-time endpoint status
- **UpdateDeployment**: Update endpoint configuration (image, replicas, env vars)
- **ListSpecs**: List available GPU specifications
- **GetSpec**: Get specific spec details
- **PreviewDeploymentYAML**: Preview Novita configuration as JSON
- **WatchReplicas**: Monitor endpoint status changes via polling (configurable interval)

### ⚠️ Limitations & Differences

The following features are **not supported** by Novita's API and will return friendly error messages:

- **GetAppLogs**: Logs must be accessed via [Novita Dashboard](https://console.novita.ai)
- **GetPods / DescribePod / GetPodYAML**: Novita manages workers internally; use `GetApp` for worker status
- **ListPVCs**: Storage is managed by Novita; persistent storage not yet supported
- **Volume Mounts**: PVC volume mounts are not supported (Novita uses network storage)
- **ShmSize**: Shared memory size configuration not applicable to Novita
- **EnablePtrace**: Ptrace capability not applicable to Novita

**Behavioral Differences:**

- **Replica Watching**: Uses polling instead of real-time watch (configurable interval)
- **Worker Lifecycle**: Workers are managed by Novita's auto-scaling system
- **Health Checks**: Fixed to `/health` endpoint on port 8000
- **Networking**: Endpoints are accessible via Novita's managed load balancer
- **Region**: Specified per-spec (not per-deployment) in `specs.yaml`

## Configuration

### 1. Enable Novita Provider

Edit `config/config.yaml`:

```yaml
providers:
  deployment: "novita"  # Switch from k8s to novita

novita:
  enabled: true
  api_key: "your-novita-api-key-here"     # Your Novita API key (Bearer token)
  base_url: "https://api.novita.ai"       # Novita API base URL
  config_dir: "./config"                   # Configuration directory (contains specs.yaml)
  poll_interval: 10                        # Status polling interval in seconds (default: 10)
```

### 2. Configure Specs

Add Novita-compatible specs to `config/specs.yaml`:

```yaml
specs:
  # Novita 5090 Single GPU
  - name: "novita-5090-single"
    displayName: "Novita 5090 1x GPU"
    category: "gpu"
    resourceType: "serverless"
    resources:
      gpu: "1"
      gpuType: "NVIDIA GeForce RTX 5090"
      cpu: "12"
      memory: "50"
      ephemeralStorage: "100"  # Rootfs size in GB
    platforms:
      novita:
        productId: "SL-serverless-3"  # Get from Novita Console
        region: "us-dallas-nas-2"     # Region/cluster ID
        cudaVersion: "any"             # Optional: CUDA version

  # Novita 4090 Single GPU
  - name: "novita-4090-single"
    displayName: "Novita 4090 1x GPU"
    category: "gpu"
    resourceType: "serverless"
    resources:
      gpu: "1"
      gpuType: "NVIDIA GeForce RTX 4090"
      cpu: "8"
      memory: "32"
      ephemeralStorage: "50"
    platforms:
      novita:
        productId: "1"  # Replace with actual Novita product ID
        region: "us-dallas-nas-2"
        cudaVersion: "any"
```

**Important Notes:**
- `ephemeralStorage`: Specifies rootfs disk size in GB (e.g., "100" = 100GB)
- `productId`: Get from [Novita Console](https://console.novita.ai)
- `region`: Novita cluster/region ID (e.g., "us-dallas-nas-2")
- `cudaVersion`: Optional CUDA version specification

## Usage

### Deploy Endpoint

**Via API:**

```bash
curl -X POST http://localhost:8090/api/v1/endpoints \
  -H "Content-Type: application/json" \
  -d '{
    "endpoint": "base-test",
    "specName": "novita-5090-single",
    "image": "ubuntu:22.04",
    "replicas": 3,
    "taskTimeout": 1200,
    "env": {
      "MODEL_NAME": "llama-3-70b"
    }
  }'
```

**Via Go SDK:**

```go
import (
    "context"
    "waverless/pkg/interfaces"
)

req := &interfaces.DeployRequest{
    Endpoint:    "my-inference-endpoint",
    SpecName:    "novita-5090-single",
    Image:       "your-docker-image:latest",
    Replicas:    2,
    TaskTimeout: 1200,  // Task timeout in seconds
    Env: map[string]string{
        "MODEL_NAME": "llama-3-70b",
    },
}

resp, err := provider.Deploy(ctx, req)
```

**Request Fields:**
- `endpoint` (required): Unique endpoint name
- `specName` (required): Spec name from `specs.yaml`
- `image` (required): Docker image URL
- `replicas` (optional): Number of workers (default: 1)
- `taskTimeout` (optional): Task timeout in seconds (default: 3600)
- `env` (optional): Environment variables
- `volumeMounts` (optional): PVC volume mounts (not fully supported by Novita yet)
- `shmSize` (optional): Shared memory size (e.g., "1Gi", not applicable to Novita)
- `enablePtrace` (optional): Enable ptrace capability (not applicable to Novita)

**Complete API Request Example (with all fields):**

```json
{
  "endpoint": "base-test",
  "specName": "novita-5090-single",
  "image": "ubuntu:22.04",
  "replicas": 3,
  "taskTimeout": 1200,
  "maxPendingTasks": 10,
  "env": {
    "MODEL_NAME": "llama-3-70b",
    "MODEL_VERSION": "v1.0"
  },
  "minReplicas": 1,
  "maxReplicas": 10,
  "scaleUpThreshold": 2,
  "scaleDownIdleTime": 300,
  "scaleUpCooldown": 30,
  "scaleDownCooldown": 60,
  "priority": 50,
  "enableDynamicPrio": true,
  "highLoadThreshold": 10,
  "priorityBoost": 20
}
```

**Autoscaling Fields (optional):**
- `minReplicas`: Minimum replica count (default: 0, scale-to-zero)
- `maxReplicas`: Maximum replica count (default: 10)
- `scaleUpThreshold`: Queue threshold for scale up (default: 1)
- `scaleDownIdleTime`: Idle time before scale down in seconds (default: 300)
- `scaleUpCooldown`: Scale up cooldown in seconds (default: 30)
- `scaleDownCooldown`: Scale down cooldown in seconds (default: 60)
- `priority`: Priority for resource allocation (0-100, default: 50)
- `enableDynamicPrio`: Enable dynamic priority (default: true)
- `highLoadThreshold`: High load threshold for priority boost (default: 10)
- `priorityBoost`: Priority boost amount when high load (default: 20)

### Region Configuration

Region is specified in the spec configuration (`config/specs.yaml`):

```yaml
platforms:
  novita:
    productId: "SL-serverless-3"
    region: "us-dallas-nas-2"  # Novita cluster/region ID
```

The region value is used as the `clusterID` when creating Novita endpoints. Common regions include:
- `us-dallas-nas-2` (US Dallas)
- `us-west-1` (US West)
- `us-east-1` (US East)

For available regions, check the [Novita Console](https://console.novita.ai).

### Preview Deployment Configuration

Preview the Novita API configuration before deploying:

**Via API:**

```bash
curl -X POST http://localhost:8090/api/v1/endpoints/preview \
  -H "Content-Type: application/json" \
  -d '{
    "endpoint": "base-test",
    "specName": "novita-5090-single",
    "image": "ubuntu:22.04",
    "replicas": 3,
    "taskTimeout": 1200
  }'
```

This returns the complete Novita API request JSON that will be sent to create the endpoint.

### Scale Endpoint

```bash
curl -X POST http://localhost:8090/api/v1/endpoints/base-test/scale \
  -H "Content-Type: application/json" \
  -d '{"replicas": 5}'
```

**Via Go SDK:**

```go
// Scale to 5 workers
err := provider.ScaleApp(ctx, "base-test", 5)
```

### Get Endpoint Status

**Via API:**

```bash
# Get detailed endpoint info
curl http://localhost:8090/api/v1/endpoints/base-test

# Get status only
curl http://localhost:8090/api/v1/endpoints/base-test/status
```

**Via Go SDK:**

```go
// Get detailed info
app, err := provider.GetApp(ctx, "base-test")
fmt.Printf("Status: %s, Replicas: %d/%d\n", 
    app.Status, app.ReadyReplicas, app.Replicas)

// Get status only
status, err := provider.GetAppStatus(ctx, "base-test")
fmt.Printf("Ready Workers: %d/%d\n", 
    status.ReadyReplicas, status.TotalReplicas)
```

### List All Endpoints

**Via API:**

```bash
curl http://localhost:8090/api/v1/endpoints
```

**Via Go SDK:**

```go
apps, err := provider.ListApps(ctx)
for _, app := range apps {
    fmt.Printf("Endpoint: %s, Status: %s, Replicas: %d/%d\n",
        app.Name, app.Status, app.ReadyReplicas, app.Replicas)
}
```

### Delete Endpoint

**Via API:**

```bash
curl -X DELETE http://localhost:8090/api/v1/endpoints/base-test
```

**Via Go SDK:**

```go
err := provider.DeleteApp(ctx, "base-test")
```

### Update Deployment

**Via API:**

```bash
curl -X PATCH http://localhost:8090/api/v1/endpoints/base-test/deployment \
  -H "Content-Type: application/json" \
  -d '{
    "image": "ubuntu:22.04",
    "replicas": 5,
    "env": {
      "MODEL_VERSION": "v2"
    }
  }'
```

**Via Go SDK:**

```go
replicas := 3
req := &interfaces.UpdateDeploymentRequest{
    Endpoint: "my-inference-endpoint",
    Image:    "new-image:v2",
    Replicas: &replicas,
    Env: &map[string]string{
        "MODEL_VERSION": "v2",
    },
}

resp, err := provider.UpdateDeployment(ctx, req)
```

**Update Fields (all optional):**
- `image`: New Docker image
- `replicas`: New worker count (pointer to distinguish from zero)
- `env`: New environment variables (replaces all existing env vars)
- `taskTimeout`: New task timeout in seconds

### Watch Status Changes

Monitor endpoint status changes in real-time:

```go
// Register callback to monitor endpoint status changes
err := provider.WatchReplicas(ctx, func(event interfaces.ReplicaEvent) {
    fmt.Printf("[%s] Status changed: desired=%d, ready=%d, available=%d, status=%v\n",
        event.Name, 
        event.DesiredReplicas, 
        event.ReadyReplicas, 
        event.AvailableReplicas,
        event.Conditions)
})
```

**Polling Mechanism:**

Unlike K8s which provides real-time watch API, Novita provider uses a **polling mechanism**:

1. Polls all endpoints at configured interval (default: 10s)
2. Compares current state with cached previous state
3. Triggers callback only when state changes (replicas, status, etc.)
4. Automatically handles endpoint lifecycle (creation, deletion)

**Configure Poll Interval:**

```yaml
novita:
  poll_interval: 10  # Poll every 10 seconds (default: 10)
```

**Status Change Events:**

The callback receives `ReplicaEvent` with:
- `Name`: Endpoint name
- `DesiredReplicas`: Target worker count (workerConfig.maxNum)
- `ReadyReplicas`: Number of healthy workers
- `AvailableReplicas`: Number of running workers
- `Conditions`: Status conditions (Available, Progressing, Failed, etc.)

## API Mapping

### Waverless → Novita Field Mapping

| Waverless Field | Novita API Field | Notes |
|-----------------|------------------|-------|
| `endpoint` | `endpoint.name` | Unique endpoint identifier |
| `specName` | `products[].id` + `clusterID` | Product ID and region from `specs.yaml` |
| `replicas` | `workerConfig.minNum/maxNum` | Initially set to same value for fixed count |
| `image` | `image.image` | Docker image URL |
| `env` | `envs[]` | Array of `{key, value}` objects |
| `taskTimeout` | `workerConfig.freeTimeout` | Worker idle timeout in seconds (default: 300) |
| - | `workerConfig.requestTimeout` | Request timeout (default: 3600) |
| `spec.resources.gpu` | `workerConfig.gpuNum` | Number of GPUs per worker |
| `spec.resources.ephemeralStorage` | `endpoint.rootfsSize` | Rootfs disk size in GB |
| `spec.platforms.novita.region` | `endpoint.clusterID` | Novita cluster/region ID |
| `spec.platforms.novita.productId` | `products[0].id` | Novita product/GPU type ID |
| `spec.platforms.novita.cudaVersion` | `workerConfig.cudaVersion` | CUDA version (optional) |

### Novita-Specific Default Values

| Field | Default Value | Description |
|-------|--------------|-------------|
| `ports` | `[{port: 8000}]` | Default HTTP port for health check and requests |
| `policy` | `{type: "queue", value: 60}` | Auto-scaling policy: queue wait time |
| `healthy.path` | `/health` | Health check endpoint path |
| `workerConfig.maxConcurrent` | `1` | Max concurrent requests per worker |
| `workerConfig.freeTimeout` | `300` | Worker idle timeout (5 minutes) |
| `workerConfig.requestTimeout` | `3600` | Request timeout (1 hour) |

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│          NovitaDeploymentProvider                       │
├─────────────────────────────────────────────────────────┤
│  - Deploy / GetApp / ListApps / DeleteApp               │
│  - ScaleApp / GetAppStatus / UpdateDeployment           │
│  - Endpoint ID caching (name -> ID mapping)             │
└──────────────────┬──────────────────────────────────────┘
                   │
         ┌─────────┴─────────┐
         │                   │
    ┌────▼─────┐      ┌─────▼──────┐
    │  Client  │      │   Mapper   │
    ├──────────┤      ├────────────┤
    │ HTTP API │      │ Data       │
    │ calls to │      │ conversion │
    │ Novita   │      │ logic      │
    └──────────┘      └────────────┘
         │
         ▼
  Novita AI API
```

## Testing

### Manual Testing

1. Set up configuration:
   ```bash
   cp config/config.example.yaml config/config.yaml
   # Edit config.yaml with your Novita API key
   ```

2. Add test specs:
   ```bash
   cat config/specs-novita-example.yaml >> config/specs.yaml
   ```

3. Deploy test endpoint:
   ```bash
   curl -X POST http://localhost:8090/api/v1/endpoints \
     -H "Content-Type: application/json" \
     -d '{
       "endpoint": "test-endpoint",
       "specName": "novita-5090-single",
       "image": "ubuntu:22.04",
       "replicas": 1,
       "taskTimeout": 1200
     }'
   ```

## Troubleshooting

### Error: "novita provider is not enabled"

**Cause**: Provider not enabled in configuration

**Solution**: Enable Novita provider in `config.yaml`:

```yaml
providers:
  deployment: "novita"

novita:
  enabled: true
  api_key: "your-api-key"
```

### Error: "novita API key is required"

**Cause**: Missing API key in configuration

**Solution**: Set `novita.api_key` in `config.yaml`:

```yaml
novita:
  api_key: "your-novita-api-key-here"
```

Get API key from [Novita Console](https://console.novita.ai).

### Error: "no Novita product ID found for spec"

**Cause**: Missing platform configuration for the spec

**Solution**: Add `platforms.novita` section to your spec in `specs.yaml`:

```yaml
platforms:
  novita:
    productId: "SL-serverless-3"  # Get from Novita Console
    region: "us-dallas-nas-2"
```

### Error: "endpoint not found in Novita"

**Cause**: Endpoint was deleted outside Waverless or cache is stale

**Solution**: The provider automatically refreshes the cache on next API call. If the endpoint truly doesn't exist, create a new one.

### Error: "failed to parse rootfs size"

**Cause**: Invalid `ephemeralStorage` format in spec resources

**Solution**: Use numeric value (GB) without unit:

```yaml
resources:
  ephemeralStorage: "100"  # 100GB (correct)
  # NOT: "100Gi" or "100GB"
```

### Error: "failed to create Novita endpoint" (401 Unauthorized)

**Cause**: Invalid API key or expired token

**Solution**: 
1. Verify API key in [Novita Console](https://console.novita.ai)
2. Update `config.yaml` with correct API key
3. Restart Waverless service

### Error: "failed to create Novita endpoint" (404 Not Found)

**Cause**: Invalid product ID or region

**Solution**: 
1. Check available products in [Novita Console](https://console.novita.ai)
2. Update `specs.yaml` with correct `productId` and `region`
3. Verify the product is available in the specified region

## Development

### Adding New Features

1. Check if Novita API supports the feature
2. Add types to `types.go` if needed
3. Implement in `client.go` (HTTP calls)
4. Add mapping logic in `mapper.go`
5. Expose via `provider.go`

### Testing Locally

```bash
# Run with Novita provider
go run cmd/main.go --config config/config.yaml
```

## References

- [Novita AI Serverless API Documentation](https://novita.ai/docs/api-reference/serverless-create-endpoint)
- [Novita Console](https://console.novita.ai)
- [Waverless Architecture](../../../docs/ARCHITECTURE.md)
