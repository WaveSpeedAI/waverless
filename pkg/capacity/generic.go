package capacity

import (
	"context"

	"waverless/pkg/interfaces"
)

// GenericProvider generic fallback implementation, does not actively sense capacity
type GenericProvider struct{}

func NewGenericProvider() *GenericProvider {
	return &GenericProvider{}
}

func (p *GenericProvider) SupportsWatch() bool { return false }

func (p *GenericProvider) Watch(ctx context.Context, callback func(interfaces.CapacityEvent)) error {
	// Does not support watch
	<-ctx.Done()
	return nil
}

func (p *GenericProvider) Check(ctx context.Context, specName string) (*interfaces.CapacityEvent, error) {
	// Default returns available
	return &interfaces.CapacityEvent{
		SpecName: specName,
		Status:   interfaces.CapacityAvailable,
	}, nil
}

func (p *GenericProvider) CheckAll(ctx context.Context) ([]interfaces.CapacityEvent, error) {
	return nil, nil
}
