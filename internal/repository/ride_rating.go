package repository

import (
	"time"

	"gorm.io/gorm"

	"line-fleet-dispatch/internal/model"
)

// RideRatingRepository 乘客對司機的行程評分讀寫（B5）。
type RideRatingRepository struct {
	db *gorm.DB
}

func NewRideRatingRepository(db *gorm.DB) *RideRatingRepository {
	return &RideRatingRepository{db: db}
}

// Create 寫入評分。同一趟重複評分會撞唯一索引 uq_ride_ratings_ride_id
// （服務層先行檢查，此為競態下的最後防線）。
func (r *RideRatingRepository) Create(rating *model.RideRating) error {
	if rating.CreatedAt.IsZero() {
		rating.CreatedAt = time.Now()
	}
	return r.db.Create(rating).Error
}

// FindByRide 取該行程的評分；未評分回 (nil, nil)——「還沒評」不是錯誤，
// 它是完成卡決定顯示星星還是評分按鈕的正常輸入。
func (r *RideRatingRepository) FindByRide(rideID int64) (*model.RideRating, error) {
	var rating model.RideRating
	err := r.db.Where("ride_id = ?", rideID).First(&rating).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rating, nil
}

// DriverRatingSummary 司機的評分彙總（平均分與則數）。
type DriverRatingSummary struct {
	// Average 平均分，未經四捨五入（呈現端自行決定小數位）。無評分時為 0。
	Average float64 `json:"rating_avg"`
	Count   int64   `json:"rating_count"`
}

// SummaryByDriver 司機平均分與評分則數。**無評分時回 (0, 0) 而非錯誤**——
// 新司機沒有評分是常態，不該讓司機主頁查詢失敗。
func (r *RideRatingRepository) SummaryByDriver(driverID int64) (DriverRatingSummary, error) {
	var row struct {
		Avg   *float64
		Count int64
	}
	err := r.db.Model(&model.RideRating{}).
		Select("AVG(score) AS avg", "COUNT(*) AS count").
		Where("driver_id = ?", driverID).
		Scan(&row).Error
	if err != nil {
		return DriverRatingSummary{}, err
	}
	summary := DriverRatingSummary{Count: row.Count}
	if row.Avg != nil {
		summary.Average = *row.Avg
	}
	return summary, nil
}
