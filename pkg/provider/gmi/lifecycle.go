package gmi

import (
	"context"
	"sync"
	"time"

	"waverless/pkg/interfaces"
	"waverless/pkg/logger"
)

// GMILifecycleCallbacks defines GMI lifecycle callback functions
type GMILifecycleCallbacks struct {
	OnWorkerStatusChange   func(workerID, endpoint string, podInfo *interfaces.PodInfo)
	OnWorkerDelete         func(workerID, endpoint string, deletedAt *time.Time)
	OnWorkerDraining       func(workerID, endpoint, reason string)
	OnWorkerFailure        func(workerID, endpoint string, failureInfo *interfaces.WorkerFailureInfo)
	OnEndpointStatusChange func(endpoint, status string, desiredReplicas, readyReplicas, availableReplicas int)
}

// GMIProviderLifecycle manages GMI provider lifecycle
type GMIProviderLifecycle struct {
	provider          *GMIDeploymentProvider
	ctx               context.Context
	cancel            context.CancelFunc
	callbacks         *GMILifecycleCallbacks
	mu                sync.Mutex
	started           bool
	drainingReported  sync.Map // workerID → true, tracks which workers have been reported as draining
}

// NewGMIProviderLifecycle creates a new GMI provider lifecycle manager
func NewGMIProviderLifecycle(p *GMIDeploymentProvider) *GMIProviderLifecycle {
	return &GMIProviderLifecycle{
		provider: p,
	}
}

// GetProviderName returns the provider name
func (l *GMIProviderLifecycle) GetProviderName() string {
	return "gmi"
}

// RegisterWatchers registers all GMI watchers
func (l *GMIProviderLifecycle) RegisterWatchers(callbacks *GMILifecycleCallbacks) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.started {
		logger.InfoCtx(l.ctx, "GMI lifecycle watchers already started")
		return nil
	}

	l.callbacks = callbacks
	l.ctx, l.cancel = context.WithCancel(context.Background())

	// Register worker status change watcher
	if err := l.registerWorkerStatusWatcher(); err != nil {
		logger.WarnCtx(l.ctx, "Failed to register GMI worker status watcher: %v", err)
	}

	// Register worker delete watcher
	if err := l.registerWorkerDeleteWatcher(); err != nil {
		logger.WarnCtx(l.ctx, "Failed to register GMI worker delete watcher: %v", err)
	}

	// Register endpoint status change watcher (via WatchReplicas)
	if err := l.registerEndpointStatusWatcher(); err != nil {
		logger.WarnCtx(l.ctx, "Failed to register GMI endpoint status watcher: %v", err)
	}

	l.started = true
	logger.InfoCtx(l.ctx, "GMI lifecycle watchers registered successfully")
	return nil
}

// StopWatchers stops all watchers
func (l *GMIProviderLifecycle) StopWatchers() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.started {
		return nil
	}

	if l.cancel != nil {
		l.cancel()
	}

	l.provider.StopWatcher()

	l.started = false
	logger.InfoCtx(context.Background(), "GMI lifecycle watchers stopped")
	return nil
}

// registerWorkerStatusWatcher registers worker status change watcher
func (l *GMIProviderLifecycle) registerWorkerStatusWatcher() error {
	if l.callbacks == nil || l.callbacks.OnWorkerStatusChange == nil {
		return nil
	}

	// Create failure detector
	failureDetector := NewGMIWorkerStatusMonitor(nil)

	return l.provider.WatchPodStatusChange(l.ctx, func(workerID, endpoint string, info *interfaces.PodInfo) {
		// 1. Trigger status change callback
		l.callbacks.OnWorkerStatusChange(workerID, endpoint, info)

		// 2. Detect draining state and trigger OnWorkerDraining callback
		if l.callbacks.OnWorkerDraining != nil && info != nil {
			isDraining := info.DeletionTimestamp != "" ||
				info.Status == "Draining" || info.Status == "Terminating"
			if isDraining {
				// Only report once per worker (cleared on delete)
				if _, alreadyReported := l.drainingReported.LoadOrStore(workerID, true); !alreadyReported {
					reason := "pod_terminating"
					if info.Status == "Draining" {
						reason = "drain_requested"
					}
					l.callbacks.OnWorkerDraining(workerID, endpoint, reason)

					// Mark in Redis draining store
					if err := l.provider.MarkWorkerDraining(l.ctx, workerID); err != nil {
						logger.WarnCtx(l.ctx, "Failed to mark worker %s as draining in Redis: %v", workerID, err)
					}
				}
			}
		}

		// 3. Detect failure and trigger failure callback
		if l.callbacks.OnWorkerFailure != nil {
			if failureInfo := failureDetector.DetectFailure(info); failureInfo != nil {
				l.callbacks.OnWorkerFailure(workerID, endpoint, failureInfo)
			}
		}
	})
}

// registerWorkerDeleteWatcher registers worker delete watcher
func (l *GMIProviderLifecycle) registerWorkerDeleteWatcher() error {
	if l.callbacks == nil || l.callbacks.OnWorkerDelete == nil {
		return nil
	}

	return l.provider.WatchPodDelete(l.ctx, func(workerID, endpoint string, deletedAt *time.Time) {
		// Clear draining state in Redis when worker is confirmed deleted
		if err := l.provider.ClearDrainingWorker(l.ctx, workerID); err != nil {
			logger.WarnCtx(l.ctx, "Failed to clear draining state for worker %s: %v", workerID, err)
		}

		// Clear draining reported tracker to avoid stale entries
		l.drainingReported.Delete(workerID)

		if deletedAt == nil {
			now := time.Now()
			deletedAt = &now
		}
		l.callbacks.OnWorkerDelete(workerID, endpoint, deletedAt)
	})
}

// registerEndpointStatusWatcher registers endpoint status change watcher
func (l *GMIProviderLifecycle) registerEndpointStatusWatcher() error {
	if l.callbacks == nil || l.callbacks.OnEndpointStatusChange == nil {
		return nil
	}

	return l.provider.WatchReplicas(l.ctx, func(event interfaces.ReplicaEvent) {
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
