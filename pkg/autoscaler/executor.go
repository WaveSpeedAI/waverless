package autoscaler

import (
	"context"
	"fmt"
	"time"

	"waverless/internal/model"
	endpointsvc "waverless/internal/service/endpoint"
	"waverless/pkg/deploy/k8s"
	"waverless/pkg/interfaces"
	"waverless/pkg/logger"
	"waverless/pkg/store/mysql"
	redisstore "waverless/pkg/store/redis"
)

// Executor executor - executes scaling operations
type Executor struct {
	deploymentProvider interfaces.DeploymentProvider
	endpointService    *endpointsvc.Service
	scalingEventRepo   *mysql.ScalingEventRepository
	workerRepo         *redisstore.WorkerRepository // For smart scale-down
	taskRepo           *mysql.TaskRepository        // For checking running tasks in database
	k8sProvider        *k8s.K8sDeploymentProvider   // For pod draining & deletion
}

// NewExecutor creates executor
func NewExecutor(
	deploymentProvider interfaces.DeploymentProvider,
	endpointService *endpointsvc.Service,
	scalingEventRepo *mysql.ScalingEventRepository,
	workerRepo *redisstore.WorkerRepository,
	taskRepo *mysql.TaskRepository,
) *Executor {
	// Try to get K8sDeploymentProvider for pod-level operations
	k8sProvider, _ := deploymentProvider.(*k8s.K8sDeploymentProvider)

	return &Executor{
		deploymentProvider: deploymentProvider,
		endpointService:    endpointService,
		scalingEventRepo:   scalingEventRepo,
		workerRepo:         workerRepo,
		taskRepo:           taskRepo,
		k8sProvider:        k8sProvider,
	}
}

// ExecuteDecisions executes scaling decisions
func (e *Executor) ExecuteDecisions(ctx context.Context, decisions []*ScaleDecision) error {
	for _, decision := range decisions {
		if !decision.Approved {
			// Record blocked events
			event := &mysql.ScalingEvent{
				EventID:      generateEventID(),
				Endpoint:     decision.Endpoint,
				Timestamp:    time.Now(),
				Action:       "blocked",
				FromReplicas: decision.CurrentReplicas,
				ToReplicas:   decision.DesiredReplicas,
				Reason:       decision.BlockedReason,
				QueueLength:  decision.QueueLength,
				Priority:     decision.Priority,
			}
			if err := e.scalingEventRepo.Create(ctx, event); err != nil {
				logger.ErrorCtx(ctx, "failed to save blocked event: %v", err)
			}
			logger.WarnCtx(ctx, "scale decision blocked for %s: %s", decision.Endpoint, decision.BlockedReason)
			continue
		}

		if decision.ScaleAmount > 0 {
			// Scale up
			if err := e.scaleUp(ctx, decision); err != nil {
				logger.ErrorCtx(ctx, "failed to scale up %s: %v", decision.Endpoint, err)
				continue
			}
		} else if decision.ScaleAmount < 0 {
			// Scale down
			if err := e.scaleDown(ctx, decision); err != nil {
				logger.ErrorCtx(ctx, "failed to scale down %s: %v", decision.Endpoint, err)
				continue
			}
		}
	}

	return nil
}

