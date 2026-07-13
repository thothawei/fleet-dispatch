package repository

import (
	"time"

	"gorm.io/gorm"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/model"
)

// LostItemRepository 遺失物協尋單讀寫。
type LostItemRepository struct {
	db *gorm.DB
}

func NewLostItemRepository(db *gorm.DB) *LostItemRepository {
	return &LostItemRepository{db: db}
}

// Create 建立協尋單。同行程已有未結案協尋單時會撞部分唯一索引
// uq_lost_item_ride_active（服務層先行檢查，此為競態下的最後防線）。
func (r *LostItemRepository) Create(item *model.LostItemRequest) error {
	now := time.Now()
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	return r.db.Create(item).Error
}

// GetByID 取單筆協尋單。
func (r *LostItemRepository) GetByID(id int64) (*model.LostItemRequest, error) {
	var item model.LostItemRequest
	if err := r.db.First(&item, id).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// FindActiveByRide 取該行程「未結案」（非 returned/closed）的協尋單；無則回 (nil, nil)。
func (r *LostItemRepository) FindActiveByRide(rideID int64) (*model.LostItemRequest, error) {
	var item model.LostItemRequest
	err := r.db.Where("ride_id = ? AND status NOT IN ?", rideID,
		[]string{constants.LostItemStatusReturned, constants.LostItemStatusClosed}).
		Order("id DESC").First(&item).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// LatestByRide 取該行程最新一張協尋單（含已結案，供 UI 呈現歷史）；無則回 (nil, nil)。
func (r *LostItemRepository) LatestByRide(rideID int64) (*model.LostItemRequest, error) {
	var item model.LostItemRequest
	err := r.db.Where("ride_id = ?", rideID).Order("id DESC").First(&item).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// ListByDriver 司機的協尋單列表；statuses 非空時篩選狀態。新的在前。
func (r *LostItemRepository) ListByDriver(driverID int64, statuses []string) ([]model.LostItemRequest, error) {
	q := r.db.Where("driver_id = ?", driverID)
	if len(statuses) > 0 {
		q = q.Where("status IN ?", statuses)
	}
	var rows []model.LostItemRequest
	err := q.Order("id DESC").Limit(MaxListRows).Find(&rows).Error
	return rows, err
}

// ListByCustomer 乘客的協尋單列表；statuses 非空時篩選狀態。新的在前。
func (r *LostItemRepository) ListByCustomer(customerID int64, statuses []string) ([]model.LostItemRequest, error) {
	q := r.db.Where("customer_id = ?", customerID)
	if len(statuses) > 0 {
		q = q.Where("status IN ?", statuses)
	}
	var rows []model.LostItemRequest
	err := q.Order("id DESC").Limit(MaxListRows).Find(&rows).Error
	return rows, err
}

// TransitionStatus 守門式狀態轉換：僅當現況在 from 之中才改為 to，回傳是否有改到。
// markPaid 為 true 時一併寫入 paid_at（乘客支付處理費當下）。
func (r *LostItemRepository) TransitionStatus(id int64, from []string, to string, markPaid bool) (bool, error) {
	updates := map[string]interface{}{
		"status":     to,
		"updated_at": time.Now(),
	}
	if markPaid {
		updates["paid_at"] = time.Now()
	}
	res := r.db.Model(&model.LostItemRequest{}).
		Where("id = ? AND status IN ?", id, from).
		Updates(updates)
	return res.RowsAffected > 0, res.Error
}
