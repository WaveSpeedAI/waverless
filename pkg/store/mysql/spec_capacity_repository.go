package mysql

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"waverless/pkg/store/mysql/model"
)

type SpecCapacityRepository struct {
	ds *Datastore
}

func NewSpecCapacityRepository(ds *Datastore) *SpecCapacityRepository {
	return &SpecCapacityRepository{ds: ds}
}

func (r *SpecCapacityRepository) Get(ctx context.Context, specName string) (*model.SpecCapacity, error) {
	var cap model.SpecCapacity
	err := r.ds.db.WithContext(ctx).Where("spec_name = ?", specName).First(&cap).Error
	if err != nil {
		return nil, err
	}
	return &cap, nil
}

func (r *SpecCapacityRepository) List(ctx context.Context) ([]*model.SpecCapacity, error) {
	var caps []*model.SpecCapacity
	err := r.ds.db.WithContext(ctx).Find(&caps).Error
	return caps, err
}

func (r *SpecCapacityRepository) Upsert(ctx context.Context, cap *model.SpecCapacity) error {
	return r.ds.db.WithContext(ctx).Save(cap).Error
}

func (r *SpecCapacityRepository) UpdateStatus(ctx context.Context, specName string, status model.CapacityStatus, reason string) error {
	now := time.Now()
	updates := map[string]interface{}{
		"status":     status,
		"reason":     reason,
		"updated_at": now,
	}
	if status == model.CapacitySoldOut {
		updates["last_failure_at"] = now
		updates["failure_count"] = gorm.Expr("failure_count + 1")
	} else if status == model.CapacityAvailable {
		updates["last_success_at"] = now
		updates["failure_count"] = 0
	}
	return r.ds.db.WithContext(ctx).Model(&model.SpecCapacity{}).
		Where("spec_name = ?", specName).Updates(updates).Error
}

func (r *SpecCapacityRepository) UpdateCounts(ctx context.Context, specName string, running, pending int) error {
	return r.ds.db.WithContext(ctx).Model(&model.SpecCapacity{}).
		Where("spec_name = ?", specName).
		Updates(map[string]interface{}{
			"running_count": running,
			"pending_count": pending,
			"updated_at":    time.Now(),
		}).Error
}

// UpdateSpotInfo updates Spot info
func (r *SpecCapacityRepository) UpdateSpotInfo(ctx context.Context, specName string, score int, price float64, instanceType string) error {
	now := time.Now()
	priceDecimal := decimal.NewFromFloat(price)
	return r.ds.db.WithContext(ctx).Model(&model.SpecCapacity{}).
		Where("spec_name = ?", specName).
		Updates(map[string]interface{}{
			"spot_score":         score,
			"spot_price":         priceDecimal,
			"instance_type":      instanceType,
			"last_spot_check_at": now,
			"updated_at":         now,
		}).Error
}