// scaleUp executes scale-up
func (e *Executor) scaleUp(ctx context.Context, decision *ScaleDecision) error {
	logger.InfoCtx(ctx, "scaling up %s from %d to %d replicas (reason: %s)",
		decision.Endpoint, decision.CurrentReplicas, decision.DesiredReplicas, decision.Reason)

	// Update K8s Deployment
	req := &interfaces.UpdateDeploymentRequest{
		Endpoint: decision.Endpoint,
		Replicas: &decision.DesiredReplicas,
	}
	if _, err := e.deploymentProvider.UpdateDeployment(ctx, req); err != nil {
		return fmt.Errorf("failed to update deployment: %w", err)
	}

	// Update metadata
	meta, err := e.endpointService.GetEndpoint(ctx, decision.Endpoint)
	if err != nil {
		logger.WarnCtx(ctx, "failed to get endpoint metadata: %v", err)
	} else {
		meta.Replicas = decision.DesiredReplicas
		meta.LastScaleTime = time.Now()
		meta.UpdatedAt = time.Now()
		if err := e.endpointService.UpdateEndpoint(ctx, meta); err != nil {
			logger.ErrorCtx(ctx, "failed to update endpoint metadata: %v", err)
		}
	}

	// Record event
	action := "scale_up"
	if len(decision.PreemptedFrom) > 0 {
		action = "preempted"
	}
	event := &mysql.ScalingEvent{
		EventID:       generateEventID(),
		Endpoint:      decision.Endpoint,
		Timestamp:     time.Now(),
		Action:        action,
		FromReplicas:  decision.CurrentReplicas,
		ToReplicas:    decision.DesiredReplicas,
		Reason:        decision.Reason,
		QueueLength:   decision.QueueLength,
		Priority:      decision.Priority,
		PreemptedFrom: mysql.JSONStringArray(decision.PreemptedFrom),
	}
	if err := e.scalingEventRepo.Create(ctx, event); err != nil {
		logger.ErrorCtx(ctx, "failed to save scale up event: %v", err)
	}

	return nil
}

// scaleDown executes smart scale-down - only delete idle workers
func (e *Executor) scaleDown(ctx context.Context, decision *ScaleDecision) error {
	logger.InfoCtx(ctx, "smart scale down %s from %d to %d replicas (reason: %s)",
		decision.Endpoint, decision.CurrentReplicas, decision.DesiredReplicas, decision.Reason)

	// Step 1: Get all workers for this endpoint
	allWorkers, err := e.workerRepo.GetAll(ctx)
	if err != nil {
		return fmt.Errorf("failed to get workers: %w", err)
	}

	// Step 2: Filter workers for this endpoint and find idle ones
	var idleWorker *model.Worker
	for _, w := range allWorkers {
		if w.Endpoint == decision.Endpoint && w.CurrentJobs == 0 {
			idleWorker = w
			break
		}
	}

	// Step 3: If no idle worker found, cannot safely scale down
	if idleWorker == nil {
		logger.WarnCtx(ctx, "no idle worker found for %s, skip scale down", decision.Endpoint)
		// Record blocked scale-down event
		event := &mysql.ScalingEvent{
			EventID:      generateEventID(),
			Endpoint:     decision.Endpoint,
			Timestamp:    time.Now(),
			Action:       "scale_down_blocked",
			FromReplicas: decision.CurrentReplicas,
			ToReplicas:   decision.DesiredReplicas,
			Reason:       "No idle worker available",
			QueueLength:  decision.QueueLength,
			Priority:     decision.Priority,
		}
		e.scalingEventRepo.Create(ctx, event)
		return fmt.Errorf("no idle worker available for scale down")
	}

	// Step 4: Target Pod Name = worker ID (from deployment.yaml: RUNPOD_POD_ID = metadata.name)
	targetPodName := idleWorker.ID
	logger.InfoCtx(ctx, "selected idle worker for scale down: %s (endpoint: %s)", targetPodName, decision.Endpoint)

	// Step 5: Mark Pod as draining (prevent pulling new tasks) + set deletion priority
	if e.k8sProvider != nil {
		// 5.1: Mark draining label (business logic: prevent worker from pulling new tasks)
		if err := e.k8sProvider.MarkPodDraining(ctx, targetPodName); err != nil {
			logger.WarnCtx(ctx, "failed to mark pod draining: %v, continue anyway", err)
		} else {
			logger.InfoCtx(ctx, "marked pod as draining: %s", targetPodName)
		}

		// 5.2: Set Pod Deletion Cost (K8s logic: make Deployment controller prioritize deleting this Pod)
		if err := e.k8sProvider.SetPodDeletionCost(ctx, targetPodName, -1000); err != nil {
			logger.WarnCtx(ctx, "failed to set pod deletion cost: %v, continue anyway", err)
		} else {
			logger.InfoCtx(ctx, "set pod deletion cost to -1000: %s", targetPodName)
		}
	}

	// Step 6: Start background task: wait for Pod to be completely idle then update Deployment
	go e.gracefulScaleDown(context.Background(), decision, targetPodName)

	logger.InfoCtx(ctx, "initiated graceful scale down for %s, target pod: %s", decision.Endpoint, targetPodName)
	return nil
}

