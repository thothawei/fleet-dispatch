package repository

import (
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"line-fleet-dispatch/internal/model"
)

// SavedPlaceRepository 乘客常用地點讀寫。
type SavedPlaceRepository struct {
	db *gorm.DB
}

func NewSavedPlaceRepository(db *gorm.DB) *SavedPlaceRepository {
	return &SavedPlaceRepository{db: db}
}

// ListByCustomer 該乘客的全部常用地點。
//
// 排序刻意是「住家 → 公司 → 其他（新的在前）」而不是純 id 序：這份清單直接就是 App 上
// 那排快捷鈕的順序，住家／公司永遠該在最前面，不該因為後來新增了自訂地點就被擠掉。
func (r *SavedPlaceRepository) ListByCustomer(customerID int64) ([]model.CustomerSavedPlace, error) {
	var rows []model.CustomerSavedPlace
	err := r.db.Where("customer_id = ?", customerID).
		Order(`CASE kind WHEN 'home' THEN 0 WHEN 'work' THEN 1 ELSE 2 END, id DESC`).
		Limit(MaxListRows).Find(&rows).Error
	return rows, err
}

// GetOwned 取單筆並確認屬於該乘客；不存在或不是他的都回 (nil, nil)。
//
// 「不是你的」與「不存在」故意回同一種結果——分開回會讓人用 404／403 的差異
// 探測出別人有幾筆地點。
func (r *SavedPlaceRepository) GetOwned(id, customerID int64) (*model.CustomerSavedPlace, error) {
	var row model.CustomerSavedPlace
	err := r.db.Where("id = ? AND customer_id = ?", id, customerID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// FindByKind 取該乘客某個插槽種類的地點（home／work）；沒有則回 (nil, nil)。
func (r *SavedPlaceRepository) FindByKind(customerID int64, kind string) (*model.CustomerSavedPlace, error) {
	var row model.CustomerSavedPlace
	err := r.db.Where("customer_id = ? AND kind = ?", customerID, kind).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// Create 新增一筆常用地點。撞 uq_saved_place_customer_kind 時回 ErrDuplicateSlot——
// 服務層會先查再決定新增或覆蓋，這條是競態下的最後防線。
func (r *SavedPlaceRepository) Create(place *model.CustomerSavedPlace) error {
	now := time.Now()
	if place.CreatedAt.IsZero() {
		place.CreatedAt = now
	}
	place.UpdatedAt = now
	err := r.db.Create(place).Error
	if isSavedPlaceSlotConflict(err) {
		return ErrDuplicateSlot
	}
	return err
}

// Update 更新既有地點的顯示名稱、地址與座標（kind 不可改——
// 要把住家改成公司，語意上是刪掉重建，不是改一個欄位）。
func (r *SavedPlaceRepository) Update(id, customerID int64, label, address string, point model.GeoPoint) (bool, error) {
	res := r.db.Model(&model.CustomerSavedPlace{}).
		Where("id = ? AND customer_id = ?", id, customerID).
		Updates(map[string]interface{}{
			"label":      label,
			"address":    address,
			"point":      point,
			"updated_at": time.Now(),
		})
	return res.RowsAffected > 0, res.Error
}

// Delete 刪除自己的地點；回傳是否真的刪到（不是自己的就刪不到）。
func (r *SavedPlaceRepository) Delete(id, customerID int64) (bool, error) {
	res := r.db.Where("id = ? AND customer_id = ?", id, customerID).
		Delete(&model.CustomerSavedPlace{})
	return res.RowsAffected > 0, res.Error
}

// ErrDuplicateSlot 同一位乘客的 home／work 插槽已被佔用。
var ErrDuplicateSlot = errors.New("該種類的常用地點已存在")

// isSavedPlaceSlotConflict 判斷是否撞到 uq_saved_place_customer_kind。
// 只認這一個約束名——其他唯一鍵衝突不該被翻譯成「插槽已存在」而蓋掉真正的錯誤。
func isSavedPlaceSlotConflict(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == pgUniqueViolation &&
		pgErr.ConstraintName == "uq_saved_place_customer_kind"
}

// CountByCustomer 該乘客已存的地點數（服務層據此擋上限）。
func (r *SavedPlaceRepository) CountByCustomer(customerID int64) (int64, error) {
	var n int64
	err := r.db.Model(&model.CustomerSavedPlace{}).
		Where("customer_id = ?", customerID).Count(&n).Error
	return n, err
}
