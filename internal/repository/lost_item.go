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

// LostItemAdminRow 後台協尋單總覽列（含司機/乘客姓名，供 admin 列表呈現）。
type LostItemAdminRow struct {
	ID           int64      `json:"id"`
	RideID       int64      `json:"ride_id"`
	CustomerID   int64      `json:"customer_id"`
	CustomerName string     `json:"customer_name"`
	DriverID     int64      `json:"driver_id"`
	DriverName   string     `json:"driver_name"`
	Description  string     `json:"description"`
	FeeCents     int64      `json:"fee_cents"`
	FeeBps       int        `json:"fee_bps"`
	Status       string     `json:"status"`
	PaidAt       *time.Time `json:"paid_at"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// ListAll 後台總覽：全部協尋單（status 空字串為全部），新的在前，上限 MaxListRows。
func (r *LostItemRepository) ListAll(status string) ([]LostItemAdminRow, error) {
	var rows []LostItemAdminRow
	err := r.db.Raw(`
		SELECT li.id, li.ride_id,
		       li.customer_id, c.name AS customer_name,
		       li.driver_id, d.name AS driver_name,
		       li.description, li.fee_cents, li.fee_bps,
		       li.status, li.paid_at, li.created_at, li.updated_at
		FROM lost_item_requests li
		JOIN customers c ON c.id = li.customer_id
		JOIN drivers d ON d.id = li.driver_id
		WHERE (? = '' OR li.status = ?)
		ORDER BY li.id DESC
		LIMIT ?
	`, status, status, MaxListRows).Scan(&rows).Error
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
