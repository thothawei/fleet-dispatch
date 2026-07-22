package service

import (
	"context"
	"errors"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

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

// SetVehicle 設定司機車輛資訊（O2）：車種須為白名單 code，車牌正規化後做寬鬆格式檢查。
// 兩者皆必填——留空等同「未設定」，會讓司機繞過 O3 的接單 gate。
// 車牌與其他司機重複時回 repository.ErrPlateTaken。
func (s *DriverRegistry) SetVehicle(driverID int64, vehicleType, plateNumber string) (*model.Driver, error) {
	if !constants.IsValidVehicleType(vehicleType) {
		return nil, ErrInvalidVehicleType
	}
	plate := constants.NormalizePlateNumber(plateNumber)
	if !constants.IsValidPlateNumber(plate) {
		return nil, ErrInvalidPlateNumber
	}
	d, err := s.drivers.FindByID(driverID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if err := s.drivers.UpdateVehicle(driverID, vehicleType, plate); err != nil {
		return nil, err
	}
	d.VehicleType = vehicleType
	d.PlateNumber = plate
	// O5：填/改車輛回 pending 待審核（repo 已原子寫入，這裡同步回傳物件）。
	d.VehicleReviewStatus = constants.VehicleReviewPending
	d.VehicleReviewNote = ""
	return d, nil
}

// SetPhone 設定司機聯絡電話（O7）。乘客在「司機前往上車點」階段看得到這個號碼並可直接撥打，
// 但在此之前 drivers.phone **沒有任何寫入路徑**——註冊不收、車輛設定也不收，
// 導致乘客端的撥號按鈕實質永遠不出現。
// 刻意獨立於 SetVehicle：改電話不該觸發車輛重新審核（O5），否則司機換號碼就被鎖出派單池。
func (s *DriverRegistry) SetPhone(driverID int64, phone string) (*model.Driver, error) {
	normalized := constants.NormalizePhone(phone)
	if !constants.IsValidPhone(normalized) {
		return nil, ErrInvalidPhone
	}
	d, err := s.drivers.FindByID(driverID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if err := s.drivers.UpdatePhone(driverID, normalized); err != nil {
		return nil, err
	}
	d.Phone = normalized
	return d, nil
}

// GoOnline 顯式上線：設為待命（Idle），重新進入派單池。
// 載客中（OnTrip）則維持原狀不降級；已停用（Disabled）回 ErrDriverDisabled。
func (s *DriverRegistry) GoOnline(driverID int64) (*model.Driver, error) {
	d, err := s.drivers.FindByID(driverID)
	if err != nil {
		return nil, err
	}
	if d.Status == constants.DriverStatusDisabled {
		return nil, ErrDriverDisabled
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