// gracefulScaleDown graceful scale-down - confirm Worker is truly idle before notifying K8s to scale down
func (e *Executor) gracefulScaleDown(ctx context.Context, decision *ScaleDecision, podName string) {
	// 🔥 Improvement: Only wait a short time (30 seconds), because we already know worker.CurrentJobs == 0
	// This is just to confirm no new tasks are assigned, then update Deployment to let K8s delete the Pod
	maxWait := 30 * time.Second
	ticker := time.NewTicker(2 * time.Second) // Check every 2 seconds
	defer ticker.Stop()

	timeout := time.After(maxWait)

	for {
		select {
		case <-timeout:
			logger.WarnCtx(ctx, "drain confirmation timeout, updating deployment for scale down: %s", podName)
			e.executeScaleDownConfirmed(ctx, decision, podName)
			return

		case <-ticker.C:
			// Check if Worker is still idle (podName = worker_id)
			worker, err := e.workerRepo.Get(ctx, podName)
			if err != nil || worker == nil {
				// Worker no longer exists, may have been deleted
				logger.InfoCtx(ctx, "worker no longer exists: %s, scale down aborted", podName)
				return
			}

			// 🔥 CRITICAL: Confirm no running tasks through database
			hasRunningTasks, err := e.hasRunningTasks(ctx, podName)
			if err != nil {
				logger.WarnCtx(ctx, "failed to check running tasks for %s: %v, will retry", podName, err)
				continue
			}

			if hasRunningTasks || worker.CurrentJobs > 0 {
				// Worker has running tasks, cannot scale down
				logger.WarnCtx(ctx, "worker has running tasks, aborting scale down: %s (currentJobs=%d, hasRunningTasks=%v)",
					podName, worker.CurrentJobs, hasRunningTasks)
				// 🔥 Important: Don't scale down Worker with tasks, restore Pod Deletion Cost
				e.revertScaleDown(ctx, decision, podName)
				return
			}

			// ✅ Worker confirmed idle (double-checked via Redis and database), update Deployment to let K8s delete Pod
			logger.InfoCtx(ctx, "worker confirmed idle (redis + database), updating deployment: %s", podName)
			e.executeScaleDownConfirmed(ctx, decision, podName)
			return
		}
	}
}

