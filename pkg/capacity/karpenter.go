package capacity

import (
	"context"
	"strings"
	"sync"
	"time"

	"waverless/pkg/interfaces"
	"waverless/pkg/logger"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"
)

var nodeClaimGVR = schema.GroupVersionResource{
	Group:    "karpenter.sh",
	Version:  "v1",
	Resource: "nodeclaims",
}

// KarpenterProvider watches NodeClaim + active polling for capacity awareness
type KarpenterProvider struct {
	client         dynamic.Interface
	nodePoolToSpec map[string]string // nodepool name -> spec name
	pollInterval   time.Duration

	// Cache recent failure status
	failureCache   map[string]time.Time // spec -> last failure time
	failureCacheMu sync.RWMutex
}

func NewKarpenterProvider(client dynamic.Interface, nodePoolToSpec map[string]string) *KarpenterProvider {
	return &KarpenterProvider{
		client:         client,
		nodePoolToSpec: nodePoolToSpec,
		pollInterval:   2 * time.Minute,
		failureCache:   make(map[string]time.Time),
	}
}

func (p *KarpenterProvider) SupportsWatch() bool { return true }

func (p *KarpenterProvider) Watch(ctx context.Context, callback func(interfaces.CapacityEvent)) error {
	factory := dynamicinformer.NewDynamicSharedInformerFactory(p.client, time.Minute*10)
	informer := factory.ForResource(nodeClaimGVR).Informer()

	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			p.handleNodeClaim(ctx, obj.(*unstructured.Unstructured), callback)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			p.handleNodeClaim(ctx, newObj.(*unstructured.Unstructured), callback)
		},
		DeleteFunc: func(obj interface{}) {
			p.handleNodeClaimDelete(ctx, obj, callback)
		},
	})

	// Start informer
	factory.Start(ctx.Done())
	factory.WaitForCacheSync(ctx.Done())

	// Also start active polling
	go p.startPolling(ctx, callback)

	<-ctx.Done()
	return nil
}

// startPolling actively polls all NodeClaim status
func (p *KarpenterProvider) startPolling(ctx context.Context, callback func(interfaces.CapacityEvent)) {
	ticker := time.NewTicker(p.pollInterval)
	defer ticker.Stop()

	// Execute immediately on first run
	p.pollAllNodeClaims(ctx, callback)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.pollAllNodeClaims(ctx, callback)
		}
	}
}

// pollAllNodeClaims polls all NodeClaim status
func (p *KarpenterProvider) pollAllNodeClaims(ctx context.Context, callback func(interfaces.CapacityEvent)) {
	events, err := p.CheckAll(ctx)
	if err != nil {
		logger.WarnCtx(ctx, "Failed to poll NodeClaims: %v", err)
		return
	}

	for _, event := range events {
		callback(event)
	}

	// Check specs that haven't succeeded for a long time, may need recovery
	p.checkRecovery(ctx, callback)
}

// checkRecovery checks if any spec can be recovered to available
func (p *KarpenterProvider) checkRecovery(ctx context.Context, callback func(interfaces.CapacityEvent)) {
	p.failureCacheMu.RLock()
	defer p.failureCacheMu.RUnlock()

	recoveryThreshold := 10 * time.Minute // 10 minutes without new failures, try recovery

	for specName, lastFailure := range p.failureCache {
		if time.Since(lastFailure) > recoveryThreshold {
			// Check if there's a successfully running NodeClaim
			hasRunning, _ := p.hasRunningNodeClaim(ctx, specName)
			if hasRunning {
				callback(interfaces.CapacityEvent{
					SpecName:  specName,
					Status:    interfaces.CapacityAvailable,
					Reason:    "nodeclaim",
					UpdatedAt: time.Now(),
				})
			}
		}
	}
}

// hasRunningNodeClaim checks if a spec has running NodeClaim
func (p *KarpenterProvider) hasRunningNodeClaim(ctx context.Context, specName string) (bool, error) {
	var nodePool string
	for np, spec := range p.nodePoolToSpec {
		if spec == specName {
			nodePool = np
			break
		}
	}
	if nodePool == "" {
		return false, nil
	}

	list, err := p.client.Resource(nodeClaimGVR).List(ctx, metav1.ListOptions{
		LabelSelector: "karpenter.sh/nodepool=" + nodePool,
	})
	if err != nil {
		return false, err
	}

	for _, item := range list.Items {
		if p.isNodeClaimReady(&item) {
			return true, nil
		}
	}
	return false, nil
}

// isNodeClaimReady checks if NodeClaim is ready
func (p *KarpenterProvider) isNodeClaimReady(obj *unstructured.Unstructured) bool {
	conditions, found, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if !found {
		return false
	}

	for _, c := range conditions {
		cond, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		if cond["type"] == "Ready" && cond["status"] == "True" {
			return true
		}
	}
	return false
}

