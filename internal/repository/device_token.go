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
func (r *DeviceTokenRepository) Upsert(role string, subjectID int64, platform, token string) error {
	now := time.Now()
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
		Order("id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]notify.Device, 0, len(rows))
	for _, row := range rows {
		out = append(out, notify.Device{Platform: row.Platform, Token: row.Token})
	}
	return out, nil
}
