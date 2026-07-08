package service

import (
	"context"

	"golang.org/x/crypto/bcrypt"

	"line-fleet-dispatch/internal/constants"
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

// Me 取司機個資與目前狀態（App 首頁顯示，取代信任本地狀態）
func (s *DriverRegistry) Me(driverID int64) (*model.Driver, error) {
	return s.drivers.FindByID(driverID)
}

// GoOnline 顯式上線：設為待命（Idle），重新進入派單池。
// 載客中（OnTrip）則維持原狀不降級，直接回傳目前狀態。
func (s *DriverRegistry) GoOnline(driverID int64) (*model.Driver, error) {
	d, err := s.drivers.FindByID(driverID)
	if err != nil {
		return nil, err
	}
	if d.Status == constants.DriverStatusOnTrip {
		return d, nil
	}
	if err := s.drivers.UpdateStatus(driverID, constants.DriverStatusIdle); err != nil {
		return nil, err
	}
	d.Status = constants.DriverStatusIdle
	return d, nil
}

// GoOffline 顯式下線：設為離線（Offline），乾淨移出派單池（dispatch 以 status 過濾）。
// 載客中不得下線，回 ErrDriverOnTrip，避免遺失進行中行程。
func (s *DriverRegistry) GoOffline(driverID int64) (*model.Driver, error) {
	d, err := s.drivers.FindByID(driverID)
	if err != nil {
		return nil, err
	}
	if d.Status == constants.DriverStatusOnTrip {
		return nil, ErrDriverOnTrip
	}
	if err := s.drivers.UpdateStatus(driverID, constants.DriverStatusOffline); err != nil {
		return nil, err
	}
	d.Status = constants.DriverStatusOffline
	return d, nil
}
