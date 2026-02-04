# Waverless Web UI

Web-based management interface for Waverless serverless GPU platform.

## Features

- **Dashboard**: System overview with endpoint statistics and resource utilization
- **Endpoints**: Create, update, scale, and manage worker deployments
- **Endpoint Detail**: Monitor workers, view logs, configure autoscaling
- **Tasks**: View task history, status, and execution details
- **Specs**: Browse available hardware specifications (GPU types)
- **Serverless**: Quick deployment wizard for new endpoints

## Tech Stack

| Technology | Purpose |
|------------|---------|
| React 18 | UI framework |
| TypeScript | Type safety |
| Ant Design | UI components |
| React Query | Data fetching & caching |
| React Router | Client-side routing |
| Zustand | State management |
| Monaco Editor | Code/YAML editing |
| ECharts | Data visualization |
| Vite | Build tool |

## Quick Start

### Development

```bash
# Install dependencies
pnpm install

# Start development server
pnpm dev
```

Access http://localhost:5173

### Production Build

```bash
# Build for production
pnpm build

# Preview production build
pnpm preview
```

Built files output to `dist/` directory.

## Configuration

### Environment Variables

Create `.env` file in `web-ui` directory:

```bash
# API Backend URL
VITE_API_BACKEND_URL=http://localhost:8080

# Admin credentials (for build-time injection)
VITE_ADMIN_USERNAME=admin
VITE_ADMIN_PASSWORD=admin
```

### Environment Examples

| Environment | VITE_API_BACKEND_URL |
|-------------|---------------------|
| Local Dev | `http://localhost:8080` |
| K8s Dev | `http://waverless-svc:8080` |
| Production | `https://api.yourcompany.com` |

## Pages

### Dashboard
- System-wide statistics overview
- Endpoint count, worker count, task metrics
- Quick Start panel for API testing

### Endpoints
- List all endpoints with status
- Create new endpoint deployments
- Scale replicas up/down
- Delete endpoints

### Endpoint Detail
- Worker list with status indicators
- Real-time logs viewer
- Autoscaling configuration
- Task history for this endpoint

### Tasks
- Global task list with filtering
- Task status (PENDING, IN_PROGRESS, COMPLETED, FAILED)
- Execution time and queue time metrics
- Input/output data viewer

### Specs
- Available hardware specifications
- GPU types, CPU, memory configurations
- Platform-specific settings

### Serverless
- Guided deployment wizard
- Image selection and configuration
- Resource spec selection
- Environment variable setup

## Quick Start Panel

The Dashboard includes a collapsible Quick Start panel for API testing:

- **Test Modes**:
  - `/run` - Async task submission
  - `/runsync` - Sync task submission
  - `/status` - Query task status

- **Features**:
  - JSON input editor
  - Auto-fill task ID from async responses
  - Code examples (cURL, Python, JavaScript)
  - Copy-to-clipboard support

## Docker Deployment

```bash
docker build -t waverless-web:latest \
  --build-arg VITE_API_BACKEND_URL=http://api:8080 \
  --build-arg VITE_ADMIN_USERNAME=admin \
  --build-arg VITE_ADMIN_PASSWORD=admin \
  .
```

The Docker image uses nginx to serve static files with runtime environment variable substitution.

## Project Structure

```
web-ui/
├── src/
│   ├── api/           # API client
│   ├── components/    # Reusable components
│   │   └── Layout/    # Sidebar, Header
│   ├── pages/         # Page components
│   │   ├── Dashboard/
│   │   ├── Endpoints/
│   │   ├── EndpointDetail/
│   │   ├── Tasks/
│   │   ├── Specs/
│   │   ├── Serverless/
│   │   └── Login/
│   ├── styles/        # CSS styles
│   ├── types/         # TypeScript types
│   ├── utils/         # Utility functions
│   ├── App.tsx        # Main app component
│   └── main.tsx       # Entry point
├── public/            # Static assets
├── index.html         # HTML template
├── vite.config.ts     # Vite configuration
└── tsconfig.json      # TypeScript configuration
```

## Authentication

Simple username/password authentication with localStorage persistence.

Default credentials: `admin` / `admin`

Configure via environment variables or build args.

---

**Related Documentation**: [../docs/USER_GUIDE.md](../docs/USER_GUIDE.md)
