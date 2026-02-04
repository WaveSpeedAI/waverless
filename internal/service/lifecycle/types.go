// Package lifecycle defines types for Provider lifecycle management
package lifecycle

import (
	"waverless/pkg/interfaces"
)

// WorkerStatusEvent represents a Worker status change event
type WorkerStatusEvent struct {
	WorkerID string              // Worker ID (usually Pod name)
	Endpoint string              // Endpoint name
	PodInfo  *interfaces.PodInfo // Pod information
}

// WorkerDeleteEvent represents a Worker deletion event
type WorkerDeleteEvent struct {
	WorkerID string // Worker ID
	Endpoint string // Endpoint name
}

// WorkerDrainingEvent represents a Worker draining event
type WorkerDrainingEvent struct {
	WorkerID string // Worker ID
	Endpoint string // Endpoint name
	Reason   string // Draining reason
}

// WorkerFailureEvent represents a Worker failure event
type WorkerFailureEvent struct {
	WorkerID    string                        // Worker ID
	Endpoint    string                        // Endpoint name
	FailureInfo *interfaces.WorkerFailureInfo // Failure information
}

// EndpointStatusEvent represents an Endpoint status change event
type EndpointStatusEvent struct {
	Endpoint          string // Endpoint name
	Status            string // Status
	DesiredReplicas   int    // Desired replica count
	ReadyReplicas     int    // Ready replica count
	AvailableReplicas int    // Available replica count
}

// DeploymentChangeEvent represents a Deployment change event
type DeploymentChangeEvent struct {
	Endpoint string // Endpoint name
}
