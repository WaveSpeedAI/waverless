package queue

import (
	"fmt"

	"waverless/pkg/config"
	"waverless/pkg/interfaces"
	"waverless/pkg/queue/redis"
)

// CreateQueueProvider creates queue provider
func CreateQueueProvider(cfg *config.Config, providerType string) (interfaces.QueueProvider, error) {
	switch providerType {
	case "redis", "":
		return redis.NewRedisQueueProvider(cfg)
	default:
		return nil, fmt.Errorf("unsupported queue provider type: %s", providerType)
	}
}
