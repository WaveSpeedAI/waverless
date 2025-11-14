export interface AppInfo {
  name: string;
  namespace?: string;
  type: string;
  status: string;
  replicas?: number;
  readyReplicas?: number;
  availableReplicas?: number;
  image: string;
  labels?: Record<string, string>;
  createdAt: string;
  updatedAt?: string;
  displayName?: string;
  description?: string;
  specName?: string;
  taskTimeout?: number;
  env?: Record<string, string>;
  minReplicas?: number;
  maxReplicas?: number;
  scaleUpThreshold?: number;
  scaleDownIdleTime?: number;
  scaleUpCooldown?: number;
  scaleDownCooldown?: number;
  priority?: number;
  enableDynamicPrio?: boolean;
  highLoadThreshold?: number;
  priorityBoost?: number;
  autoscalerEnabled?: string; // Three-state: undefined/"" = default, "disabled" = force off, "enabled" = force on
  pendingTasks?: number;
  runningTasks?: number;
  workerCount?: number;
  activeWorkerCount?: number;
  totalTasks?: number;
  completedTasks?: number;
  failedTasks?: number;
  lastScaleTime?: string;
  lastTaskTime?: string;
  firstPendingTime?: string;
}

export interface SpecInfo {
  name: string;
  displayName: string;
  category: string;
  resources: ResourceRequirements;
  platforms: Record<string, PlatformConfig>;
}

export interface ResourceRequirements {
  gpu?: string;
  gpuType?: string;
  cpu: string;
  memory: string;
  disk?: string;
  ephemeralStorage?: string;
}

export interface PlatformConfig {
  nodeSelector?: Record<string, string>;
  tolerations?: Toleration[];
  labels?: Record<string, string>;
  annotations?: Record<string, string>;
}

export interface Toleration {
  key: string;
  operator: string;
  value?: string;
  effect: string;
}

export interface DeployRequest {
  endpoint: string;
  specName: string;
  image: string;
  replicas: number;
  taskTimeout?: number;
  // Auto-scaling configuration (optional)
  minReplicas?: number;
  maxReplicas?: number;
  scaleUpThreshold?: number;
  scaleDownIdleTime?: number;
  scaleUpCooldown?: number;
  scaleDownCooldown?: number;
  priority?: number;
  enableDynamicPrio?: boolean;
  highLoadThreshold?: number;
  priorityBoost?: number;
}

export interface UpdateDeploymentRequest {
  endpoint: string;
  specName?: string;
  image?: string;
  replicas?: number;
  taskTimeout?: number;
}

export interface UpdateEndpointConfigRequest {
  // Basic metadata
  displayName?: string;
  description?: string;
  taskTimeout?: number;

  // Autoscaling configuration
  minReplicas?: number;
  maxReplicas?: number;
  priority?: number;
  scaleUpThreshold?: number;
  scaleDownIdleTime?: number;
  scaleUpCooldown?: number;
  scaleDownCooldown?: number;
  enableDynamicPrio?: boolean;
  highLoadThreshold?: number;
  priorityBoost?: number;
  autoscalerEnabled?: string; // "" = default, "disabled" = off, "enabled" = on
}

export interface DeployResponse {
  endpoint: string;
  message: string;
  createdAt?: string;
}

export interface Task {
  id: string;
  endpoint?: string;
  status: 'PENDING' | 'IN_PROGRESS' | 'COMPLETED' | 'FAILED' | 'CANCELLED';
  workerId?: string;
  delayTime?: number;
  executionTime?: number;
  createdAt?: string;
  input?: Record<string, any>;
  output?: Record<string, any>;
  error?: string;
}

export interface TaskListParams {
  endpoint?: string;
  status?: string;
  task_id?: string;
  limit?: number;
  offset?: number;
}

export interface TaskListResponse {
  tasks: Task[];
  total: number;
  limit: number;
  offset: number;
}

export interface EndpointStats {
  endpoint: string;
  total: number;
  pending: number;
  inProgress: number;
  completed: number;
  failed: number;
  cancelled: number;
}

export interface Worker {
  id: string;
  endpoint: string;
  status: 'online' | 'offline' | 'busy';
  concurrency: number;
  current_jobs: number;
  jobs_in_progress: string[];
  last_heartbeat: string;
  version?: string;
  registered_at: string;
}

export interface TaskEvent {
  id: number;
  task_id: string;
  endpoint: string;
  event_type: string;
  event_time: string;
  worker_id?: string;
  worker_pod_name?: string;
  from_status?: string;
  to_status?: string;
  error_message?: string;
}

export interface ExecutionRecord {
  worker_id: string;
  start_time: string;
  end_time?: string;
  duration_seconds?: number;
}

export interface TaskEventsResponse {
  task_id: string;
  events: TaskEvent[];
  total: number;
}

export interface TaskTimelineResponse {
  task_id: string;
  timeline: TaskEvent[];
  total: number;
}

export interface TaskExecutionHistoryResponse {
  task_id: string;
  history: ExecutionRecord[];
}

// Worker with Pod information
export interface WorkerWithPodInfo extends Worker {
  pod_name?: string;
  podPhase?: string; // Pending, Running, Succeeded, Failed, Unknown
  podStatus?: string; // Creating, Running, Terminating, Failed, etc.
  podReason?: string;
  podMessage?: string;
  podIP?: string;
  podNodeName?: string;
  podCreatedAt?: string;
  podStartedAt?: string;
  podRestartCount?: number;
  deletionTimestamp?: string; // Set when pod is terminating
}

// Pod Detail (kubectl describe-like)
export interface PodDetail {
  // Basic Info
  name: string;
  phase: string;
  status: string;
  reason?: string;
  message?: string;
  ip?: string;
  nodeName?: string;
  createdAt: string;
  startedAt?: string;
  deletionTimestamp?: string;
  restartCount: number;
  labels?: Record<string, string>;
  workerID?: string;

  // Detailed Info
  namespace: string;
  uid: string;
  annotations?: Record<string, string>;
  containers: ContainerInfo[];
  initContainers?: ContainerInfo[];
  conditions: PodCondition[];
  events: PodEvent[];
  ownerReferences?: OwnerReference[];
  tolerations?: Record<string, string>[];
  affinity?: Record<string, any>;
  volumes?: VolumeInfo[];
}

export interface ContainerInfo {
  name: string;
  image: string;
  state: string; // Waiting, Running, Terminated
  ready: boolean;
  restartCount: number;
  reason?: string;
  message?: string;
  startedAt?: string;
  finishedAt?: string;
  exitCode?: number;
  resources?: Record<string, any>;
  ports?: ContainerPort[];
  env?: EnvVar[];
}

export interface ContainerPort {
  name?: string;
  containerPort: number;
  protocol: string;
}

export interface EnvVar {
  name: string;
  value?: string;
}

export interface PodCondition {
  type: string;
  status: string;
  reason?: string;
  message?: string;
  lastTransitionTime?: string;
}

export interface PodEvent {
  type: string; // Normal, Warning
  reason: string;
  message: string;
  count: number;
  firstSeen: string;
  lastSeen: string;
}

export interface OwnerReference {
  kind: string;
  name: string;
  uid: string;
}

export interface VolumeInfo {
  name: string;
  type: string;
  source?: Record<string, any>;
}
