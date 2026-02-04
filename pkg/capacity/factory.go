package capacity

import (
	"k8s.io/client-go/dynamic"
)

type ProviderType string

const (
	ProviderKarpenter ProviderType = "karpenter"
	ProviderGeneric   ProviderType = "generic"
)

// NewProvider creates a Provider based on type
func NewProvider(providerType ProviderType, dynamicClient dynamic.Interface, nodePoolToSpec map[string]string) Provider {
	switch providerType {
	case ProviderKarpenter:
		return NewKarpenterProvider(dynamicClient, nodePoolToSpec)
	default:
		return NewGenericProvider()
	}
}