// handleNodeClaimDelete handles NodeClaim deletion events.
// When Karpenter deletes a NodeClaim due to capacity errors (e.g. InsufficientCapacityError),
// the NodeClaim is created and deleted so quickly that Add/Update handlers may not see the
// failure condition. By checking the failureCache on delete, we ensure the sold_out status
// is emitted even for these fast-fail scenarios.
func (p *KarpenterProvider) handleNodeClaimDelete(ctx context.Context, obj interface{}, callback func(interfaces.CapacityEvent)) {
	// Handle DeletedFinalStateUnknown (informer may wrap deleted objects)
	var u *unstructured.Unstructured
	switch t := obj.(type) {
	case *unstructured.Unstructured:
		u = t
	case cache.DeletedFinalStateUnknown:
		var ok bool
		u, ok = t.Obj.(*unstructured.Unstructured)
		if !ok {
			return
		}
	default:
		return
	}

	labels := u.GetLabels()
	nodePool := labels["karpenter.sh/nodepool"]
	specName, ok := p.nodePoolToSpec[nodePool]
	if !ok {
		return
	}

	// Check if this NodeClaim had a capacity failure condition before deletion
	conditions, found, _ := unstructured.NestedSlice(u.Object, "status", "conditions")
	if found {
		for _, c := range conditions {
			cond, ok := c.(map[string]interface{})
			if !ok {
				continue
			}
			condType, _ := cond["type"].(string)
			status, _ := cond["status"].(string)
			reason, _ := cond["reason"].(string)
			message, _ := cond["message"].(string)

			if condType == "Launched" && status == "False" && p.isCapacityError(reason, message) {
				p.failureCacheMu.Lock()
				p.failureCache[specName] = time.Now()
				p.failureCacheMu.Unlock()

				logger.WarnCtx(ctx, "NodeClaim deleted with capacity failure: spec=%s, reason=%s", specName, reason)
				callback(interfaces.CapacityEvent{
					SpecName:  specName,
					Status:    interfaces.CapacitySoldOut,
					Reason:    "nodeclaim:" + reason,
					UpdatedAt: time.Now(),
				})
				return
			}
		}
	}

	// Even without explicit failure conditions, if the NodeClaim was never ready
	// and we have a recent failure in cache, re-emit sold_out to prevent CheckAll from overriding
	p.failureCacheMu.RLock()
	lastFailure, hasRecentFailure := p.failureCache[specName]
	p.failureCacheMu.RUnlock()

	if hasRecentFailure && time.Since(lastFailure) < 5*time.Minute {
		if !p.isNodeClaimReady(u) {
			logger.WarnCtx(ctx, "Non-ready NodeClaim deleted for spec=%s (recent failure at %v), maintaining sold_out", specName, lastFailure)
			callback(interfaces.CapacityEvent{
				SpecName:  specName,
				Status:    interfaces.CapacitySoldOut,
				Reason:    "nodeclaim:deleted_unready",
				UpdatedAt: time.Now(),
			})
		}
	}
}

func (p *KarpenterProvider) handleNodeClaim(ctx context.Context, obj *unstructured.Unstructured, callback func(interfaces.CapacityEvent)) {
	labels := obj.GetLabels()
	nodePool := labels["karpenter.sh/nodepool"]
	specName, ok := p.nodePoolToSpec[nodePool]
	if !ok {
		return
	}

	conditions, found, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if !found {
		return
	}

	for _, c := range conditions {
		cond, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		condType, _ := cond["type"].(string)
		status, _ := cond["status"].(string)
		reason, _ := cond["reason"].(string)
		message, _ := cond["message"].(string)

		if condType == "Launched" {
			event := interfaces.CapacityEvent{
				SpecName:  specName,
				UpdatedAt: time.Now(),
			}

			if status == "False" && p.isCapacityError(reason, message) {
				event.Status = interfaces.CapacitySoldOut
				event.Reason = "nodeclaim:" + reason

				// Record failure time
				p.failureCacheMu.Lock()
				p.failureCache[specName] = time.Now()
				p.failureCacheMu.Unlock()

				logger.WarnCtx(ctx, "NodeClaim capacity failure detected: spec=%s, reason=%s", specName, reason)
			} else if status == "True" {
				event.Status = interfaces.CapacityAvailable
				event.Reason = "nodeclaim"

				// Clear failure cache
				p.failureCacheMu.Lock()
				delete(p.failureCache, specName)
				p.failureCacheMu.Unlock()
			} else {
				continue
			}

			callback(event)
			return
		}
	}
}

