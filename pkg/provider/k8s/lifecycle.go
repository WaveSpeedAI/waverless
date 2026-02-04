package k8s

import (
	"context"
	"sync"

	appsv1 "k8s.io/api/apps/v1"

	"waverless/pkg/interfaces"
	"waverless/pkg/logger"
)

// K8sLifecycleCallbacks defines K8s lifecycle callback functions
// Defined in k8s package to avoid circular imports
type K8sLifecycleCallbacks struct {
	// Worker status change callback
	OnWorkerStatusChange func(workerID, endpoint string, podInfo *interfaces.PodInfo)
	// Worker delete callback
	OnWorkerDelete func(workerID, endpoint string)
	// Worker draining callback
	OnWorkerDraining func(workerID, endpoint, reason string)
	// Worker failure callback
	OnWorkerFailure func(workerID, endpoint string, failureInfo *interfaces.WorkerFailureInfo)
	// Endpoint status change callback
	OnEndpointStatusChange func(endpoint, status string, desiredReplicas, readyReplicas, availableReplicas int)
	// Deployment change callback
	OnDeploymentChange func(endpoint string)
}

// K8sProviderLifecycle manages K8s provider lifecycle
type K8sProviderLifecycle struct {
	provider  *K8sDeploymentProvider
	ctx       context.Context
	cancel    context.CancelFunc
	callbacks *K8sLifecycleCallbacks
	mu        sync.Mutex
	started   bool
}

// NewK8sProviderLifecycle creates a new K8s provider lifecycle manager
func NewK8sProviderLifecycle(p *K8sDeploymentProvider) *K8sProviderLifecycle {
	return &K8sProviderLifecycle{
		provider: p,
	}
}

// GetProviderName returns the provider name
func (l *K8sProviderLifecycle) GetProviderName() string {
	return "k8s"
}

// RegisterWatchers registers all K8s watchers
func (l *K8sProviderLifecycle) RegisterWatchers(callbacks *K8sLifecycleCallbacks) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.started {
		logger.InfoCtx(l.ctx, "K8s lifecycle watchers already started")
		return nil
	}

	l.callbacks = callbacks
	l.ctx, l.cancel = context.WithCancel(context.Background())

	// Register pod status change watcher (worker status sync + failure detection)
	if err := l.registerPodStatusWatcher(); err != nil {
		logger.WarnCtx(l.ctx, "Failed to register pod status watcher: %v", err)
	}

	// Register pod delete watcher (worker deletion handling)
	if err := l.registerPodDeleteWatcher(); err != nil {
		logger.WarnCtx(l.ctx, "Failed to register pod delete watcher: %v", err)
	}

	// Register pod terminating watcher (draining status handling)
	if err := l.registerPodTerminatingWatcher(); err != nil {
		logger.WarnCtx(l.ctx, "Failed to register pod terminating watcher: %v", err)
	}

	// Register spot interruption watcher
	if err := l.registerSpotInterruptionWatcher(); err != nil {
		logger.WarnCtx(l.ctx, "Failed to register spot interruption watcher: %v", err)
	}

	// Register deployment spec change watcher
	if err := l.registerDeploymentSpecChangeWatcher(); err != nil {
		logger.WarnCtx(l.ctx, "Failed to register deployment spec change watcher: %v", err)
	}

	// Register deployment status change watcher
	if err := l.registerDeploymentStatusChangeWatcher(); err != nil {
		logger.WarnCtx(l.ctx, "Failed to register deployment status change watcher: %v", err)
	}

	l.started = true
	logger.InfoCtx(l.ctx, "✅ K8s lifecycle watchers registered successfully")
	return nil
}

// StopWatchers stops all watchers
func (l *K8sProviderLifecycle) StopWatchers() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.started {
		return nil
	}

	if l.cancel != nil {
		l.cancel()
	}

	l.started = false
	logger.InfoCtx(context.Background(), "K8s lifecycle watchers stopped")
	return nil
}

