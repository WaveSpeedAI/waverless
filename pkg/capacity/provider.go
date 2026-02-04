package capacity

import (
	"context"

	"waverless/pkg/interfaces"
)

// Provider defines the capacity awareness interface
type Provider interface {
	// SupportsWatch returns whether watch mode is supported
	SupportsWatch() bool

	// Watch passively listens for capacity changes
	Watch(ctx context.Context, callback func(interfaces.CapacityEvent)) error

	// Check actively queries capacity status for a specific spec
	Check(ctx context.Context, specName string) (*interfaces.CapacityEvent, error)

	// CheckAll batch queries all specs
	CheckAll(ctx context.Context) ([]interfaces.CapacityEvent, error)
}
