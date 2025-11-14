package autoscaler

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"waverless/pkg/logger"
)

// DecisionEngine decision engine
type DecisionEngine struct {
	config             *Config
	resourceCalculator *ResourceCalculator
}

// NewDecisionEngine creates decision engine
func NewDecisionEngine(config *Config, resourceCalculator *ResourceCalculator) *DecisionEngine {
	return &DecisionEngine{
		config:             config,
		resourceCalculator: resourceCalculator,
	}
}

// MakeDecisions makes scaling decisions
func (e *DecisionEngine) MakeDecisions(ctx context.Context, endpoints []*EndpointConfig, clusterResources *ClusterResources) ([]*ScaleDecision, error) {
	decisions := make([]*ScaleDecision, 0)

	// Step 1: Identify endpoints that need to scale up
	scaleUpDecisions := e.identifyScaleUp(ctx, endpoints, &clusterResources.Available)

	// Step 2: Identify endpoints that can scale down
	scaleDownDecisions := e.identifyScaleDown(ctx, endpoints)

	// Step 3: Merge decisions
	decisions = append(decisions, scaleUpDecisions...)
	decisions = append(decisions, scaleDownDecisions...)

	// Step 4: If scale-up requests are blocked by insufficient resources, consider preemption
	if len(scaleUpDecisions) > 0 {
		blockedDecisions := filterBlocked(scaleUpDecisions)
		if len(blockedDecisions) > 0 {
			preemptionDecisions := e.considerPreemption(ctx, blockedDecisions, endpoints, &clusterResources.Available)
			decisions = append(decisions, preemptionDecisions...)
		}
	}

	return decisions, nil
}

// identifyScaleUp identifies endpoints that need to scale up
func (e *DecisionEngine) identifyScaleUp(ctx context.Context, endpoints []*EndpointConfig, availableResources *Resources) []*ScaleDecision {
	decisions := make([]*ScaleDecision, 0)

	for _, ep := range endpoints {
		decision := e.shouldScaleUp(ctx, ep, availableResources.Clone())
		if decision != nil {
			decisions = append(decisions, decision)
		}
	}

	// Sort by effective priority (high priority first)
	sort.Slice(decisions, func(i, j int) bool {
		if decisions[i].Priority != decisions[j].Priority {
			return decisions[i].Priority > decisions[j].Priority
		}
		// When priority is the same, sort by queue length
		return decisions[i].QueueLength > decisions[j].QueueLength
	})

	// Fair allocation strategy: ensure each endpoint with tasks gets at least 1 replica
	approvedDecisions := e.fairAllocation(ctx, decisions, availableResources)

	return approvedDecisions
}