// executeScaleDown executes actual scale-down operation (reduce replicas first, then delete Pod immediately)
func (e *Executor) executeScaleDown(ctx context.Context, decision *ScaleDecision, podName string) {
	// Step 1: Reduce Deployment replicas first (critical: must be before deleting Pod)
	req := &interfaces.UpdateDeploymentRequest{
		Endpoint: decision.Endpoint,
		Replicas: &decision.DesiredReplicas,
	}

	if _, err := e.deploymentProvider.UpdateDeployment(ctx, req); err != nil {
		logger.ErrorCtx(ctx, "failed to update deployment replicas: %v", err)
		return
	}

	logger.InfoCtx(ctx, "updated deployment replicas: %s, %d -> %d",
		decision.Endpoint, decision.CurrentReplicas, decision.DesiredReplicas)

	// Step 2: Delete target Pod immediately (prevent Deployment controller from recreating it)
	if e.k8sProvider != nil {
		if err := e.k8sProvider.DeletePod(ctx, podName); err != nil {
			logger.ErrorCtx(ctx, "failed to delete pod: %v", err)
		} else {
			logger.InfoCtx(ctx, "deleted pod: %s", podName)
		}
	}

	// Step 3: Update metadata
	meta, err := e.endpointService.GetEndpoint(ctx, decision.Endpoint)
	if err != nil {
		logger.WarnCtx(ctx, "failed to get endpoint metadata: %v", err)
	} else {
		meta.Replicas = decision.DesiredReplicas
		meta.LastScaleTime = time.Now()
		meta.UpdatedAt = time.Now()
		if err := e.endpointService.UpdateEndpoint(ctx, meta); err != nil {
			logger.ErrorCtx(ctx, "failed to update endpoint metadata: %v", err)
		}
	}

	// Step 4: Record scale-down event
	event := &mysql.ScalingEvent{
		EventID:      generateEventID(),
		Endpoint:     decision.Endpoint,
		Timestamp:    time.Now(),
		Action:       "scale_down",
		FromReplicas: decision.CurrentReplicas,
		ToReplicas:   decision.DesiredReplicas,
		Reason:       fmt.Sprintf("%s (pod: %s)", decision.Reason, podName),
		QueueLength:  decision.QueueLength,
		Priority:     decision.Priority,
	}

	if err := e.scalingEventRepo.Create(ctx, event); err != nil {
		logger.ErrorCtx(ctx, "failed to save scale down event: %v", err)
	}

	logger.InfoCtx(ctx, "scale down completed: %s, pod: %s", decision.Endpoint, podName)
}

// forceScaleDown force scale-down (after timeout)
func (e *Executor) forceScaleDown(ctx context.Context, decision *ScaleDecision, podName string) {
	logger.WarnCtx(ctx, "force scale down after timeout: %s, pod: %s", decision.Endpoint, podName)
	e.executeScaleDown(ctx, decision, podName)
}

// hasRunningTasks checks if worker has running tasks through database
// This is the second layer of the double-check mechanism (Redis + Database)
func (e *Executor) hasRunningTasks(ctx context.Context, workerID string) (bool, error) {
	// Query database for IN_PROGRESS tasks for this worker
	tasks, err := e.taskRepo.GetTasksByWorker(ctx, workerID)
	if err != nil {
		return false, fmt.Errorf("failed to check running tasks for worker %s: %w", workerID, err)
	}

	logger.DebugCtx(ctx, "worker %s has %d running tasks in database", workerID, len(tasks))
	return len(tasks) > 0, nil
}