// isCapacityError checks if the error is capacity-related
func (p *KarpenterProvider) isCapacityError(reason, message string) bool {
	capacityErrors := []string{
		"InsufficientInstanceCapacity",
		"InsufficientCapacity",
		"Unsupported",
		"MaxSpotInstanceCountExceeded",
	}

	combined := reason + " " + message
	for _, err := range capacityErrors {
		if strings.Contains(combined, err) {
			return true
		}
	}
	return false
}

func (p *KarpenterProvider) Check(ctx context.Context, specName string) (*interfaces.CapacityEvent, error) {
	var nodePool string
	for np, spec := range p.nodePoolToSpec {
		if spec == specName {
			nodePool = np
			break
		}
	}
	if nodePool == "" {
		return nil, nil
	}

	list, err := p.client.Resource(nodeClaimGVR).List(ctx, metav1.ListOptions{
		LabelSelector: "karpenter.sh/nodepool=" + nodePool,
	})
	if err != nil {
		return nil, err
	}

	// Check if there are failed NodeClaims
	var hasFailure bool
	var failureReason string
	var hasSuccess bool

	for _, item := range list.Items {
		conditions, found, _ := unstructured.NestedSlice(item.Object, "status", "conditions")
		if !found {
			continue
		}

		for _, c := range conditions {
			cond, ok := c.(map[string]interface{})
			if !ok {
				continue
			}

			if cond["type"] == "Launched" {
				status, _ := cond["status"].(string)
				reason, _ := cond["reason"].(string)
				message, _ := cond["message"].(string)

				if status == "False" && p.isCapacityError(reason, message) {
					hasFailure = true
					failureReason = reason
				} else if status == "True" {
					hasSuccess = true
				}
			}
		}
	}

	event := &interfaces.CapacityEvent{
		SpecName:  specName,
		UpdatedAt: time.Now(),
	}

	// If there's a success, it's available; otherwise check for failures
	if hasSuccess {
		event.Status = interfaces.CapacityAvailable
		event.Reason = "nodeclaim"
	} else if hasFailure {
		event.Status = interfaces.CapacitySoldOut
		event.Reason = "nodeclaim:" + failureReason
	} else {
		event.Status = interfaces.CapacityAvailable // No NodeClaim also counts as available
		event.Reason = "default"
	}

	return event, nil
}

func (p *KarpenterProvider) CheckAll(ctx context.Context) ([]interfaces.CapacityEvent, error) {
	// Collect status for all specs
	specStatus := make(map[string]*interfaces.CapacityEvent)

	// Initialize specs: use failureCache to preserve recent failure status
	// instead of blindly setting everything to available
	p.failureCacheMu.RLock()
	failureCacheCopy := make(map[string]time.Time, len(p.failureCache))
	for k, v := range p.failureCache {
		failureCacheCopy[k] = v
	}
	p.failureCacheMu.RUnlock()

	for _, specName := range p.nodePoolToSpec {
		event := &interfaces.CapacityEvent{
			SpecName:  specName,
			UpdatedAt: time.Now(),
		}
		// If there's a recent failure (within 10 minutes), keep sold_out status
		if lastFailure, ok := failureCacheCopy[specName]; ok && time.Since(lastFailure) < 10*time.Minute {
			event.Status = interfaces.CapacitySoldOut
			event.Reason = "nodeclaim:recent_failure"
		} else {
			event.Status = interfaces.CapacityAvailable
			event.Reason = "default"
		}
		specStatus[specName] = event
	}

	// List all NodeClaims
	list, err := p.client.Resource(nodeClaimGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	// Analyze each NodeClaim
	for _, item := range list.Items {
		labels := item.GetLabels()
		nodePool := labels["karpenter.sh/nodepool"]
		specName, ok := p.nodePoolToSpec[nodePool]
		if !ok {
			continue
		}

		conditions, found, _ := unstructured.NestedSlice(item.Object, "status", "conditions")
		if !found {
			continue
		}

		for _, c := range conditions {
			cond, ok := c.(map[string]interface{})
			if !ok {
				continue
			}

			if cond["type"] == "Launched" {
				status, _ := cond["status"].(string)
				reason, _ := cond["reason"].(string)
				message, _ := cond["message"].(string)

				if status == "True" {
					// Has success, mark as available and clear failure cache
					specStatus[specName].Status = interfaces.CapacityAvailable
					specStatus[specName].Reason = "nodeclaim"
				} else if status == "False" && p.isCapacityError(reason, message) {
					// Mark as sold_out (even if initialized as available)
					specStatus[specName].Status = interfaces.CapacitySoldOut
					specStatus[specName].Reason = "nodeclaim:" + reason
				}
			}
		}
	}

	// Convert to array
	var events []interfaces.CapacityEvent
	for _, event := range specStatus {
		events = append(events, *event)
	}

	return events, nil
}
