package lifecycle

import (
	"context"
	"fmt"
	"sync"

	"waverless/internal/service"
	endpointsvc "waverless/internal/service/endpoint"
	"waverless/pkg/interfaces"
	"waverless/pkg/logger"
	"waverless/pkg/provider"
	"waverless/pkg/provider/k8s"
	"waverless/pkg/provider/novita"
	"waverless/pkg/status"
	"waverless/pkg/store/mysql"
)

// Manager is the Provider lifecycle manager
// It manages watcher registration for all Providers
type Manager struct {
	ctx                context.Context
	providers          map[string]ProviderLifecycle // stores registered provider lifecycles
	mu                 sync.RWMutex
	callbackHandler    *CallbackHandler
	workerRepo         *mysql.WorkerRepository
	endpointRepo       *mysql.EndpointRepository
	workerService      *service.WorkerService
	workerEventService *service.WorkerEventService

	// statusEventRecorder records status/phase/failure events to the status_events table.
	statusEventRecorder k8s.StatusEventRecorder
	// pendingDetector detects the specific pending phase of a pod.
	pendingDetector *status.PendingPhaseDetector
}

// NewManager creates a new lifecycle manager
func NewManager(
	ctx context.Context,
	workerRepo *mysql.WorkerRepository,
	endpointRepo *mysql.EndpointRepository,
	workerService *service.WorkerService,
	workerEventService *service.WorkerEventService,
) *Manager {
	callbackHandler := NewCallbackHandler(ctx, workerRepo, endpointRepo, workerService, workerEventService)

	return &Manager{
		ctx:                ctx,
		providers:          make(map[string]ProviderLifecycle),
		callbackHandler:    callbackHandler,
		workerRepo:         workerRepo,
		endpointRepo:       endpointRepo,
		workerService:      workerService,
		workerEventService: workerEventService,
	}
}

// SetEndpointService sets the endpoint service on the callback handler for status summary updates.
// This must be called after NewManager and before any provider registration to ensure
// status summary updates are triggered on worker status changes.
func (m *Manager) SetEndpointService(svc *endpointsvc.Service) {
	m.callbackHandler.SetEndpointService(svc)
}

// SetSpotPriceLookup sets the spot price lookup on the callback handler.
// This enables recording the Spot price at worker creation time.
func (m *Manager) SetSpotPriceLookup(lookup SpotPriceLookup) {
	m.callbackHandler.SetSpotPriceLookup(lookup)
}

// SetStatusEventRecorder sets the status event recorder for recording status/phase/failure events.
// This must be called before RegisterK8sProvider to ensure events are recorded.
func (m *Manager) SetStatusEventRecorder(recorder k8s.StatusEventRecorder) {
	m.statusEventRecorder = recorder
}

// SetPendingPhaseDetector sets the pending phase detector for detecting pending phases.
// This must be called before RegisterK8sProvider to ensure pending phases are detected.
func (m *Manager) SetPendingPhaseDetector(detector *status.PendingPhaseDetector) {
	m.pendingDetector = detector
}

