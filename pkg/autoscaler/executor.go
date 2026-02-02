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
)

// Executor executor - executes scaling operations
type Executor struct {
	deploymentProvider interfaces.DeploymentProvider
	endpointService    *endpointsvc.Service
	scalingEventRepo   *mysql.ScalingEventRepository
	workerLister       interfaces.WorkerLister    // For smart scale-down
	taskRepo           *mysql.TaskRepository      // For checking running tasks in database
	k8sProvider        *k8s.K8sDeploymentProvider // For pod draining & deletion
	endpointRepo       *mysql.EndpointRepository  // For checking endpoint health status
}

// NewExecutor creates executor
func NewExecutor(
	deploymentProvider interfaces.DeploymentProvider,
	endpointService *endpointsvc.Service,
	scalingEventRepo *mysql.ScalingEventRepository,
	workerLister interfaces.WorkerLister,
	taskRepo *mysql.TaskRepository,
	endpointRepo *mysql.EndpointRepository,
) *Executor {
	// Try to get K8sDeploymentProvider for pod-level operations
	k8sProvider, _ := deploymentProvider.(*k8s.K8sDeploymentProvider)

	return &Executor{
		deploymentProvider: deploymentProvider,
		endpointService:    endpointService,
		scalingEventRepo:   scalingEventRepo,
		workerLister:       workerLister,
		taskRepo:           taskRepo,
		k8sProvider:        k8sProvider,
		endpointRepo:       endpointRepo,
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
	// Check if endpoint is blocked due to image failure (Property 8: Failed Endpoint Prevents New Pods)
	// Validates: Requirements 5.5
	if e.endpointRepo != nil {
		blocked, reason, err := e.endpointRepo.IsBlockedDueToImageFailure(ctx, decision.Endpoint)
		if err != nil {
			logger.WarnCtx(ctx, "failed to check endpoint health status: %v", err)
			// Continue with scale-up if check fails (fail-open for availability)
		} else if blocked {
			logger.WarnCtx(ctx, "scale up blocked for %s: endpoint is UNHEALTHY due to image issues - %s",
				decision.Endpoint, reason)

			// Record blocked event
			event := &mysql.ScalingEvent{
				EventID:      generateEventID(),
				Endpoint:     decision.Endpoint,
				Timestamp:    time.Now(),
				Action:       "scale_up_blocked_image_failure",
				FromReplicas: decision.CurrentReplicas,
				ToReplicas:   decision.DesiredReplicas,
				Reason:       fmt.Sprintf("Endpoint is UNHEALTHY due to image issues: %s. Please update the image configuration.", reason),
				QueueLength:  decision.QueueLength,
				Priority:     decision.Priority,
			}
			if err := e.scalingEventRepo.Create(ctx, event); err != nil {
				logger.ErrorCtx(ctx, "failed to save blocked event: %v", err)
			}

			return fmt.Errorf("scale up blocked: endpoint %s is UNHEALTHY due to image issues, please update the image configuration", decision.Endpoint)
		}
	}

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
		// Fix status if it was incorrectly set to Stopped by K8s informer race condition
		// When scaling up, status should be Pending (waiting for pods to be ready)
		if meta.Status == "Stopped" && decision.DesiredReplicas > 0 {
			logger.InfoCtx(ctx, "fixing status for %s: Stopped -> Pending (scaling up to %d replicas)",
				decision.Endpoint, decision.DesiredReplicas)
			meta.Status = "Pending"
		}
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

	// Step 1: Get workers for this endpoint only (optimized query)
	endpointWorkers, err := e.workerLister.ListWorkers(ctx, decision.Endpoint)
	if err != nil {
		return fmt.Errorf("failed to get workers: %w", err)
	}

	// Step 2: Find idle workers
	// Priority: select the worker with the longest idle time (earliest LastTaskTime)
	var idleWorker *model.Worker
	var oldestIdleTime time.Time

	for _, w := range endpointWorkers {
		if w.CurrentJobs == 0 {
			// If this is the first idle worker, or has been idle longer
			if idleWorker == nil {
				idleWorker = w
				oldestIdleTime = w.LastTaskTime
			} else if !w.LastTaskTime.IsZero() {
				// Select worker with earliest LastTaskTime (longest idle)
				if oldestIdleTime.IsZero() || w.LastTaskTime.Before(oldestIdleTime) {
					idleWorker = w
					oldestIdleTime = w.LastTaskTime
				}
			}
		}
	}

	// Step 3: If no idle worker found, cannot safely scale down
	if idleWorker == nil {
		logger.WarnCtx(ctx, "no idle worker found for %s, skip scale down", decision.Endpoint)

		// 🔧 Check if this is an orphaned endpoint (no deployment exists)
		// If deployment doesn't exist and no idle workers, fix the database state
		if e.isOrphanedEndpoint(ctx, decision) {
			logger.WarnCtx(ctx, "🔧 detected orphaned endpoint %s: no deployment and no idle workers, fixing status", decision.Endpoint)
			e.fixOrphanedEndpoint(ctx, decision)
			return nil // Successfully fixed, no error
		}

		// Only record blocked event once per 5 minutes to avoid noise
		recent, _ := e.scalingEventRepo.GetLatestByEndpoint(ctx, decision.Endpoint)
		if recent == nil || recent.Action != "scale_down_blocked" || time.Since(recent.Timestamp) > 5*time.Minute {
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
		}
		return fmt.Errorf("no idle worker available for scale down")
	}

	// Step 4: Target Pod Name = worker ID (from deployment.yaml: RUNPOD_POD_ID = metadata.name)
	targetPodName := idleWorker.ID

	// Log idle duration for visibility
	var idleDurationMsg string
	if !idleWorker.LastTaskTime.IsZero() {
		idleDuration := time.Since(idleWorker.LastTaskTime)
		idleDurationMsg = fmt.Sprintf(", idle for %.0f seconds", idleDuration.Seconds())
	} else {
		idleDurationMsg = ", never processed tasks"
	}

	logger.InfoCtx(ctx, "selected idle worker for scale down: %s (endpoint: %s%s)",
		targetPodName, decision.Endpoint, idleDurationMsg)

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
			worker, err := e.workerLister.GetWorker(ctx, podName)
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

// isOrphanedEndpoint checks if an endpoint is orphaned (no deployment exists)
// An orphaned endpoint has replicas > 0 in database but no actual deployment
//
// IMPORTANT: This check is ONLY for K8s provider, not for Novita or other providers
// because different providers have different behaviors for GetApp when endpoint is being created
//
// Conditions to be considered orphaned (ALL must be true):
// 1. Using K8s provider (not Novita)
// 2. Deployment doesn't exist in K8s
// 3. Endpoint was created more than 10 minutes ago (not a new deployment)
// 4. Has had worker records before (was deployed successfully at some point)
func (e *Executor) isOrphanedEndpoint(ctx context.Context, decision *ScaleDecision) bool {
	// Only apply this logic for K8s provider
	// Novita and other providers have different behaviors
	if e.k8sProvider == nil {
		logger.DebugCtx(ctx, "endpoint %s: skipping orphan check (not K8s provider)", decision.Endpoint)
		return false
	}

	// Check if deployment exists in K8s
	_, err := e.deploymentProvider.GetApp(ctx, decision.Endpoint)
	if err == nil {
		// Deployment exists, not orphaned
		return false
	}

	// Deployment doesn't exist, but we need more checks to confirm it's truly orphaned
	// Get endpoint metadata to check creation time
	meta, err := e.endpointService.GetEndpoint(ctx, decision.Endpoint)
	if err != nil {
		logger.WarnCtx(ctx, "endpoint %s: failed to get metadata for orphan check: %v", decision.Endpoint, err)
		return false
	}

	// Check 1: Endpoint must be created more than 10 minutes ago
	// This prevents marking newly created endpoints as orphaned
	endpointAge := time.Since(meta.CreatedAt)
	if endpointAge < 10*time.Minute {
		logger.DebugCtx(ctx, "endpoint %s: skipping orphan check (created only %.1f minutes ago)",
			decision.Endpoint, endpointAge.Minutes())
		return false
	}

	// Check 2: Must have had worker records before (indicates it was deployed at some point)
	// If no workers ever existed, it might be a deployment that never succeeded
	workers, err := e.workerLister.ListWorkers(ctx, decision.Endpoint)
	if err != nil {
		logger.WarnCtx(ctx, "endpoint %s: failed to list workers for orphan check: %v", decision.Endpoint, err)
		return false
	}

	if len(workers) == 0 {
		// No worker records at all - this could be a deployment that's still initializing
		// or one that failed before any worker registered
		// Be conservative and don't mark as orphaned
		logger.DebugCtx(ctx, "endpoint %s: skipping orphan check (no worker records found)", decision.Endpoint)
		return false
	}

	// All checks passed - this is truly an orphaned endpoint:
	// - K8s provider
	// - No deployment exists
	// - Created more than 10 minutes ago
	// - Had workers before (so it was deployed successfully at some point)
	logger.InfoCtx(ctx, "endpoint %s: confirmed orphaned (no deployment, age=%.1f min, had %d workers)",
		decision.Endpoint, endpointAge.Minutes(), len(workers))
	return true
}

// fixOrphanedEndpoint fixes an orphaned endpoint by updating database state to Stopped
func (e *Executor) fixOrphanedEndpoint(ctx context.Context, decision *ScaleDecision) {
	// Update endpoint metadata: set replicas to 0 and status to Stopped
	meta, err := e.endpointService.GetEndpoint(ctx, decision.Endpoint)
	if err != nil {
		logger.ErrorCtx(ctx, "failed to get endpoint metadata for orphan fix: %v", err)
		return
	}

	oldReplicas := meta.Replicas
	meta.Replicas = 0
	meta.Status = "Stopped"
	meta.UpdatedAt = time.Now()

	if err := e.endpointService.UpdateEndpoint(ctx, meta); err != nil {
		logger.ErrorCtx(ctx, "failed to fix orphaned endpoint %s: %v", decision.Endpoint, err)
		return
	}

	// Record the fix event
	event := &mysql.ScalingEvent{
		EventID:      generateEventID(),
		Endpoint:     decision.Endpoint,
		Timestamp:    time.Now(),
		Action:       "orphan_fixed",
		FromReplicas: oldReplicas,
		ToReplicas:   0,
		Reason:       "Auto-fixed orphaned endpoint: deployment not found in K8s, no active workers",
		QueueLength:  decision.QueueLength,
		Priority:     decision.Priority,
	}
	if err := e.scalingEventRepo.Create(ctx, event); err != nil {
		logger.WarnCtx(ctx, "failed to record orphan fix event: %v", err)
	}

	logger.InfoCtx(ctx, "✅ fixed orphaned endpoint %s: status changed to Stopped, replicas %d -> 0", decision.Endpoint, oldReplicas)
}
