package service

import (
	"context"

	"line-fleet-dispatch/internal/model"
	"line-fleet-dispatch/internal/repository"
)

// DriverRegistry 司機註冊
type DriverRegistry struct {
	drivers *repository.DriverRepository
}

func NewDriverRegistry(drivers *repository.DriverRepository) *DriverRegistry {
	return &DriverRegistry{drivers: drivers}
}

func (s *DriverRegistry) Register(ctx context.Context, lineUserID, name string) (*model.Driver, error) {
	return s.drivers.FindOrCreate(lineUserID, name)
}
