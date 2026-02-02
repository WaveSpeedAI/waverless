package interfaces

import "time"

type CapacityStatus string

const (
	CapacityAvailable CapacityStatus = "available"
	CapacityLimited   CapacityStatus = "limited"
	CapacitySoldOut   CapacityStatus = "sold_out"
)

// CapacityEvent capacity change event
type CapacityEvent struct {
	SpecName  string         `json:"specName"`
	Status    CapacityStatus `json:"status"`
	Reason    string         `json:"reason,omitempty"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

// SpecWithCapacity spec info with capacity status
type SpecWithCapacity struct {
	*SpecInfo
	Capacity     CapacityStatus `json:"capacity"`
	SpotScore    int            `json:"spotScore,omitempty"`
	SpotPrice    float64        `json:"spotPrice,omitempty"`
	RunningCount int            `json:"runningCount,omitempty"`
	PendingCount int            `json:"pendingCount,omitempty"`
}
