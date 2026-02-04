// Package lifecycle defines types for Provider lifecycle management
package lifecycle

// ProviderLifecycle defines the interface that all provider lifecycle implementations must satisfy
// This ensures type safety and consistent behavior across different providers
// Note: This is a simplified interface used by the lifecycle Manager.
// The full ProviderLifecycle interface is defined in pkg/provider/types.go
type ProviderLifecycle interface {
	// GetProviderName returns the provider name (e.g., "k8s", "novita")
	GetProviderName() string

	// StopWatchers stops all watchers for this provider
	StopWatchers() error
}
