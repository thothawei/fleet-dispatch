package repository

import (
	"time"

	"gorm.io/gorm"

	"line-fleet-dispatch/internal/model"
)

// RideMessageRepository 行程內對話訊息讀寫。
type RideMessageRepository struct {
	db *gorm.DB
}

func NewRideMessageRepository(db *gorm.DB) *RideMessageRepository {
	return &RideMessageRepository{db: db}
}

// Create 寫入一則訊息。
func (r *RideMessageRepository) Create(m *model.RideMessage) error {
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now()
	}
	return r.db.Create(m).Error
}

// FindByClientMsgID 查同一趟行程、同一位發話者、同一個冪等鍵的既有訊息；
// 沒有時回 (nil, nil)。**去重範圍刻意不含全表**——鍵由客戶端產生，
// 跨使用者撞號不該讓後方那位發不出訊息。
func (r *RideMessageRepository) FindByClientMsgID(rideID int64, senderRole string, senderID int64, clientMsgID string) (*model.RideMessage, error) {
	if clientMsgID == "" {
		return nil, nil
	}
	var m model.RideMessage
	err := r.db.Where(
		"ride_id = ? AND sender_role = ? AND sender_id = ? AND client_msg_id = ?",
		rideID, senderRole, senderID, clientMsgID,
	).First(&m).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// ListByRide 依 id 正序回傳該行程的訊息；afterID > 0 時只取其後的訊息（增量補讀）。
// limit <= 0 或超過 MaxListRows 時以 MaxListRows 為準。
func (r *RideMessageRepository) ListByRide(rideID, afterID int64, limit int) ([]model.RideMessage, error) {
	if limit <= 0 || limit > MaxListRows {
		limit = MaxListRows
	}
	q := r.db.Where("ride_id = ?", rideID)
	if afterID > 0 {
		q = q.Where("id > ?", afterID)
	}
	var rows []model.RideMessage
	err := q.Order("id ASC").Limit(limit).Find(&rows).Error
	return rows, err
}