// registerPodStatusWatcher registers pod status change watcher
func (l *K8sProviderLifecycle) registerPodStatusWatcher() error {
	if l.callbacks == nil || l.callbacks.OnWorkerStatusChange == nil {
		return nil
	}

	// Create failure detector
	failureDetector := NewK8sWorkerStatusMonitor(l.provider.GetManager(), nil)

	return l.provider.WatchPodStatusChange(l.ctx, func(podName, endpoint string, info *interfaces.PodInfo) {
		// 1. Trigger status change callback
		l.callbacks.OnWorkerStatusChange(podName, endpoint, info)

		// 2. Detect failure and trigger failure callback
		if l.callbacks.OnWorkerFailure != nil {
			if failureInfo := failureDetector.DetectFailure(info); failureInfo != nil {
				l.callbacks.OnWorkerFailure(podName, endpoint, failureInfo)
			}
		}
	})
}

// registerPodDeleteWatcher registers pod delete watcher
func (l *K8sProviderLifecycle) registerPodDeleteWatcher() error {
	if l.callbacks == nil || l.callbacks.OnWorkerDelete == nil {
		return nil
	}

	return l.provider.WatchPodDelete(l.ctx, func(podName, endpoint string) {
		l.callbacks.OnWorkerDelete(podName, endpoint)
	})
}

// registerPodTerminatingWatcher registers pod terminating watcher (for draining)
func (l *K8sProviderLifecycle) registerPodTerminatingWatcher() error {
	if l.callbacks == nil || l.callbacks.OnWorkerDraining == nil {
		return nil
	}

	return l.provider.WatchPodTerminating(l.ctx, func(podName, endpoint string) {
		l.callbacks.OnWorkerDraining(podName, endpoint, "pod_terminating")

		// Also mark pod as draining (K8s level marking)
		if err := l.provider.MarkPodDraining(l.ctx, podName); err != nil {
			logger.WarnCtx(l.ctx, "Failed to mark pod %s as draining: %v", podName, err)
		}
	})
}

// registerSpotInterruptionWatcher registers spot interruption watcher
func (l *K8sProviderLifecycle) registerSpotInterruptionWatcher() error {
	if l.callbacks == nil || l.callbacks.OnWorkerDraining == nil {
		return nil
	}

	return l.provider.WatchSpotInterruption(l.ctx, func(podName, endpoint, reason string) {
		logger.WarnCtx(l.ctx, "🚨 SPOT INTERRUPTION detected for Pod %s (endpoint: %s), reason: %s",
			podName, endpoint, reason)

		l.callbacks.OnWorkerDraining(podName, endpoint, "spot_interruption: "+reason)

		// Mark pod as draining
		if err := l.provider.MarkPodDraining(l.ctx, podName); err != nil {
			logger.WarnCtx(l.ctx, "Failed to mark pod %s as draining: %v", podName, err)
		}
	})
}

// registerDeploymentSpecChangeWatcher registers deployment spec change watcher
func (l *K8sProviderLifecycle) registerDeploymentSpecChangeWatcher() error {
	if l.callbacks == nil || l.callbacks.OnDeploymentChange == nil {
		return nil
	}

	return l.provider.WatchDeploymentSpecChange(l.ctx, func(endpoint string) {
		l.callbacks.OnDeploymentChange(endpoint)
	})
}

// registerDeploymentStatusChangeWatcher registers deployment status change watcher
func (l *K8sProviderLifecycle) registerDeploymentStatusChangeWatcher() error {
	if l.callbacks == nil || l.callbacks.OnEndpointStatusChange == nil {
		return nil
	}

	return l.provider.WatchDeploymentStatusChange(l.ctx, func(endpoint string, deployment *appsv1.Deployment) {
		// Calculate status
		status := "Pending"
		if deployment.Status.AvailableReplicas == *deployment.Spec.Replicas && *deployment.Spec.Replicas > 0 {
			status = "Running"
		} else if *deployment.Spec.Replicas == 0 {
			status = "Stopped"
		}

		l.callbacks.OnEndpointStatusChange(
			endpoint,
			status,
			int(*deployment.Spec.Replicas),
			int(deployment.Status.ReadyReplicas),
			int(deployment.Status.AvailableReplicas),
		)
	})
}