// RegisterK8sProvider registers K8s Provider and starts its watchers
func (m *Manager) RegisterK8sProvider(provider *k8s.K8sDeploymentProvider) error {
	if provider == nil {
		return fmt.Errorf("k8s provider is nil")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	name := "k8s"
	if _, exists := m.providers[name]; exists {
		logger.WarnCtx(m.ctx, "Provider %s already registered, skipping", name)
		return nil
	}

	// Get lifecycle
	lifecycle := provider.GetLifecycle()
	if lifecycle == nil {
		return fmt.Errorf("k8s provider lifecycle is nil")
	}

	// Create K8s callbacks
	callbacks := m.createK8sCallbacks(provider)

	// Register watchers
	if err := lifecycle.RegisterWatchers(callbacks); err != nil {
		return fmt.Errorf("failed to register watchers for k8s provider: %w", err)
	}

	m.providers[name] = lifecycle
	logger.InfoCtx(m.ctx, "K8s Provider registered successfully")

	return nil
}

// RegisterNovitaProvider registers Novita Provider and starts its watchers
func (m *Manager) RegisterNovitaProvider(provider *novita.NovitaDeploymentProvider) error {
	if provider == nil {
		return fmt.Errorf("novita provider is nil")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	name := "novita"
	if _, exists := m.providers[name]; exists {
		logger.WarnCtx(m.ctx, "Provider %s already registered, skipping", name)
		return nil
	}

	// Get lifecycle
	lifecycle := provider.GetLifecycle()
	if lifecycle == nil {
		return fmt.Errorf("novita provider lifecycle is nil")
	}

	// Create Novita callbacks
	callbacks := m.createNovitaCallbacks()

	// Register watchers
	if err := lifecycle.RegisterWatchers(callbacks); err != nil {
		return fmt.Errorf("failed to register watchers for novita provider: %w", err)
	}

	m.providers[name] = lifecycle
	logger.InfoCtx(m.ctx, "Novita Provider registered successfully")

	return nil
}

// createK8sCallbacks creates K8s callback functions
func (m *Manager) createK8sCallbacks(k8sProvider *k8s.K8sDeploymentProvider) *k8s.K8sLifecycleCallbacks {
	return &k8s.K8sLifecycleCallbacks{
		OnWorkerStatusChange: func(workerID, endpoint string, podInfo *interfaces.PodInfo) {
			m.callbackHandler.HandleWorkerStatusChange(&provider.WorkerStatusEvent{
				WorkerID: workerID,
				Endpoint: endpoint,
				PodInfo:  podInfo,
			})
		},
		OnWorkerDelete: func(workerID, endpoint string) {
			m.callbackHandler.HandleWorkerDelete(&provider.WorkerDeleteEvent{
				WorkerID: workerID,
				Endpoint: endpoint,
			})
		},
		OnWorkerDraining: func(workerID, endpoint, reason string) {
			m.callbackHandler.HandleWorkerDraining(&provider.WorkerDrainingEvent{
				WorkerID: workerID,
				Endpoint: endpoint,
				Reason:   reason,
			})
		},
		OnWorkerFailure: func(workerID, endpoint string, failureInfo *interfaces.WorkerFailureInfo) {
			m.callbackHandler.HandleWorkerFailure(&provider.WorkerFailureEvent{
				WorkerID:    workerID,
				Endpoint:    endpoint,
				FailureInfo: failureInfo,
			})
		},
		OnEndpointStatusChange: func(endpoint, status string, desiredReplicas, readyReplicas, availableReplicas int) {
			m.callbackHandler.HandleEndpointStatusChange(&provider.EndpointStatusEvent{
				Endpoint:          endpoint,
				Status:            status,
				DesiredReplicas:   desiredReplicas,
				ReadyReplicas:     readyReplicas,
				AvailableReplicas: availableReplicas,
			})
		},
		OnDeploymentChange: func(endpoint string) {
			// When Deployment spec changes (e.g., image update), optimize rolling update
			// by setting PodDeletionCost for idle workers to -1000, so K8s deletes them first
			m.handleDeploymentChangeWithOptimization(k8sProvider, endpoint)
		},
		// Wire status event recorder and pending phase detector for status_events recording
		StatusEventRecorder:  m.statusEventRecorder,
		PendingPhaseDetector: m.pendingDetector,
		// Update pending phase in workers table when detected or cleared
		OnPendingPhaseUpdate: func(podName, endpoint, phase, reason, message string) {
			if m.workerRepo == nil {
				return
			}
			if phase == "" {
				// Clear pending phase
				if err := m.workerRepo.ClearPendingPhase(m.ctx, podName); err != nil {
					logger.WarnCtx(m.ctx, "Failed to clear pending phase for worker %s: %v", podName, err)
				}
			} else {
				if err := m.workerRepo.UpdatePendingPhase(m.ctx, podName, phase, reason, message); err != nil {
					logger.WarnCtx(m.ctx, "Failed to update pending phase for worker %s: %v", podName, err)
				} else {
					logger.DebugCtx(m.ctx, "Updated pending phase for worker %s: %s", podName, phase)
				}
			}
		},
	}
}

// createNovitaCallbacks creates Novita callback functions
func (m *Manager) createNovitaCallbacks() *novita.NovitaLifecycleCallbacks {
	return &novita.NovitaLifecycleCallbacks{
		OnWorkerStatusChange: func(workerID, endpoint string, podInfo *interfaces.PodInfo) {
			m.callbackHandler.HandleWorkerStatusChange(&provider.WorkerStatusEvent{
				WorkerID: workerID,
				Endpoint: endpoint,
				PodInfo:  podInfo,
			})
		},
		OnWorkerDelete: func(workerID, endpoint string) {
			m.callbackHandler.HandleWorkerDelete(&provider.WorkerDeleteEvent{
				WorkerID: workerID,
				Endpoint: endpoint,
			})
		},
		OnWorkerDraining: func(workerID, endpoint, reason string) {
			m.callbackHandler.HandleWorkerDraining(&provider.WorkerDrainingEvent{
				WorkerID: workerID,
				Endpoint: endpoint,
				Reason:   reason,
			})
		},
		OnWorkerFailure: func(workerID, endpoint string, failureInfo *interfaces.WorkerFailureInfo) {
			m.callbackHandler.HandleWorkerFailure(&provider.WorkerFailureEvent{
				WorkerID:    workerID,
				Endpoint:    endpoint,
				FailureInfo: failureInfo,
			})
		},
		OnEndpointStatusChange: func(endpoint, status string, desiredReplicas, readyReplicas, availableReplicas int) {
			m.callbackHandler.HandleEndpointStatusChange(&provider.EndpointStatusEvent{
				Endpoint:          endpoint,
				Status:            status,
				DesiredReplicas:   desiredReplicas,
				ReadyReplicas:     readyReplicas,
				AvailableReplicas: availableReplicas,
			})
		},
	}
}

// UnregisterProvider unregisters a Provider and stops its watchers
func (m *Manager) UnregisterProvider(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	provider, exists := m.providers[name]
	if !exists {
		return fmt.Errorf("provider %s not found", name)
	}

	// Call StopWatchers using the interface method
	if err := provider.StopWatchers(); err != nil {
		logger.WarnCtx(m.ctx, "Failed to stop watchers for provider %s: %v", name, err)
	}

	delete(m.providers, name)
	logger.InfoCtx(m.ctx, "Provider %s unregistered", name)

	return nil
}

// StopAll stops all Provider watchers
func (m *Manager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name, provider := range m.providers {
		if err := provider.StopWatchers(); err != nil {
			logger.WarnCtx(m.ctx, "Failed to stop watchers for provider %s: %v", name, err)
		}
	}

	m.providers = make(map[string]ProviderLifecycle)
	logger.InfoCtx(m.ctx, "All providers stopped")
}

// GetRegisteredProviders returns the list of registered Providers
func (m *Manager) GetRegisteredProviders() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.providers))
	for name := range m.providers {
		names = append(names, name)
	}
	return names
}