// shouldScaleUp determines whether to scale up
func (e *DecisionEngine) shouldScaleUp(ctx context.Context, ep *EndpointConfig, availableResources *Resources) *ScaleDecision {
	// 1. Check basic conditions
	// 🔥 FIX: Use Replicas (desired) instead of ActualReplicas (ready) to avoid duplicate scale-up
	// When pods are starting, ReadyReplicas=0 but Replicas may already be set to N
	currentReplicas := ep.Replicas
	if currentReplicas >= ep.MaxReplicas {
		logger.DebugCtx(ctx, "endpoint %s: skip scale up, already at max replicas (current=%d, max=%d)",
			ep.Name, currentReplicas, ep.MaxReplicas)
		return nil // Already at max replicas
	}

	// 2. Check cooldown time (check first to avoid frequent scaling)
	if !ep.LastScaleTime.IsZero() {
		cooldown := time.Duration(ep.ScaleUpCooldown) * time.Second
		elapsed := time.Since(ep.LastScaleTime)
		if elapsed < cooldown {
			logger.DebugCtx(ctx, "endpoint %s: skip scale up, still in cooldown (elapsed=%.0fs, cooldown=%ds)",
				ep.Name, elapsed.Seconds(), ep.ScaleUpCooldown)
			return nil // Still in cooldown period
		}
	}

	// 3. Calculate total tasks and target replicas
	// Strategy: assume each worker handles 1 concurrent task
	// Consider both running and queued tasks
	totalTasks := ep.PendingTasks + ep.RunningTasks
	targetReplicas := int(math.Ceil(float64(totalTasks)))

	// 🔍 DEBUG: Log detailed scale-up decision calculation
	logger.InfoCtx(ctx, "endpoint %s: scale-up calculation - pending=%d, running=%d, totalTasks=%d, currentReplicas(desired)=%d, actualReplicas(ready)=%d, targetReplicas(calculated)=%d",
		ep.Name, ep.PendingTasks, ep.RunningTasks, totalTasks, currentReplicas, ep.ActualReplicas, targetReplicas)

	// 🔥 CRITICAL FIX: If calculated target replicas <= current replicas, capacity is sufficient, no scale-up needed
	// This avoids repeated scale-up triggers during Pod startup
	if targetReplicas <= currentReplicas {
		logger.DebugCtx(ctx, "endpoint %s: skip scale up, capacity is sufficient (target=%d <= current=%d)",
			ep.Name, targetReplicas, currentReplicas)
		return nil
	}

	// 4. Check if pending tasks reach scale-up threshold (only needed when scaling from 0)
	// If replicas are already running, consider scaling up whenever there are queued tasks
	if currentReplicas == 0 && ep.PendingTasks < int64(ep.ScaleUpThreshold) {
		logger.DebugCtx(ctx, "endpoint %s: skip scale up, pending tasks below threshold for zero replicas (pending=%d, threshold=%d)",
			ep.Name, ep.PendingTasks, ep.ScaleUpThreshold)
		return nil
	}

	// 5. Limit to maxReplicas
	if targetReplicas > ep.MaxReplicas {
		targetReplicas = ep.MaxReplicas
	}

	// 6. Ensure scaling to at least MinReplicas (if configured)
	if currentReplicas < ep.MinReplicas && targetReplicas < ep.MinReplicas {
		targetReplicas = ep.MinReplicas
		logger.InfoCtx(ctx, "endpoint %s: adjusting target to meet MinReplicas requirement (current=%d, min=%d, target=%d)",
			ep.Name, currentReplicas, ep.MinReplicas, targetReplicas)
	}

	scaleAmount := targetReplicas - currentReplicas
	logger.InfoCtx(ctx, "endpoint %s: final scale decision - targetReplicas=%d, scaleAmount=%d",
		ep.Name, targetReplicas, scaleAmount)

	if scaleAmount <= 0 {
		logger.DebugCtx(ctx, "endpoint %s: no scale needed (target=%d <= current=%d)",
			ep.Name, targetReplicas, currentReplicas)
		return nil
	}

	// 4. Calculate required resources
	requiredResources, err := e.resourceCalculator.CalculateEndpointResource(ctx, ep, scaleAmount)
	if err != nil {
		logger.ErrorCtx(ctx, "failed to calculate resources for %s: %v", ep.Name, err)
		return nil
	}

	// 5. Check if resources are sufficient
	effectivePriority := ep.EffectivePriority(e.config.StarvationTime)
	blocked := !availableResources.CanAllocate(requiredResources)

	if blocked {
		logger.InfoCtx(ctx, "endpoint %s: scale up blocked due to insufficient resources (required: GPU=%d, CPU=%.1f, Mem=%.1fGB, available: GPU=%d, CPU=%.1f, Mem=%.1fGB)",
			ep.Name, requiredResources.GPUCount, requiredResources.CPUCores, requiredResources.MemoryGB,
			availableResources.GPUCount, availableResources.CPUCores, availableResources.MemoryGB)
	} else {
		logger.InfoCtx(ctx, "endpoint %s: planning scale up from %d to %d replicas (pending=%d, running=%d, ready=%d, priority=%d)",
			ep.Name, currentReplicas, targetReplicas, ep.PendingTasks, ep.RunningTasks, ep.ActualReplicas, effectivePriority)
	}

	decision := &ScaleDecision{
		Endpoint:         ep.Name,
		CurrentReplicas:  currentReplicas,
		DesiredReplicas:  targetReplicas,
		ScaleAmount:      scaleAmount,
		Priority:         effectivePriority,
		BasePriority:     ep.Priority,
		QueueLength:      ep.PendingTasks,
		Reason:           fmt.Sprintf("queue length %d exceeds threshold %d", ep.PendingTasks, ep.ScaleUpThreshold),
		Approved:         !blocked,
		Blocked:          blocked,
		RequiredResource: *requiredResources,
	}

	if blocked {
		decision.BlockedReason = "insufficient cluster resources"
	}

	return decision
}

