package service

import (
	"context"

	"golang.org/x/crypto/bcrypt"

	"line-fleet-dispatch/internal/model"
	"line-fleet-dispatch/internal/repository"
)

// DriverRegistry 司機註冊與登入
type DriverRegistry struct {
	drivers *repository.DriverRepository
}

func NewDriverRegistry(drivers *repository.DriverRepository) *DriverRegistry {
	return &DriverRegistry{drivers: drivers}
}

// Register 註冊或更新司機，並設定登入密碼（bcrypt）
func (s *DriverRegistry) Register(ctx context.Context, lineUserID, name, password string) (*model.Driver, error) {
	driver, err := s.drivers.FindOrCreate(lineUserID, name)
	if err != nil {
		return nil, err
	}
	if password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return nil, err
		}
		if err := s.drivers.SetPassword(driver.ID, string(hash)); err != nil {
			return nil, err
		}
		driver.PasswordHash = string(hash)
	}
	return driver, nil
}

// Login 以 line_user_id + 密碼驗證，成功回傳司機
func (s *DriverRegistry) Login(ctx context.Context, lineUserID, password string) (*model.Driver, error) {
	driver, err := s.drivers.FindByLineUserID(lineUserID)
	if err != nil {
		return nil, ErrInvalidCredentials
	}
	if driver.PasswordHash == "" ||
		bcrypt.CompareHashAndPassword([]byte(driver.PasswordHash), []byte(password)) != nil {
		return nil, ErrInvalidCredentials
	}
	return driver, nil
}
