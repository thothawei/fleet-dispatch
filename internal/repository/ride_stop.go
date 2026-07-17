package repository

import (
	"time"

	"gorm.io/gorm"

	"line-fleet-dispatch/internal/model"
)

// RideStopRepository 多停靠點（N1）讀寫。
type RideStopRepository struct {
	db *gorm.DB
}

func NewRideStopRepository(db *gorm.DB) *RideStopRepository {
	return &RideStopRepository{db: db}
}

// StopRow 落庫用的單一停靠點（座標分開傳，因為要走 ST_MakePoint）。
type StopRow struct {
	Seq            int
	Kind           string
	Lat, Lng       float64
	Address        string
	PassengerLabel string
}

// CreateForRide 批次寫入某趟的停靠點。
// 呼叫端（RideService.CreateByCustomer）負責先驗證配對與上限。
func (r *RideStopRepository) CreateForRide(rideID int64, rows []StopRow) error {
	if len(rows) == 0 {
		return nil
	}
	now := time.Now()
	return r.db.Transaction(func(tx *gorm.DB) error {
		for _, s := range rows {
			// 座標走 ST_MakePoint(lng, lat)——與 rides 的寫法一致（經度在前）。
			err := tx.Exec(`
				INSERT INTO ride_stops (ride_id, seq, kind, point, address, passenger_label, created_at, updated_at)
				VALUES (?, ?, ?, ST_SetSRID(ST_MakePoint(?, ?), 4326)::geography, ?, ?, ?, ?)
			`, rideID, s.Seq, s.Kind, s.Lng, s.Lat, s.Address, s.PassengerLabel, now, now).Error
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// ListByRide 依 seq 由小到大回傳整趟停靠點；單點訂單回空 slice（非錯誤）。
func (r *RideStopRepository) ListByRide(rideID int64) ([]model.RideStop, error) {
	var stops []model.RideStop
	err := r.db.Where("ride_id = ?", rideID).Order("seq ASC").Find(&stops).Error
	return stops, err
}