// fairAllocation fair allocation strategy
func (e *DecisionEngine) fairAllocation(ctx context.Context, decisions []*ScaleDecision, availableResources *Resources) []*ScaleDecision {
	approved := make([]*ScaleDecision, 0)

	// 🔍 DEBUG: Log the number of decisions entering fair allocation
	logger.InfoCtx(ctx, "fairAllocation: received %d initial scale decisions", len(decisions))
	for i, d := range decisions {
		logger.InfoCtx(ctx, "fairAllocation: decision[%d] - endpoint=%s, current=%d, desired=%d, scaleAmount=%d, queueLength=%d",
			i, d.Endpoint, d.CurrentReplicas, d.DesiredReplicas, d.ScaleAmount, d.QueueLength)
	}

	// Phase 1: Ensure each endpoint with queued tasks can scale up at least 1 replica (minimum guarantee)
	minimalAllocations := make([]*ScaleDecision, 0)
	remainingDecisions := make([]*ScaleDecision, 0)

	for _, decision := range decisions {
		if decision.ScaleAmount > 0 && decision.QueueLength > 0 {
			// Calculate resources needed for 1 replica
			singleReplica, err := e.resourceCalculator.CalculateEndpointResource(ctx, &EndpointConfig{Name: decision.Endpoint}, 1)
			if err != nil {
				logger.ErrorCtx(ctx, "failed to calculate single replica resource: %v", err)
				continue
			}

			if availableResources.CanAllocate(singleReplica) {
				// Allocate 1 replica
				availableResources.Subtract(singleReplica)
				minimalDecision := &ScaleDecision{
					Endpoint:         decision.Endpoint,
					CurrentReplicas:  decision.CurrentReplicas,
					DesiredReplicas:  decision.CurrentReplicas + 1,
					ScaleAmount:      1,
					Priority:         decision.Priority,
					BasePriority:     decision.BasePriority,
					QueueLength:      decision.QueueLength,
					Reason:           "minimal guarantee allocation",
					Approved:         true,
					RequiredResource: *singleReplica,
				}
				minimalAllocations = append(minimalAllocations, minimalDecision)
				logger.InfoCtx(ctx, "fairAllocation: created minimal decision for %s (%d → %d)",
					decision.Endpoint, minimalDecision.CurrentReplicas, minimalDecision.DesiredReplicas)

				// If more replicas are needed, add to remaining decisions list
				if decision.ScaleAmount > 1 {
					remaining := *decision
					remaining.CurrentReplicas++
					remaining.ScaleAmount--
					remaining.DesiredReplicas = remaining.CurrentReplicas + remaining.ScaleAmount
					remainingDecisions = append(remainingDecisions, &remaining)
					logger.InfoCtx(ctx, "fairAllocation: created remaining decision for %s (%d → %d, scaleAmount=%d)",
						decision.Endpoint, remaining.CurrentReplicas, remaining.DesiredReplicas, remaining.ScaleAmount)
				}
			} else {
				// Insufficient resources, mark as blocked
				decision.Approved = false
				decision.Blocked = true
				decision.BlockedReason = "insufficient resources even for minimal guarantee"
				approved = append(approved, decision)
			}
		}
	}

	approved = append(approved, minimalAllocations...)
	logger.InfoCtx(ctx, "fairAllocation: Phase 1 complete - created %d minimal allocations, %d remaining decisions",
		len(minimalAllocations), len(remainingDecisions))

	// Phase 2: Allocate remaining resources by priority
	// Re-sort by priority
	sort.Slice(remainingDecisions, func(i, j int) bool {
		if remainingDecisions[i].Priority != remainingDecisions[j].Priority {
			return remainingDecisions[i].Priority > remainingDecisions[j].Priority
		}
		return remainingDecisions[i].QueueLength > remainingDecisions[j].QueueLength
	})

	for _, decision := range remainingDecisions {
		resources, err := e.resourceCalculator.CalculateEndpointResource(ctx, &EndpointConfig{Name: decision.Endpoint}, decision.ScaleAmount)
		if err != nil {
			continue
		}

		if availableResources.CanAllocate(resources) {
			availableResources.Subtract(resources)
			decision.Approved = true
			decision.Reason = "priority-based allocation"
			approved = append(approved, decision)
		} else {
			decision.Approved = false
			decision.Blocked = true
			decision.BlockedReason = "insufficient resources after minimal guarantees"
			approved = append(approved, decision)
		}
	}

	return approved
}

// identifyScaleDown identifies endpoints that can scale down
func (e *DecisionEngine) identifyScaleDown(ctx context.Context, endpoints []*EndpointConfig) []*ScaleDecision {
	decisions := make([]*ScaleDecision, 0)

	for _, ep := range endpoints {
		decision := e.shouldScaleDown(ctx, ep)
		if decision != nil {
			decisions = append(decisions, decision)
		}
	}

	return decisions
}