// executeScaleDownConfirmed executes confirmed fast scale-down (worker idle confirmed)
// Strategy: Mark Pod as low-priority deletion candidate, then only update Deployment replicas, let K8s automatically delete marked Pod
func (e *Executor) executeScaleDownConfirmed(ctx context.Context, decision *ScaleDecision, podName string) {
	logger.InfoCtx(ctx, "executing confirmed scale down for %s, pod: %s", decision.Endpoint, podName)

	// Step 1: Set Pod Deletion Cost, make K8s prioritize deleting this Pod
	// Use -1000 to ensure this Pod has lowest priority (deleted first)
	if e.k8sProvider != nil {
		if err := e.k8sProvider.SetPodDeletionCost(ctx, podName, -1000); err != nil {
			logger.WarnCtx(ctx, "failed to set pod deletion cost for %s: %v", podName, err)
			// Not a fatal error, continue execution
		} else {
			logger.InfoCtx(ctx, "set pod deletion cost to -1000 for %s (K8s will prioritize deletion)", podName)
		}
	}

	// Step 2: Update Deployment replicas, let K8s Deployment controller automatically select and delete marked Pod
	req := &interfaces.UpdateDeploymentRequest{
		Endpoint: decision.Endpoint,
		Replicas: &decision.DesiredReplicas,
	}

	if _, err := e.deploymentProvider.UpdateDeployment(ctx, req); err != nil {
		logger.ErrorCtx(ctx, "failed to update deployment replicas: %v", err)
		return
	}

	logger.InfoCtx(ctx, "updated deployment replicas: %s, %d -> %d, K8s will delete pod %s",
		decision.Endpoint, decision.CurrentReplicas, decision.DesiredReplicas, podName)

	// Step 3: Update metadata
	meta, err := e.endpointService.GetEndpoint(ctx, decision.Endpoint)
	if err != nil {
		logger.WarnCtx(ctx, "failed to get endpoint metadata: %v", err)
	} else {
		meta.Replicas = decision.DesiredReplicas
		meta.LastScaleTime = time.Now()
		meta.UpdatedAt = time.Now()
		if err := e.endpointService.UpdateEndpoint(ctx, meta); err != nil {
			logger.ErrorCtx(ctx, "failed to update endpoint metadata: %v", err)
		}
	}

	// Step 4: Record scale-down event
	event := &mysql.ScalingEvent{
		EventID:      generateEventID(),
		Endpoint:     decision.Endpoint,
		Timestamp:    time.Now(),
		Action:       "scale_down",
		FromReplicas: decision.CurrentReplicas,
		ToReplicas:   decision.DesiredReplicas,
		Reason:       fmt.Sprintf("%s (pod: %s, confirmed idle, K8s-managed deletion)", decision.Reason, podName),
		QueueLength:  decision.QueueLength,
		Priority:     decision.Priority,
	}

	if err := e.scalingEventRepo.Create(ctx, event); err != nil {
		logger.ErrorCtx(ctx, "failed to save scale down event: %v", err)
	}

	logger.InfoCtx(ctx, "scale down completed: %s, pod %s marked for K8s deletion", decision.Endpoint, podName)
}

// revertScaleDown reverts scale-down (if worker has tasks detected)
// Needs podName parameter to clear the Pod's deletion cost
func (e *Executor) revertScaleDown(ctx context.Context, decision *ScaleDecision, podName string) {
	logger.WarnCtx(ctx, "reverting scale down for %s, worker %s has running tasks",
		decision.Endpoint, podName)

	// 🔥 CRITICAL: Must restore Pod Deletion Cost to 0
	// Reason: If not restored, this Pod will keep deletion cost = -1000
	// Next scale-down, if another worker is selected, K8s might mistakenly delete this Pod with tasks!
	if e.k8sProvider != nil {
		if err := e.k8sProvider.SetPodDeletionCost(ctx, podName, 0); err != nil {
			logger.ErrorCtx(ctx, "CRITICAL: failed to reset pod deletion cost for %s: %v", podName, err)
			// This is a serious error, but we cannot block the process
		} else {
			logger.InfoCtx(ctx, "reset pod deletion cost to 0 for %s (restored to normal priority)", podName)
		}
	}

	// Record rollback event (no actual Deployment update)
	event := &mysql.ScalingEvent{
		EventID:      generateEventID(),
		Endpoint:     decision.Endpoint,
		Timestamp:    time.Now(),
		Action:       "scale_down_aborted",
		FromReplicas: decision.CurrentReplicas,
		ToReplicas:   decision.CurrentReplicas, // Keep unchanged
		Reason:       fmt.Sprintf("Worker %s has running tasks detected during drain verification", podName),
		QueueLength:  decision.QueueLength,
		Priority:     decision.Priority,
	}

	if err := e.scalingEventRepo.Create(ctx, event); err != nil {
		logger.ErrorCtx(ctx, "failed to save abort event: %v", err)
	}

	logger.InfoCtx(ctx, "scale down aborted: %s, worker %s has active tasks",
		decision.Endpoint, podName)
}

// generateEventID generates event ID
func generateEventID() string {
	return fmt.Sprintf("evt_%d", time.Now().UnixNano())
}
