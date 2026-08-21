<div align="center">
  <a href="https://wavespeed.ai" target="_blank">
    <img src="docs/images/wavespeed-dark-logo.png" alt="WaveSpeedAI Logo" width="200"/>
  </a>

  <h1>Waverless</h1>
  <p><strong>High-performance Serverless GPU Task Orchestration System</strong></p>
  <p>
    <a href="https://wavespeed.ai">🌐 wavespeed.ai</a> •
    <a href="docs/ARCHITECTURE.md">📐 Architecture</a> •
    <a href="docs/USER_GUIDE.md">📖 User Guide</a> •
    <a href="docs/DEVELOPER_GUIDE.md">🔧 Developer Guide</a>
  </p>
</div>

---

## Features

- 🚀 **Pull-based Architecture** - Workers actively pull tasks for better load balancing
- 🔌 **RunPod Compatible** - Zero-code migration from runpod-python SDK
- ☸️ **Multi-Provider** - Kubernetes, Novita Serverless, Docker backends
- 📊 **Smart Autoscaling** - Queue-depth, priority, and resource-aware scaling
- 🛡️ **Graceful Shutdown** - Zero task loss during rolling updates

## Architecture

```mermaid
flowchart TB
    subgraph Clients
        direction LR
        Client[Client V1 API]
        WebUI[Web UI]
    end

    subgraph Core["Waverless API Server"]
        direction TB
        Queue[Task Queue]
        WM[Worker Mgmt]
        Autoscaler[Autoscaler]
        Store[(Redis + MySQL)]
    end

    subgraph Provider
        direction LR
        K8s[K8s]
        Novita[Novita]
        Docker[Docker]
    end

    subgraph Workers
        direction LR
        W1[Worker A]
        W2[Worker B]
        W3[Worker ...]
    end

    Clients -->|submit| Core
    Core --> Provider
    Provider -->|manage| Workers
    Workers -->|pull tasks| Core

    style Clients fill:#4a90a4,color:#fff
    style Core fill:#2d5a7b,color:#fff
    style Provider fill:#5d8aa8,color:#fff
    style Workers fill:#7fb3d3,color:#000
```

## Quick Start

```bash
# Local development
docker-compose up -d mysql redis
cp config/config.example.yaml config/config.yaml
go run cmd/main.go

# Kubernetes deployment
./deploy.sh install
```

## API Example

```bash
# Submit task
curl -X POST http://localhost:8090/v1/my-endpoint/run \
  -H "Content-Type: application/json" \
  -d '{"input": {"prompt": "hello world"}}'

# Check status
curl http://localhost:8090/v1/status/{task_id}
```

## Documentation

| Document | Description |
|----------|-------------|
| [Architecture](docs/ARCHITECTURE.md) | System design, components, data flow, lifecycle |
| [User Guide](docs/USER_GUIDE.md) | Deployment, API reference, autoscaling, troubleshooting |
| [Developer Guide](docs/DEVELOPER_GUIDE.md) | Code structure, core design, provider integration |

## License

MIT License

---

**[WaveSpeed AI](https://wavespeed.ai/)** — hosted inference for image, video, audio and 3D models.
Try it in the browser: **[Image generator](https://wavespeed.ai/image-generator)** · **[Video generator](https://wavespeed.ai/video-generator)**