// shouldScaleDown determines whether to scale down
func (e *DecisionEngine) shouldScaleDown(ctx context.Context, ep *EndpointConfig) *ScaleDecision {
	// 1. Check if already at minimum replicas
	// 🔥 FIX: Use Replicas (desired) to check min replicas, consistent with scale-up logic
	// If we're already scaling down (Replicas < ActualReplicas), wait for it to complete
	currentReplicas := ep.Replicas
	if currentReplicas <= ep.MinReplicas {
		logger.DebugCtx(ctx, "endpoint %s: skip scale down, already at min replicas (current=%d, min=%d)",
			ep.Name, currentReplicas, ep.MinReplicas)
		return nil
	}

	// If scale-down is in progress (desired < actual), wait for completion
	if currentReplicas < ep.ActualReplicas {
		logger.DebugCtx(ctx, "endpoint %s: skip scale down, scale down in progress (desired=%d < actual=%d)",
			ep.Name, currentReplicas, ep.ActualReplicas)
		return nil
	}

	// 2. Check if there are queued tasks
	if ep.PendingTasks > 0 {
		logger.DebugCtx(ctx, "endpoint %s: skip scale down, has pending tasks (pending=%d)",
			ep.Name, ep.PendingTasks)
		return nil // Has queued tasks, do not scale down
	}

	// 🔥 CRITICAL FIX: Calculate minimum required replicas based on running tasks
	// Example: 5 tasks running, 10 replicas → can scale down to 5-6 (with buffer)
	// But: 5 tasks running, 3 replicas → should NOT scale down (would interrupt tasks)
	minRequiredReplicas := int(ep.RunningTasks)
	if minRequiredReplicas > 0 {
		// Add 1 replica as buffer to handle task completion timing
		minRequiredReplicas += 1
	}

	// If current replicas <= required replicas, don't scale down
	if currentReplicas <= minRequiredReplicas {
		if ep.RunningTasks > 0 {
			logger.DebugCtx(ctx, "endpoint %s: skip scale down, need at least %d replicas for %d running tasks (current=%d)",
				ep.Name, minRequiredReplicas, ep.RunningTasks, currentReplicas)
		}
		return nil
	}

	// 3. Check idle time
	if ep.LastTaskTime.IsZero() {
		// Never processed tasks, can scale down
	} else {
		idleDuration := time.Since(ep.LastTaskTime)
		if idleDuration.Seconds() < float64(ep.ScaleDownIdleTime) {
			return nil // Idle time threshold not reached yet
		}
	}

	// 4. Check cooldown time
	if !ep.LastScaleTime.IsZero() {
		cooldown := time.Duration(ep.ScaleDownCooldown) * time.Second
		elapsed := time.Since(ep.LastScaleTime)
		if elapsed < cooldown {
			logger.DebugCtx(ctx, "endpoint %s: skip scale down, still in cooldown (elapsed=%.0fs, cooldown=%ds)",
				ep.Name, elapsed.Seconds(), ep.ScaleDownCooldown)
			return nil // Still in cooldown period
		}
	}

	// 5. Determine scale-down amount
	var desiredReplicas int
	var idleDuration time.Duration

	// 🔥 FIX: If LastTaskTime is zero, set a safe default value
	if ep.LastTaskTime.IsZero() {
		// Never processed tasks, use current time as baseline
		idleDuration = 0
	} else {
		idleDuration = time.Since(ep.LastTaskTime)
	}

	doubleIdleTime := time.Duration(ep.ScaleDownIdleTime*2) * time.Second

	// Calculate minimum safe replicas (cannot be lower than running tasks + buffer)
	minSafeReplicas := minRequiredReplicas
	if ep.MinReplicas > minSafeReplicas {
		minSafeReplicas = ep.MinReplicas
	}

	if idleDuration > doubleIdleTime {
		// Idle time exceeds 2x threshold, scale down to safe minimum
		desiredReplicas = minSafeReplicas
	} else {
		// 🔥 FIX: Use currentReplicas (desired) instead of ActualReplicas (ready)
		// Gradual scale-down, reduce by 1 replica each time
		desiredReplicas = currentReplicas - 1
		if desiredReplicas < minSafeReplicas {
			desiredReplicas = minSafeReplicas
		}
	}

	// Double check: never scale below running tasks + buffer
	if desiredReplicas < minRequiredReplicas {
		desiredReplicas = minRequiredReplicas
	}

	// 🔥 FIX: Use currentReplicas to calculate scaleAmount, consistent with scale-up logic
	scaleAmount := desiredReplicas - currentReplicas // Negative number indicates scale-down

	// Enhanced logging with task context (use formatted idle time string)
	idleTimeStr := "never processed tasks"
	if !ep.LastTaskTime.IsZero() {
		idleTimeStr = fmt.Sprintf("idle for %.0f seconds", idleDuration.Seconds())
	}

	if ep.RunningTasks > 0 {
		logger.InfoCtx(ctx, "endpoint %s: planning scale down from %d to %d replicas (%s, running tasks=%d, min required=%d, actual=%d)",
			ep.Name, currentReplicas, desiredReplicas, idleTimeStr, ep.RunningTasks, minRequiredReplicas, ep.ActualReplicas)
	} else {
		logger.InfoCtx(ctx, "endpoint %s: planning scale down from %d to %d replicas (%s, no active tasks, actual=%d)",
			ep.Name, currentReplicas, desiredReplicas, idleTimeStr, ep.ActualReplicas)
	}

	return &ScaleDecision{
		Endpoint:        ep.Name,
		CurrentReplicas: currentReplicas,
		DesiredReplicas: desiredReplicas,
		ScaleAmount:     scaleAmount,
		Priority:        ep.Priority,
		BasePriority:    ep.Priority,
		QueueLength:     ep.PendingTasks,
		Reason:          fmt.Sprintf("idle for %.0f seconds", idleDuration.Seconds()),
		Approved:        true,
	}
}

