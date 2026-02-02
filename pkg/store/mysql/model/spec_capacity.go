package model

import (
	"time"

	"github.com/shopspring/decimal"
)

type CapacityStatus string

const (
	CapacityAvailable CapacityStatus = "available"
	CapacityLimited   CapacityStatus = "limited"
	CapacitySoldOut   CapacityStatus = "sold_out"
)

// SpecCapacity spec capacity status table
type SpecCapacity struct {
	ID       int64          `gorm:"primaryKey;autoIncrement" json:"id"`
	SpecName string         `gorm:"column:spec_name;type:varchar(100);not null;uniqueIndex:idx_spec_name" json:"spec_name"`
	Status   CapacityStatus `gorm:"column:status;type:varchar(20);not null;default:available" json:"status"`
	Reason   string         `gorm:"column:reason;type:varchar(255)" json:"reason"`

	// Statistics
	RunningCount int `gorm:"column:running_count;not null;default:0" json:"running_count"`
	PendingCount int `gorm:"column:pending_count;not null;default:0" json:"pending_count"`
	FailureCount int `gorm:"column:failure_count;not null;default:0" json:"failure_count"`

	// Spot info
	SpotScore    *int             `gorm:"column:spot_score" json:"spot_score"`                    // 1-10
	SpotPrice    *decimal.Decimal `gorm:"column:spot_price;type:decimal(10,6)" json:"spot_price"` // USD/hour
	InstanceType string           `gorm:"column:instance_type;type:varchar(50)" json:"instance_type"`

	// Timestamps
	LastSuccessAt   *time.Time `gorm:"column:last_success_at;type:datetime(3)" json:"last_success_at"`
	LastFailureAt   *time.Time `gorm:"column:last_failure_at;type:datetime(3)" json:"last_failure_at"`
	LastSpotCheckAt *time.Time `gorm:"column:last_spot_check_at;type:datetime(3)" json:"last_spot_check_at"`
	UpdatedAt       time.Time  `gorm:"column:updated_at;type:datetime(3);not null;autoUpdateTime" json:"updated_at"`
}

func (SpecCapacity) TableName() string {
	return "spec_capacity"
}