// IsProviderRegistered checks if a Provider is registered
func (m *Manager) IsProviderRegistered(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, exists := m.providers[name]
	return exists
}

// handleDeploymentChangeWithOptimization handles Deployment changes and optimizes rolling update
// When Deployment spec changes (e.g., image update), find idle workers and set their PodDeletionCost to -1000
// This way K8s will prioritize deleting these idle pods during rolling update, reducing impact on busy workers
func (m *Manager) handleDeploymentChangeWithOptimization(k8sProvider *k8s.K8sDeploymentProvider, endpoint string) {
	logger.InfoCtx(m.ctx, "Deployment spec changed for endpoint %s, optimizing rolling update...", endpoint)

	// Check if K8s provider is available
	if k8sProvider == nil {
		logger.WarnCtx(m.ctx, "K8s provider not available, skipping rolling update optimization")
		return
	}

	// Get all Workers for this Endpoint
	workers, err := m.workerService.ListWorkers(m.ctx, endpoint)
	if err != nil {
		logger.ErrorCtx(m.ctx, "Failed to get workers for endpoint %s: %v", endpoint, err)
		return
	}

	if len(workers) == 0 {
		logger.InfoCtx(m.ctx, "No workers found for endpoint %s, nothing to optimize", endpoint)
		return
	}

	// Set PodDeletionCost based on workload
	// Idle worker: -1000 (delete first)
	// Busy worker: 1000 (delete last)
	idleCount := 0
	busyCount := 0
	for _, w := range workers {
		if w.CurrentJobs == 0 {
			// Idle worker: set PodDeletionCost to -1000, K8s will delete it first
			if err := k8sProvider.SetPodDeletionCost(m.ctx, w.PodName, -1000); err != nil {
				logger.WarnCtx(m.ctx, "Failed to set pod deletion cost for idle worker %s: %v", w.PodName, err)
			} else {
				idleCount++
				logger.InfoCtx(m.ctx, "Idle worker %s: deletion-cost = -1000 (will be deleted first by K8s)", w.PodName)
			}
		} else {
			// Busy worker: set PodDeletionCost to 1000, K8s will delete it last
			if err := k8sProvider.SetPodDeletionCost(m.ctx, w.PodName, 1000); err != nil {
				logger.WarnCtx(m.ctx, "Failed to set pod deletion cost for busy worker %s: %v", w.PodName, err)
			} else {
				busyCount++
				logger.InfoCtx(m.ctx, "Busy worker %s (jobs=%d): deletion-cost = 1000 (will be deleted last by K8s)",
					w.PodName, w.CurrentJobs)
			}
		}
	}

	logger.InfoCtx(m.ctx, "Pod deletion priorities set for endpoint %s: %d idle workers (delete first), %d busy workers (delete last)",
		endpoint, idleCount, busyCount)
	logger.InfoCtx(m.ctx, "Workers will be marked as DRAINING by PodWatcher when K8s actually deletes them (respects maxUnavailable)")
}
