export interface AppInfo {
  name: string;
  namespace?: string;
  type: string;
  status: string;
  replicas?: number;
  gpuCount?: number; // GPU count per replica
  readyReplicas?: number;
  availableReplicas?: number;
  image: string;
  imagePrefix?: string; // Image prefix for matching updates
  imageDigest?: string; // Current image digest from DockerHub
  imageLastChecked?: string; // Last time image was checked for updates
  latestImage?: string; // Latest available image (if update is available)
  imageUpdateAvailable?: boolean; // Computed: true if latestImage exists and differs from image
  labels?: Record<string, string>;
  createdAt: string;
  updatedAt?: string;
  displayName?: string;
  description?: string;
  specName?: string;
  taskTimeout?: number;
  maxPendingTasks?: number; // Maximum allowed pending tasks before warning clients
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
  shmSize?: string; // Shared memory size from deployment
  volumeMounts?: VolumeMount[]; // PVC volume mounts from deployment
  enablePtrace?: boolean; // Enable SYS_PTRACE capability for debugging
  // Health status fields
  healthStatus?: string; // HEALTHY, DEGRADED, UNHEALTHY
  healthMessage?: string; // User-friendly health message
}

export interface SpecInfo {
  name: string;
  displayName: string;
  category: string;
  resourceType?: string; // fixed, serverless
  resources: ResourceRequirements;
  platforms: Record<string, PlatformConfig>;
  // Capacity info
  capacity?: 'available' | 'limited' | 'sold_out';
  spotScore?: number;
  spotPrice?: number;
  runningCount?: number;
  pendingCount?: number;
}

export interface ResourceRequirements {
  gpu?: string;
  gpuType?: string;
  cpu: string;
  memory: string;
  ephemeralStorage?: string;
  shmSize?: string; // Shared memory size (e.g., "1Gi", "512Mi")
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

export interface VolumeMount {
  pvcName: string;
  mountPath: string;
}

export interface PVCInfo {
  name: string;
  namespace: string;
  status: string;
  volume: string;
  capacity: string;
  accessModes: string;
  storageClass: string;
  createdAt: string;
}

export interface DeployRequest {
  endpoint: string;
  specName: string;
  image: string;
  imagePrefix?: string; // Image prefix for matching updates (e.g., "wavespeed/model-deploy:wan_i2v-default-")
  replicas: number;
  gpuCount?: number; // GPU count per replica (1-N, resources = per-gpu-config * gpuCount)
  taskTimeout?: number;
  maxPendingTasks?: number; // Maximum allowed pending tasks before warning clients
  env?: Record<string, string>; // Custom environment variables
  volumeMounts?: VolumeMount[];
  shmSize?: string; // Shared memory size (e.g., "1Gi", "512Mi")
  enablePtrace?: boolean; // Enable SYS_PTRACE capability (only for fixed resource pools)
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
  env?: Record<string, string>; // Custom environment variables
  volumeMounts?: VolumeMount[];
  shmSize?: string; // Shared memory size (e.g., "1Gi", "512Mi")
  enablePtrace?: boolean; // Enable SYS_PTRACE capability (only for fixed resource pools)
}

export interface UpdateEndpointConfigRequest {
  // Basic metadata
  displayName?: string;
  description?: string;
  taskTimeout?: number;
  maxPendingTasks?: number; // Maximum allowed pending tasks before warning clients
  imagePrefix?: string; // Image prefix for matching updates

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
  worker_id?: string;
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
  last_task_time?: string; // Last time a task was completed (for idle tracking)
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
  // Failure information (camelCase to match backend JSON)
  failureType?: string; // IMAGE_PULL_FAILED, CONTAINER_CRASH, RESOURCE_LIMIT, etc.
  failureReason?: string; // User-friendly failure message
  failureSuggestion?: string; // Suggested action to fix the issue
  failureOccurredAt?: string; // When the failure occurred
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


// ============================================
// Status Tracking Types (Endpoint Status Tracking Feature)
// ============================================

// Pending phase types for worker status tracking
export type PendingPhase = 'SCHEDULING' | 'WAITING_NODE' | 'PULLING_IMAGE' | 'INITIALIZING';

// Spot capacity status
export type SpotCapacity = 'AVAILABLE' | 'LIMITED' | 'CONSTRAINED';

// Spot status information for AWS Spot capacity
export interface SpotStatus {
  capacity: SpotCapacity;
  score: number; // 1-10
  price: number; // USD/hour
  instanceType: string;
}

// Worker pending detail for status summary
export interface WorkerPendingDetail {
  workerId: string;
  podName: string;
  phase: PendingPhase;
  reason: string;
  message: string;
  since: string; // ISO timestamp
}

// Worker failure detail for status summary
export interface WorkerFailureDetail {
  workerId: string;
  podName: string;
  failureType: string;
  reason: string;
  suggestion: string;
  occurredAt: string; // ISO timestamp
}

// Endpoint status summary aggregating worker statuses
export interface EndpointStatusSummary {
  totalWorkers: number;
  workersByStatus: Record<string, number>; // e.g., { ONLINE: 2, PENDING: 1 }
  workersByPhase: Record<string, number>; // e.g., { WAITING_NODE: 1, PULLING_IMAGE: 0 }
  pendingDetails?: WorkerPendingDetail[];
  failureDetails?: WorkerFailureDetail[];
  spotCapacity?: SpotStatus;
  lastUpdated: string; // ISO timestamp
}

// Status event types
export type StatusEventType = 'STATUS_CHANGE' | 'PHASE_CHANGE' | 'FAILURE' | 'RECOVERY';

// Status event for timeline display
export interface StatusEvent {
  id: number;
  workerId: string;
  endpoint: string;
  eventType: StatusEventType;
  oldStatus?: string;
  newStatus: string;
  phase?: string;
  reason?: string;
  message?: string;
  spotStatus?: SpotStatus;
  createdAt: string; // ISO timestamp
}

// Status event filter for API queries
export interface StatusEventFilter {
  endpoint?: string;
  workerId?: string;
  eventType?: StatusEventType;
  startTime?: string; // ISO timestamp
  endTime?: string; // ISO timestamp
  limit?: number;
  offset?: number;
}

// Extended WorkerWithPodInfo with pending phase information
export interface WorkerWithPendingInfo extends WorkerWithPodInfo {
  pendingPhase?: PendingPhase;
  pendingPhaseSince?: string; // ISO timestamp
  pendingReason?: string;
  pendingMessage?: string;
  spotStatus?: SpotStatus; // Only for WAITING_NODE phase
}

// Extended AppInfo with status summary
export interface AppInfoWithStatusSummary extends AppInfo {
  statusSummary?: EndpointStatusSummary;
}
