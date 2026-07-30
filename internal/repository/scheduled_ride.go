package repository

import (
	"errors"
	"time"

	"gorm.io/gorm"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/model"
)

// ScheduledRideRepository 預約行程讀寫。
type ScheduledRideRepository struct {
	db *gorm.DB
}

func NewScheduledRideRepository(db *gorm.DB) *ScheduledRideRepository {
	return &ScheduledRideRepository{db: db}
}

// Create 建立預約。
func (r *ScheduledRideRepository) Create(s *model.ScheduledRide) error {
	now := time.Now()
	if s.CreatedAt.IsZero() {
		s.CreatedAt = now
	}
	s.UpdatedAt = now
	return r.db.Create(s).Error
}

// GetOwned 取單筆並確認屬於該乘客；不存在或不是他的都回 (nil, nil)。
func (r *ScheduledRideRepository) GetOwned(id, customerID int64) (*model.ScheduledRide, error) {
	var row model.ScheduledRide
	err := r.db.Where("id = ? AND customer_id = ?", id, customerID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// GetByID 取單筆（排程器用，不做擁有者過濾）。
func (r *ScheduledRideRepository) GetByID(id int64) (*model.ScheduledRide, error) {
	var row model.ScheduledRide
	if err := r.db.First(&row, id).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// ListByCustomer 乘客的預約清單。
//
// upcomingOnly 為 true 時只回「還會發生的」（pending，且尚未過期太久）——
// 那是 App 首頁那張卡要的東西；false 則連已轉單／已取消一起回，供「預約紀錄」頁。
func (r *ScheduledRideRepository) ListByCustomer(customerID int64, upcomingOnly bool) ([]model.ScheduledRide, error) {
	q := r.db.Where("customer_id = ?", customerID)
	if upcomingOnly {
		q = q.Where("status = ?", constants.ScheduledRideStatusPending)
	}
	var rows []model.ScheduledRide
	// 近的在前：預約清單的閱讀順序是「下一趟是什麼時候」，不是「我什麼時候建的」。
	err := q.Order("scheduled_at ASC, id ASC").Limit(MaxListRows).Find(&rows).Error
	return rows, err
}

// CountPendingByCustomer 該乘客還沒轉單的預約數（服務層據此擋上限）。
func (r *ScheduledRideRepository) CountPendingByCustomer(customerID int64) (int64, error) {
	var n int64
	err := r.db.Model(&model.ScheduledRide{}).
		Where("customer_id = ? AND status = ?", customerID, constants.ScheduledRideStatusPending).
		Count(&n).Error
	return n, err
}

// FindDue 找出「該轉單了」的預約：仍在 pending，且約定時間已進入提前發動的界線內。
//
// 注意這裡**不做認領**，只是列出候選；認領由 ClaimForDispatch 以單一 UPDATE 完成。
// 分兩步是為了讓排程器能逐筆處理、逐筆記錯，而不是一個大交易全成全敗。
func (r *ScheduledRideRepository) FindDue(now time.Time, lead time.Duration, limit int) ([]model.ScheduledRide, error) {
	if limit <= 0 || limit > MaxListRows {
		limit = MaxListRows
	}
	var rows []model.ScheduledRide
	err := r.db.Where("status = ? AND scheduled_at <= ?",
		constants.ScheduledRideStatusPending, now.Add(lead)).
		Order("scheduled_at ASC, id ASC").Limit(limit).Find(&rows).Error
	return rows, err
}

// ClaimForDispatch 認領一筆待轉單的預約：把 attempt_count +1，並回傳是否認領成功。
//
// **這是整條排程唯一的併發防線**：條件帶 status='pending' AND attempt_count = 期望值，
// 兩個排程器（或重啟後重疊的那一輪）同時掃到同一筆時，只有一個的 RowsAffected 會是 1，
// 另一個拿到 0 就跳過。少了這一步，同一筆預約會建出兩張真訂單——
// 而訂單一旦派出去就收不回來了。
func (r *ScheduledRideRepository) ClaimForDispatch(id int64, expectAttempt int) (bool, error) {
	res := r.db.Model(&model.ScheduledRide{}).
		Where("id = ? AND status = ? AND attempt_count = ?",
			id, constants.ScheduledRideStatusPending, expectAttempt).
		Updates(map[string]interface{}{
			"attempt_count": expectAttempt + 1,
			"updated_at":    time.Now(),
		})
	return res.RowsAffected > 0, res.Error
}

// MarkDispatched 轉單成功：綁上真訂單 id 並封存狀態。
// 條件仍帶 status='pending'，避免與同時發生的取消互相覆蓋。
func (r *ScheduledRideRepository) MarkDispatched(id, rideID int64) (bool, error) {
	now := time.Now()
	res := r.db.Model(&model.ScheduledRide{}).
		Where("id = ? AND status = ?", id, constants.ScheduledRideStatusPending).
		Updates(map[string]interface{}{
			"status":        constants.ScheduledRideStatusDispatched,
			"ride_id":       rideID,
			"dispatched_at": now,
			"last_error":    "",
			"updated_at":    now,
		})
	return res.RowsAffected > 0, res.Error
}

// RecordAttemptError 記下這一輪轉單失敗的原因，維持 pending 等下一輪重試。
func (r *ScheduledRideRepository) RecordAttemptError(id int64, reason string) error {
	return r.db.Model(&model.ScheduledRide{}).
		Where("id = ? AND status = ?", id, constants.ScheduledRideStatusPending).
		Updates(map[string]interface{}{
			"last_error": truncateReason(reason),
			"updated_at": time.Now(),
		}).Error
}

// MarkFailed 重試用盡：標記永久失敗並留下原因給乘客看。
func (r *ScheduledRideRepository) MarkFailed(id int64, reason string) (bool, error) {
	res := r.db.Model(&model.ScheduledRide{}).
		Where("id = ? AND status = ?", id, constants.ScheduledRideStatusPending).
		Updates(map[string]interface{}{
			"status":     constants.ScheduledRideStatusFailed,
			"last_error": truncateReason(reason),
			"updated_at": time.Now(),
		})
	return res.RowsAffected > 0, res.Error
}

// Cancel 乘客取消：只有 pending 可取消，回傳是否真的取消到。
// 回 false 代表它已經被排程器轉單／已取消過——呼叫端要據此回 409 並讓 App 重讀，
// 不能宣稱取消成功（那張真訂單還在跑，乘客會以為沒車要來）。
func (r *ScheduledRideRepository) Cancel(id, customerID int64) (bool, error) {
	res := r.db.Model(&model.ScheduledRide{}).
		Where("id = ? AND customer_id = ? AND status = ?",
			id, customerID, constants.ScheduledRideStatusPending).
		Updates(map[string]interface{}{
			"status":     constants.ScheduledRideStatusCancelled,
			"updated_at": time.Now(),
		})
	return res.RowsAffected > 0, res.Error
}

// truncateReason 收斂錯誤訊息長度，避免上游帶回一整串 SQL／堆疊塞爆欄位。
func truncateReason(s string) string {
	const max = 200
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}