// considerPreemption considers preemptive scheduling
func (e *DecisionEngine) considerPreemption(ctx context.Context, blockedDecisions []*ScaleDecision, allEndpoints []*EndpointConfig, availableResources *Resources) []*ScaleDecision {
	preemptionDecisions := make([]*ScaleDecision, 0)

	// 1. Identify low-priority endpoints that can be preempted
	// Conditions: low priority + no tasks queued + replicas > minReplicas
	// 🔥 FIX: Use Replicas (desired) for consistency
	preemptableCandidates := make([]*EndpointConfig, 0)
	for _, ep := range allEndpoints {
		if ep.Replicas > ep.MinReplicas && ep.PendingTasks == 0 {
			preemptableCandidates = append(preemptableCandidates, ep)
		}
	}

	// 2. Sort by priority (low priority preempted first)
	sort.Slice(preemptableCandidates, func(i, j int) bool {
		effectivePrioI := preemptableCandidates[i].EffectivePriority(e.config.StarvationTime)
		effectivePrioJ := preemptableCandidates[j].EffectivePriority(e.config.StarvationTime)
		return effectivePrioI < effectivePrioJ
	})

	// 3. For each blocked high-priority request, attempt preemption
	for _, blocked := range blockedDecisions {
		if !blocked.Blocked {
			continue
		}

		requiredResources := &blocked.RequiredResource
		preemptedFrom := make([]string, 0)

		// Attempt to preempt resources from low-priority endpoints
		for _, victim := range preemptableCandidates {
			victimPriority := victim.EffectivePriority(e.config.StarvationTime)
			if victimPriority >= blocked.Priority {
				continue // Don't preempt same or higher priority
			}

			// Calculate resources that can be freed from victim (scale down 1 replica)
			freedResources, err := e.resourceCalculator.CalculateEndpointResource(ctx, victim, 1)
			if err != nil {
				continue
			}

			// Free resources
			availableResources.Add(freedResources)
			preemptedFrom = append(preemptedFrom, victim.Name)

			// 🔥 FIX: Create scale-down decision, use Replicas (desired) for consistency
			preemptionDecisions = append(preemptionDecisions, &ScaleDecision{
				Endpoint:        victim.Name,
				CurrentReplicas: victim.Replicas,
				DesiredReplicas: victim.Replicas - 1,
				ScaleAmount:     -1,
				Priority:        victimPriority,
				BasePriority:    victim.Priority,
				QueueLength:     victim.PendingTasks,
				Reason:          fmt.Sprintf("preempted by higher priority endpoint %s", blocked.Endpoint),
				Approved:        true,
			})

			// Update victim's replica count (for subsequent judgment)
			victim.Replicas--

			// Check if sufficient resources are available
			if availableResources.CanAllocate(requiredResources) {
				blocked.Approved = true
				blocked.Blocked = false
				blocked.PreemptedFrom = preemptedFrom
				blocked.Reason = fmt.Sprintf("approved after preemption from [%s]", joinStrings(preemptedFrom))
				break
			}

			// If victim has reached minReplicas, stop preemption
			if victim.Replicas <= victim.MinReplicas {
				continue
			}
		}
	}

	return preemptionDecisions
}

// filterBlocked filters out blocked decisions
func filterBlocked(decisions []*ScaleDecision) []*ScaleDecision {
	blocked := make([]*ScaleDecision, 0)
	for _, d := range decisions {
		if d.Blocked {
			blocked = append(blocked, d)
		}
	}
	return blocked
}

// joinStrings joins string array
func joinStrings(strs []string) string {
	result := ""
	for i, s := range strs {
		if i > 0 {
			result += ", "
		}
		result += s
	}
	return result
}
