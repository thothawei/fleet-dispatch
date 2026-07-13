package repository

import (
	"time"

	"gorm.io/gorm"

	"line-fleet-dispatch/internal/model"
)

type RideEventRepository struct {
	db *gorm.DB
}

func NewRideEventRepository(db *gorm.DB) *RideEventRepository {
	return &RideEventRepository{db: db}
}

// Append 寫入一筆審計事件。
func (r *RideEventRepository) Append(e *model.RideEvent) error {
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}
	return r.db.Create(e).Error
}

// ListByRideID 依時間正序回傳該訂單全部審計事件。
func (r *RideEventRepository) ListByRideID(rideID int64) ([]model.RideEvent, error) {
	var rows []model.RideEvent
	err := r.db.Where("ride_id = ?", rideID).Order("created_at ASC, id ASC").Limit(MaxListRows).Find(&rows).Error
	return rows, err
}
