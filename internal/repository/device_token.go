package repository

import (
	"errors"
	"time"

	"gorm.io/gorm"

	"line-fleet-dispatch/internal/model"
	"line-fleet-dispatch/internal/notify"
)

type DeviceTokenRepository struct {
	db *gorm.DB
}

func NewDeviceTokenRepository(db *gorm.DB) *DeviceTokenRepository {
	return &DeviceTokenRepository{db: db}
}

// Upsert 同一 role+subject+token 更新 platform／時間；否則新增。
//
// **一個 token 同時只能屬於一位主體**：註冊前先清掉「同一支 token 掛在別人身上」的舊列。
// FCM/APNs token 是「這台裝置上的這個 App」的識別，換人登入時它不會變——
// 而 App 在 session 失效（401）那條路徑**刻意不呼叫註銷**（那支 API 只會再回一次 401），
// 所以舊使用者的那一列會留著。不清的話，下一位在這台裝置登入的人會收到**前一位**的
// 行程通知與**對話內容預覽**（推播本文帶訊息文字）——這是隱私外洩，不只是雜訊。
func (r *DeviceTokenRepository) Upsert(role string, subjectID int64, platform, token string) error {
	now := time.Now()
	if err := r.db.Where("token = ? AND NOT (role = ? AND subject_id = ?)", token, role, subjectID).
		Delete(&model.DeviceToken{}).Error; err != nil {
		return err
	}
	var existing model.DeviceToken
	err := r.db.Where("role = ? AND subject_id = ? AND token = ?", role, subjectID, token).
		First(&existing).Error
	if err == nil {
		return r.db.Model(&existing).Updates(map[string]interface{}{
			"platform":   platform,
			"updated_at": now,
		}).Error
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	row := &model.DeviceToken{
		Role:      role,
		SubjectID: subjectID,
		Platform:  platform,
		Token:     token,
		CreatedAt: now,
		UpdatedAt: now,
	}
	return r.db.Create(row).Error
}

// Delete 依 role+subject+token 註銷；token 為空則該主體全清。
func (r *DeviceTokenRepository) Delete(role string, subjectID int64, token string) (int64, error) {
	q := r.db.Where("role = ? AND subject_id = ?", role, subjectID)
	if token != "" {
		q = q.Where("token = ?", token)
	}
	res := q.Delete(&model.DeviceToken{})
	return res.RowsAffected, res.Error
}

func (r *DeviceTokenRepository) ListBySubject(role string, subjectID int64) ([]notify.Device, error) {
	var rows []model.DeviceToken
	if err := r.db.Where("role = ? AND subject_id = ?", role, subjectID).
		Order("id ASC").Limit(MaxListRows).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]notify.Device, 0, len(rows))
	for _, row := range rows {
		out = append(out, notify.Device{Platform: row.Platform, Token: row.Token})
	}
	return out, nil
}
