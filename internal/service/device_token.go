package service

import (
	"errors"
	"strings"

	"line-fleet-dispatch/internal/notify"
	"line-fleet-dispatch/internal/repository"
)

var (
	ErrInvalidDeviceToken = errors.New("裝置 token 參數錯誤")
)

// DeviceTokenService 註冊／註銷 App 推播 token。
type DeviceTokenService struct {
	tokens *repository.DeviceTokenRepository
}

func NewDeviceTokenService(tokens *repository.DeviceTokenRepository) *DeviceTokenService {
	return &DeviceTokenService{tokens: tokens}
}

func (s *DeviceTokenService) Register(role string, subjectID int64, platform, token string) error {
	platform = strings.ToLower(strings.TrimSpace(platform))
	token = strings.TrimSpace(token)
	if token == "" {
		return ErrInvalidDeviceToken
	}
	if platform != notify.PlatformFCM && platform != notify.PlatformAPNs {
		return ErrInvalidDeviceToken
	}
	if role != notify.RoleDriver && role != notify.RoleCustomer {
		return ErrInvalidDeviceToken
	}
	return s.tokens.Upsert(role, subjectID, platform, token)
}

func (s *DeviceTokenService) Unregister(role string, subjectID int64, token string) error {
	token = strings.TrimSpace(token)
	if role != notify.RoleDriver && role != notify.RoleCustomer {
		return ErrInvalidDeviceToken
	}
	_, err := s.tokens.Delete(role, subjectID, token)
	return err
}
