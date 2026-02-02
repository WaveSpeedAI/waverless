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

	recoveryThreshold := 10 * time.Minute // 10分钟没有新失败，尝试恢复

	for specName, lastFailure := range p.failureCache {
		if time.Since(lastFailure) > recoveryThreshold {
			// 检查是否有成功运行的 NodeClaim
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

// isNodeClaimReady 检查 NodeClaim 是否就绪
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

				// 记录失败时间
				p.failureCacheMu.Lock()
				p.failureCache[specName] = time.Now()
				p.failureCacheMu.Unlock()

				logger.WarnCtx(ctx, "NodeClaim capacity failure detected: spec=%s, reason=%s", specName, reason)
			} else if status == "True" {
				event.Status = interfaces.CapacityAvailable
				event.Reason = "nodeclaim"

				// 清除失败缓存
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

// isCapacityError 判断是否是容量相关错误
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

	// 检查是否有失败的 NodeClaim
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

	// 有成功的就是 available，否则看是否有失败
	if hasSuccess {
		event.Status = interfaces.CapacityAvailable
		event.Reason = "nodeclaim"
	} else if hasFailure {
		event.Status = interfaces.CapacitySoldOut
		event.Reason = "nodeclaim:" + failureReason
	} else {
		event.Status = interfaces.CapacityAvailable // 没有 NodeClaim 也算 available
		event.Reason = "default"
	}

	return event, nil
}

func (p *KarpenterProvider) CheckAll(ctx context.Context) ([]interfaces.CapacityEvent, error) {
	// 收集所有 spec 的状态
	specStatus := make(map[string]*interfaces.CapacityEvent)

	// 初始化所有 spec 为 available
	for _, specName := range p.nodePoolToSpec {
		specStatus[specName] = &interfaces.CapacityEvent{
			SpecName:  specName,
			Status:    interfaces.CapacityAvailable,
			UpdatedAt: time.Now(),
		}
	}

	// 列出所有 NodeClaim
	list, err := p.client.Resource(nodeClaimGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	// 分析每个 NodeClaim
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
					// 有成功的，标记为 available
					specStatus[specName].Status = interfaces.CapacityAvailable
					specStatus[specName].Reason = "nodeclaim"
				} else if status == "False" && p.isCapacityError(reason, message) {
					// 只有当前还没有成功的才标记为 sold_out
					if specStatus[specName].Status != interfaces.CapacityAvailable {
						specStatus[specName].Status = interfaces.CapacitySoldOut
						specStatus[specName].Reason = "nodeclaim:" + reason
					}
				}
			}
		}
	}

	// 转换为数组
	var events []interfaces.CapacityEvent
	for _, event := range specStatus {
		events = append(events, *event)
	}

	return events, nil
}
