package interfaces

import (
	"context"
	"time"
)

// FailureType represents the type of worker failure
// Used to categorize different failure scenarios for proper handling and user messaging
type FailureType string

const (
	// FailureTypeImagePull indicates the worker failed due to image pull issues
	// This includes ImagePullBackOff, ErrImagePull, InvalidImageName errors
	FailureTypeImagePull FailureType = "IMAGE_PULL_FAILED"

	// FailureTypeContainerCrash indicates the container crashed after starting
	// This includes CrashLoopBackOff, Error states
	FailureTypeContainerCrash FailureType = "CONTAINER_CRASH"

	// FailureTypeResourceLimit indicates the worker failed due to resource constraints
	// This includes OutOfMemory, OutOfCpu errors
	FailureTypeResourceLimit FailureType = "RESOURCE_LIMIT"

	// FailureTypeTimeout indicates the worker failed due to timeout
	// This includes image pull timeout, startup timeout
	FailureTypeTimeout FailureType = "TIMEOUT"

	// FailureTypeUnknown indicates an unknown failure type
	// Used when the failure reason cannot be classified
	FailureTypeUnknown FailureType = "UNKNOWN"
)

// ImageValidationResult represents the result of image validation
// Contains information about whether the image exists and is accessible
type ImageValidationResult struct {
	// Valid indicates if the image reference format is valid
	Valid bool `json:"valid"`

	// Exists indicates if the image exists in the registry
	Exists bool `json:"exists"`

	// Accessible indicates if the image is accessible (authentication ok)
	Accessible bool `json:"accessible"`

	// Error contains the error message if validation failed
	Error string `json:"error,omitempty"`

	// Warning contains a warning message (e.g., when validation times out but proceeds)
	Warning string `json:"warning,omitempty"`

	// CheckedAt is the timestamp when the validation was performed
	CheckedAt time.Time `json:"checkedAt"`
}

// WorkerFailureInfo represents failure information for a worker
// Contains both provider-specific details and user-friendly sanitized messages
type WorkerFailureInfo struct {
	// Type is the categorized failure type
	Type FailureType `json:"type"`

	// Reason is the provider-specific reason (e.g., K8s reason like "ImagePullBackOff")
	Reason string `json:"reason"`

	// Message is the provider-specific message with full details
	Message string `json:"message"`

	// SanitizedMsg is the user-friendly message without sensitive information
	SanitizedMsg string `json:"sanitizedMsg"`

	// OccurredAt is the timestamp when the failure occurred
	OccurredAt time.Time `json:"occurredAt"`
}

// ImageValidator interface for image validation (optional capability)
// Providers that support image validation should implement this interface
type ImageValidator interface {
	// ValidateImageFormat validates image reference format
	// Returns nil if the format is valid, otherwise returns an error with description
	// Supports formats: nginx, nginx:latest, library/nginx:1.0,
	// gcr.io/project/image:tag, registry.example.com/image:tag
	ValidateImageFormat(image string) error

	// CheckImageExists checks if image exists in registry
	// Uses Docker Registry HTTP API V2 to verify image existence
	// If cred is nil, attempts anonymous access
	// Returns ImageValidationResult with validation details
	CheckImageExists(ctx context.Context, image string, cred *RegistryCredential) (*ImageValidationResult, error)
}

// WorkerStatusCallback is called when worker status changes
// workerID: the unique identifier of the worker
// endpoint: the endpoint name the worker belongs to
// info: the failure information if the worker failed, nil if recovered
type WorkerStatusCallback func(workerID, endpoint string, info *WorkerFailureInfo)

// WorkerStatusWatcher interface for watching worker status changes (optional capability)
// Providers that support status watching should implement this interface
type WorkerStatusWatcher interface {
	// WatchWorkerStatus watches worker status changes and calls callback on failure
	// The callback is invoked when a worker enters a failed state
	// This method blocks until the context is cancelled
	WatchWorkerStatus(ctx context.Context, callback WorkerStatusCallback) error
}

// WorkerTerminator interface for terminating workers (optional capability)
// Providers that support worker termination should implement this interface
type WorkerTerminator interface {
	// TerminateWorker terminates a specific worker
	// endpoint: the endpoint name the worker belongs to
	// workerID: the unique identifier of the worker to terminate
	// reason: the reason for termination (for logging and auditing)
	// Returns error if termination fails
	TerminateWorker(ctx context.Context, endpoint, workerID string, reason string) error
}
