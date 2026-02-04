// Package provider defines provider-related types and interfaces
package provider

import (
	"waverless/pkg/interfaces"
)

// ProviderLifecycle defines the provider lifecycle interface
// Each provider implements this interface to register its watchers
type ProviderLifecycle interface {
	// RegisterWatchers registers all watchers
	// callbacks contains all callback functions, provider calls corresponding callbacks when events are detected
	RegisterWatchers(callbacks *LifecycleCallbacks) error

	// StopWatchers stops all watchers
	StopWatchers() error

	// GetProviderName returns the provider name
	GetProviderName() string
}

// LifecycleCallbacks defines lifecycle callback functions
// Provided by LifecycleManager, provider calls these callbacks to notify status changes
type LifecycleCallbacks struct {
	// Worker status change callback
	OnWorkerStatusChange func(event *WorkerStatusEvent)
	// Worker delete callback
	OnWorkerDelete func(event *WorkerDeleteEvent)
	// Worker draining callback
	OnWorkerDraining func(event *WorkerDrainingEvent)
	// Worker failure callback
	OnWorkerFailure func(event *WorkerFailureEvent)
	// Endpoint status change callback
	OnEndpointStatusChange func(event *EndpointStatusEvent)
	// Deployment change callback
	OnDeploymentChange func(event *DeploymentChangeEvent)
}

// WorkerStatusEvent represents a worker status change event
type WorkerStatusEvent struct {
	WorkerID string              // Worker ID (usually pod name)
	Endpoint string              // Endpoint name
	PodInfo  *interfaces.PodInfo // Pod information
}

// WorkerDeleteEvent represents a worker delete event
type WorkerDeleteEvent struct {
	WorkerID string // Worker ID
	Endpoint string // Endpoint name
}

// WorkerDrainingEvent represents a worker draining event
type WorkerDrainingEvent struct {
	WorkerID string // Worker ID
	Endpoint string // Endpoint name
	Reason   string // Draining reason
}

// WorkerFailureEvent represents a worker failure event
type WorkerFailureEvent struct {
	WorkerID    string                        // Worker ID
	Endpoint    string                        // Endpoint name
	FailureInfo *interfaces.WorkerFailureInfo // Failure information
}

// EndpointStatusEvent represents an endpoint status change event
type EndpointStatusEvent struct {
	Endpoint          string // Endpoint name
	Status            string // Status
	DesiredReplicas   int    // Desired replicas
	ReadyReplicas     int    // Ready replicas
	AvailableReplicas int    // Available replicas
}

// DeploymentChangeEvent represents a deployment change event
type DeploymentChangeEvent struct {
	Endpoint string // Endpoint name
}

// DeploymentProviderWithLifecycle extends the DeploymentProvider interface
// Providers that support lifecycle management should implement this interface
type DeploymentProviderWithLifecycle interface {
	interfaces.DeploymentProvider

	// GetLifecycle returns the lifecycle manager
	// Returns nil if the provider does not support lifecycle management
	GetLifecycle() ProviderLifecycle
}
