import axios from 'axios';
import type {
  AppInfo,
  SpecInfo,
  DeployRequest,
  UpdateDeploymentRequest,
  UpdateEndpointConfigRequest,
  DeployResponse,
  Task,
  TaskListParams,
  TaskListResponse,
  EndpointStats,
  WorkerWithPodInfo,
  PodDetail,
  TaskEventsResponse,
  TaskTimelineResponse,
  TaskExecutionHistoryResponse,
  PVCInfo,
  StatusEvent,
  StatusEventFilter,
} from '@/types';

const client = axios.create({
  baseURL: '/api/v1',
  timeout: 30000,
  headers: {
    'Content-Type': 'application/json',
  },
});

// Request interceptor
client.interceptors.request.use(
  (config) => {
    return config;
  },
  (error) => {
    return Promise.reject(error);
  }
);

// Response interceptor
client.interceptors.response.use(
  (response) => {
    return response;
  },
  (error) => {
    const message = error.response?.data?.error || error.message || 'Unknown error';
    console.error('API Error:', message);
    return Promise.reject(error);
  }
);

export const api = {
  // Generic get for custom endpoints
  get: (url: string, config?: any) => axios.get(url, { ...config, baseURL: window.location.origin }),

  // Endpoints (renamed from apps)
  endpoints: {
    list: () => client.get<{ endpoints: AppInfo[] }>('/endpoints'),
    get: (name: string) => client.get<AppInfo>(`/endpoints/${name}`),
    create: (data: DeployRequest) => client.post<DeployResponse>('/endpoints', data),
    update: (name: string, data: UpdateDeploymentRequest) =>
      client.patch<DeployResponse>(`/endpoints/${name}/deployment`, data),
    updateDeployment: (name: string, data: UpdateDeploymentRequest) =>
      client.patch<DeployResponse>(`/endpoints/${name}/deployment`, data),
    updateMetadata: (name: string, data: UpdateEndpointConfigRequest) =>
      client.put<{ message: string; name: string }>(`/endpoints/${name}`, data),
    delete: (name: string) => client.delete(`/endpoints/${name}`),
    logs: (name: string, lines: number = 100, podName?: string) =>
      client.get<string>(`/endpoints/${name}/logs`, { params: { lines, pod_name: podName } }),
    getWorkers: (name: string) => client.get<{ workers: WorkerWithPodInfo[] }>(`/endpoints/${name}/workers`),
    describePod: (endpoint: string, podName: string) =>
      client.get<PodDetail>(`/endpoints/${endpoint}/workers/${podName}/describe`),
    // Status events for endpoint (Requirements 3.4, 3.5)
    getStatusEvents: (name: string, params?: Omit<StatusEventFilter, 'endpoint'>) =>
      client.get<StatusEvent[]>(`/endpoints/${name}/status-events`, {
        params: {
          limit: params?.limit,
          offset: params?.offset,
          start_time: params?.startTime,
          end_time: params?.endTime,
        },
      }),
  },

  // Status Events API (Requirements 3.4, 3.5)
  statusEvents: {
    // Get status events for an endpoint
    listByEndpoint: (endpoint: string, params?: Omit<StatusEventFilter, 'endpoint'>) =>
      client.get<StatusEvent[]>(`/endpoints/${endpoint}/status-events`, {
        params: {
          limit: params?.limit,
          offset: params?.offset,
          start_time: params?.startTime,
          end_time: params?.endTime,
        },
      }),
    // Get status events for a worker
    listByWorker: (workerId: string, params?: Omit<StatusEventFilter, 'workerId'>) =>
      client.get<StatusEvent[]>(`/workers/${workerId}/status-events`, {
        params: {
          limit: params?.limit,
          offset: params?.offset,
          start_time: params?.startTime,
          end_time: params?.endTime,
        },
      }),
  },

  // Apps (legacy alias)
  apps: {
    list: () => client.get<AppInfo[]>('/endpoints'),
    get: (name: string) => client.get<AppInfo>(`/endpoints/${name}`),
    deploy: (data: DeployRequest) => client.post<DeployResponse>('/endpoints', data),
    update: (name: string, data: UpdateDeploymentRequest) =>
      client.patch<DeployResponse>(`/endpoints/${name}/deployment`, data),
    updateMetadata: (name: string, data: UpdateEndpointConfigRequest) =>
      client.put<{ message: string; name: string }>(`/endpoints/${name}`, data),
    delete: (name: string) => client.delete(`/endpoints/${name}`),
    logs: (name: string, lines: number = 100, podName?: string) =>
      client.get<string>(`/endpoints/${name}/logs`, { params: { lines, pod_name: podName } }),
    workers: (name: string) => client.get<WorkerWithPodInfo[]>(`/endpoints/${name}/workers`),
    describePod: (endpoint: string, podName: string) =>
      client.get<PodDetail>(`/endpoints/${endpoint}/workers/${podName}/describe`),
    getPodYaml: (endpoint: string, podName: string) =>
      client.get<string>(`/endpoints/${endpoint}/workers/${podName}/yaml`, {
        headers: { Accept: 'text/plain' },
      }),
    previewYaml: (data: DeployRequest) =>
      client.post<string>('/endpoints/preview', data, {
        headers: { Accept: 'text/plain' },
      }),
    checkImage: (name: string) => client.post<{
      endpoint: string;
      currentImage: string;
      currentDigest: string;
      newDigest: string;
      updateAvailable: boolean;
      lastChecked: string;
    }>(`/endpoints/${name}/check-image`),
    checkAllImages: () => client.post<{
      message: string;
      totalChecked: number;
      updatesFound: number;
      notifiedCount: number;
      results: Array<{
        endpoint: string;
        currentImage: string;
        updateAvailable: boolean;
        currentDigest?: string;
        newDigest?: string;
        error?: string;
      }>;
    }>('/endpoints/check-images'),
  },

  // Specs (CRUD from database)
  specs: {
    list: (category?: string) => client.get<SpecInfo[]>('/specs', { params: { category } }),
    listWithCapacity: () => client.get<SpecInfo[]>('/specs/capacity'),
    get: (name: string) => client.get<SpecInfo>(`/specs/${name}`),
    getCapacity: (name: string) => client.get<{ specName: string; status: string }>(`/specs/${name}/capacity`),
    create: (data: {
      name: string;
      displayName: string;
      category: string;
      resourceType: string; // fixed, serverless
      resources: {
        cpu?: string;
        memory: string;
        gpu?: string;
        gpuType?: string;
        ephemeralStorage: string;
        shmSize?: string;
      };
      platforms?: Record<string, any>;
    }) => client.post<SpecInfo>('/specs', data),
    update: (name: string, data: {
      displayName?: string;
      category?: string;
      resourceType?: string; // fixed, serverless
      resources?: {
        cpu?: string;
        memory?: string;
        gpu?: string;
        gpuType?: string;
        ephemeralStorage?: string;
        shmSize?: string;
      };
      platforms?: Record<string, any>;
      status?: string;
    }) => client.put<SpecInfo>(`/specs/${name}`, data),
    delete: (name: string) => client.delete<{ message: string; name: string }>(`/specs/${name}`),
  },

  // K8s resources
  k8s: {
    listPVCs: () => client.get<PVCInfo[]>('/k8s/pvcs'),
  },

  // Configuration
  config: {
    getDefaultEnv: () => client.get<Record<string, string>>('/config/default-env'),
  },

  // Tasks - Note: task APIs don't have /api prefix, using absolute path
  tasks: {
    list: (params?: TaskListParams) => axios.get<TaskListResponse>('/v1/tasks', {
      params,
      baseURL: window.location.origin,
    }),
    get: (taskId: string) => axios.get<Task>(`/v1/status/${taskId}`, {
      baseURL: window.location.origin,
    }),
    cancel: (taskId: string) => axios.post(`/v1/cancel/${taskId}`, {}, {
      baseURL: window.location.origin,
    }),
    submit: (endpoint: string, input: any) => axios.post<{ id: string; status: string }>(`/v1/${endpoint}/run`, { input }, {
      baseURL: window.location.origin,
    }),
    submitSync: (endpoint: string, input: any) => axios.post<Task>(`/v1/${endpoint}/runsync`, { input }, {
      baseURL: window.location.origin,
    }),
    getEvents: (taskId: string) => client.get<TaskEventsResponse>(`/tasks/${taskId}/events`),
    getTimeline: (taskId: string) => client.get<TaskTimelineResponse>(`/tasks/${taskId}/timeline`),
    getExecutionHistory: (taskId: string) => client.get<TaskExecutionHistoryResponse>(`/tasks/${taskId}/execution-history`),
  },

  // Stats
  stats: {
    getEndpointStats: (endpoint: string) => axios.get<EndpointStats>(`/v1/${endpoint}/stats`, {
      baseURL: window.location.origin,
    }),
  },

  // AutoScaler
  autoscaler: {
    getStatus: () => client.get('/autoscaler/status'),
    // Lightweight endpoints for better performance
    getClusterResources: () => client.get('/autoscaler/cluster-resources'),
    getRecentEvents: (limit: number = 10) =>
      client.get('/autoscaler/recent-events', { params: { limit } }),
    // Control
    enable: () => client.post('/autoscaler/enable'),
    disable: () => client.post('/autoscaler/disable'),
    trigger: (endpoint?: string) =>
      client.post(endpoint ? `/autoscaler/trigger/${endpoint}` : '/autoscaler/trigger'),
    // Configuration
    getGlobalConfig: () => client.get('/autoscaler/config'),
    updateGlobalConfig: (config: any) => client.put('/autoscaler/config', config),
    listEndpoints: () => client.get('/autoscaler/endpoints'),
    getEndpointConfig: (name: string) => client.get(`/autoscaler/endpoints/${name}`),
    updateEndpointConfig: (name: string, config: any) =>
      client.put(`/autoscaler/endpoints/${name}`, config),
    // History
    getHistory: (name: string, limit?: number) =>
      client.get(`/autoscaler/history/${name}`, { params: { limit } }),
  },

  // Statistics
  statistics: {
    getOverview: () => client.get<{
      total: number;
      pending: number;
      in_progress: number;
      completed: number;
      failed: number;
      cancelled: number;
      updated_at: string;
    }>('/statistics/overview'),
    getTopEndpoints: (limit?: number) => client.get<{
      endpoints: Array<{
        endpoint: string;
        total: number;
        pending: number;
        in_progress: number;
        completed: number;
        failed: number;
        cancelled: number;
        updated_at: string;
      }>;
      total: number;
    }>('/statistics/endpoints', { params: { limit } }),
    getEndpointStats: (endpoint: string) => client.get<{
      endpoint: string;
      total: number;
      pending: number;
      in_progress: number;
      completed: number;
      failed: number;
      cancelled: number;
      updated_at: string;
    }>(`/statistics/endpoints/${endpoint}`),
  },

  // GPU Usage
  gpuUsage: {
    getMinuteStats: (params?: {
      scope_type?: string;
      scope_value?: string;
      start_time?: string;
      end_time?: string;
    }) => client.get<{
      data: Array<{
        time_bucket: string;
        scope_type: string;
        scope_value?: string;
        total_tasks: number;
        completed_tasks: number;
        failed_tasks: number;
        total_gpu_seconds: number;
        total_gpu_hours: number;
        avg_gpu_count: number;
        max_gpu_count: number;
        period_start: string;
        period_end: string;
      }>;
      total: number;
      start_time: string;
      end_time: string;
      summary: {
        total_gpu_hours: number;
        total_tasks: number;
      };
    }>('/gpu-usage/minute', { params }),
    getHourlyStats: (params?: {
      scope_type?: string;
      scope_value?: string;
      start_time?: string;
      end_time?: string;
    }) => client.get<{
      data: Array<{
        time_bucket: string;
        scope_type: string;
        scope_value?: string;
        total_tasks: number;
        completed_tasks: number;
        failed_tasks: number;
        total_gpu_hours: number;
        avg_gpu_count: number;
        max_gpu_count: number;
        peak_minute?: string;
        peak_gpu_hours?: number;
        period_start: string;
        period_end: string;
      }>;
      total: number;
      start_time: string;
      end_time: string;
      summary: {
        total_gpu_hours: number;
        total_tasks: number;
        max_gpu_count: number;
      };
    }>('/gpu-usage/hourly', { params }),
    getDailyStats: (params?: {
      scope_type?: string;
      scope_value?: string;
      start_time?: string;
      end_time?: string;
    }) => client.get<{
      data: Array<{
        time_bucket: string;
        scope_type: string;
        scope_value?: string;
        total_tasks: number;
        completed_tasks: number;
        failed_tasks: number;
        total_gpu_hours: number;
        avg_gpu_count: number;
        max_gpu_count: number;
        peak_hour?: string;
        peak_gpu_hours?: number;
        period_start: string;
        period_end: string;
      }>;
      total: number;
      start_time: string;
      end_time: string;
      summary: {
        total_gpu_hours: number;
        total_tasks: number;
        avg_gpu_hours_per_day: number;
      };
    }>('/gpu-usage/daily', { params }),
    triggerAggregation: (params?: {
      granularity?: string;
      start_time?: string;
      end_time?: string;
    }) => client.post<{
      message: string;
      granularity: string;
      range: {
        start: string;
        end: string;
      };
    }>('/gpu-usage/aggregate', null, {
      params,
      timeout: 300000  // 5 minutes for aggregation
    }),
    backfillHistoricalData: (params?: {
      batch_size?: number;
      max_tasks?: number;
    }) => client.post<{
      totalTasksProcessed: number;
      recordsCreated: number;
      recordsSkipped: number;
      errors?: string[];
      startTime: string;
      endTime: string;
      duration: string;
    }>('/gpu-usage/backfill', null, {
      params,
      timeout: 600000  // 10 minutes for backfill
    }),
  },
};

export default client;
