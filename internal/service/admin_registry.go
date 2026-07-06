package service

import (
	"context"
	"time"

	"golang.org/x/crypto/bcrypt"

	"line-fleet-dispatch/internal/model"
)

// adminStore 抽象出 AdminRegistry 需要的 repository 行為（方便測試替身）
type adminStore interface {
	FindByEmail(email string) (*model.Admin, error)
	Create(a *model.Admin) error
	CountAll() (int64, error)
}

// AdminRegistry 後台管理員登入與種子建立
type AdminRegistry struct {
	admins adminStore
}

func NewAdminRegistry(admins adminStore) *AdminRegistry {
	return &AdminRegistry{admins: admins}
}

// Login email + 密碼驗證
func (s *AdminRegistry) Login(ctx context.Context, email, password string) (*model.Admin, error) {
	admin, err := s.admins.FindByEmail(email)
	if err != nil {
		return nil, ErrInvalidCredentials
	}
	if admin.PasswordHash == "" ||
		bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(password)) != nil {
		return nil, ErrInvalidCredentials
	}
	return admin, nil
}

// EnsureSeed 若系統尚無任何管理員且提供了 email/password，建立一個種子管理員
func (s *AdminRegistry) EnsureSeed(ctx context.Context, email, password string) error {
	if email == "" || password == "" {
		return nil
	}
	n, err := s.admins.CountAll()
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	now := time.Now()
	return s.admins.Create(&model.Admin{
		Email:        email,
		PasswordHash: string(hash),
		Name:         "系統管理員",
		CreatedAt:    now,
		UpdatedAt:    now,
	})
}
