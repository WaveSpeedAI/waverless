package novita

import (
	"context"
	"sync"

	"waverless/pkg/interfaces"
	"waverless/pkg/logger"
)

// NovitaLifecycleCallbacks defines Novita lifecycle callback functions
// Defined in novita package to avoid circular imports
type NovitaLifecycleCallbacks struct {
	// Worker status change callback
	OnWorkerStatusChange func(workerID, endpoint string, podInfo *interfaces.PodInfo)
	// Worker delete callback
	OnWorkerDelete func(workerID, endpoint string)
	// Worker draining callback (triggered by scale down in Novita)
	OnWorkerDraining func(workerID, endpoint, reason string)
	// Worker failure callback
	OnWorkerFailure func(workerID, endpoint string, failureInfo *interfaces.WorkerFailureInfo)
	// Endpoint status change callback
	OnEndpointStatusChange func(endpoint, status string, desiredReplicas, readyReplicas, availableReplicas int)
}

// NovitaProviderLifecycle manages Novita provider lifecycle
type NovitaProviderLifecycle struct {
	provider  *NovitaDeploymentProvider
	ctx       context.Context
	cancel    context.CancelFunc
	callbacks *NovitaLifecycleCallbacks
	mu        sync.Mutex
	started   bool
}

// NewNovitaProviderLifecycle creates a new Novita provider lifecycle manager
func NewNovitaProviderLifecycle(p *NovitaDeploymentProvider) *NovitaProviderLifecycle {
	return &NovitaProviderLifecycle{
		provider: p,
	}
}

// GetProviderName returns the provider name
func (l *NovitaProviderLifecycle) GetProviderName() string {
	return "novita"
}

// RegisterWatchers registers all Novita watchers
func (l *NovitaProviderLifecycle) RegisterWatchers(callbacks *NovitaLifecycleCallbacks) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.started {
		logger.InfoCtx(l.ctx, "Novita lifecycle watchers already started")
		return nil
	}

	l.callbacks = callbacks
	l.ctx, l.cancel = context.WithCancel(context.Background())

	// Register worker status change watcher (worker status sync + failure detection)
	if err := l.registerWorkerStatusWatcher(); err != nil {
		logger.WarnCtx(l.ctx, "Failed to register worker status watcher: %v", err)
	}

	// Register worker delete watcher
	if err := l.registerWorkerDeleteWatcher(); err != nil {
		logger.WarnCtx(l.ctx, "Failed to register worker delete watcher: %v", err)
	}

	// Register endpoint status change watcher (via WatchReplicas)
	if err := l.registerEndpointStatusWatcher(); err != nil {
		logger.WarnCtx(l.ctx, "Failed to register endpoint status watcher: %v", err)
	}

	l.started = true
	logger.InfoCtx(l.ctx, "✅ Novita lifecycle watchers registered successfully")
	return nil
}

// StopWatchers stops all watchers
func (l *NovitaProviderLifecycle) StopWatchers() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.started {
		return nil
	}

	if l.cancel != nil {
		l.cancel()
	}

	// Stop Novita's replica watcher
	l.provider.StopReplicaWatcher()

	l.started = false
	logger.InfoCtx(context.Background(), "Novita lifecycle watchers stopped")
	return nil
}

// registerWorkerStatusWatcher registers worker status change watcher
func (l *NovitaProviderLifecycle) registerWorkerStatusWatcher() error {
	if l.callbacks == nil || l.callbacks.OnWorkerStatusChange == nil {
		return nil
	}

	// Create failure detector
	failureDetector := NewNovitaWorkerStatusMonitor(l.provider.GetClient(), nil)

	return l.provider.WatchPodStatusChange(l.ctx, func(workerID, endpoint string, info *interfaces.PodInfo) {
		// 1. Trigger status change callback
		l.callbacks.OnWorkerStatusChange(workerID, endpoint, info)

		// 2. Detect failure and trigger failure callback
		if l.callbacks.OnWorkerFailure != nil {
			if failureInfo := failureDetector.DetectFailure(info); failureInfo != nil {
				l.callbacks.OnWorkerFailure(workerID, endpoint, failureInfo)
			}
		}
	})
}

// registerWorkerDeleteWatcher registers worker delete watcher
func (l *NovitaProviderLifecycle) registerWorkerDeleteWatcher() error {
	if l.callbacks == nil || l.callbacks.OnWorkerDelete == nil {
		return nil
	}

	return l.provider.WatchPodDelete(l.ctx, func(workerID, endpoint string) {
		l.callbacks.OnWorkerDelete(workerID, endpoint)

		// Clear draining state in Redis
		if err := l.provider.ClearDrainingWorker(l.ctx, workerID); err != nil {
			logger.WarnCtx(l.ctx, "Failed to clear draining state for worker %s: %v", workerID, err)
		}
	})
}

// registerEndpointStatusWatcher registers endpoint status change watcher
func (l *NovitaProviderLifecycle) registerEndpointStatusWatcher() error {
	if l.callbacks == nil || l.callbacks.OnEndpointStatusChange == nil {
		return nil
	}

	return l.provider.WatchReplicas(l.ctx, func(event interfaces.ReplicaEvent) {
		// Calculate status
		status := "Pending"
		if event.AvailableReplicas == event.DesiredReplicas && event.DesiredReplicas > 0 {
			status = "Running"
		} else if event.DesiredReplicas == 0 {
			status = "Stopped"
		}

		l.callbacks.OnEndpointStatusChange(
			event.Name,
			status,
			event.DesiredReplicas,
			event.ReadyReplicas,
			event.AvailableReplicas,
		)
	})
}
