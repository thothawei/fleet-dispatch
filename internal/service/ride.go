package service

import (
	"context"
	"fmt"
	"time"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/model"
	"line-fleet-dispatch/internal/repository"
	redisstore "line-fleet-dispatch/internal/redis"
)

// RideRequest 客戶叫車輸入
type RideRequest struct {
	LineUserID    string
	DisplayName   string
	PickupLat     float64
	PickupLng     float64
	PickupAddress string
}

// RideService 訂單業務邏輯
type RideService struct {
	customers *repository.CustomerRepository
	rides     *repository.RideRepository
	redis     *redisstore.Store
	dispatch  *DispatchService
}

func NewRideService(
	customers *repository.CustomerRepository,
	rides *repository.RideRepository,
	redis *redisstore.Store,
	dispatch *DispatchService,
) *RideService {
	return &RideService{
		customers: customers,
		rides:     rides,
		redis:     redis,
		dispatch:  dispatch,
	}
}

// CreateFromLocation 收到 LINE 位置訊息後建立訂單並觸發派單
func (s *RideService) CreateFromLocation(ctx context.Context, req RideRequest) (*model.Ride, error) {
	allowed, err := s.redis.AllowRateLimit(ctx, req.LineUserID, 5)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, fmt.Errorf("叫車太頻繁，請稍後再試")
	}

	customer, err := s.customers.FindOrCreateByLineUserID(req.LineUserID, req.DisplayName)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	ride := &model.Ride{
		CustomerID:    customer.ID,
		Status:        constants.RideStatusRequested,
		PickupPoint:   model.GeoPoint{Lat: req.PickupLat, Lng: req.PickupLng},
		PickupAddress: req.PickupAddress,
		RequestedAt:   now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := s.rides.Create(ride); err != nil {
		return nil, err
	}

	// 非同步派單
	go func(rideID int64) {
		_ = s.dispatch.Dispatch(context.Background(), rideID)
	}(ride.ID)

	return ride, nil
}

// CreateByCustomer 供已登入乘客（App）直接叫車：身分取自 JWT 的 customer_id。
// 下單前擋「同一乘客已有進行中訂單」，建立後沿用非同步派單。
func (s *RideService) CreateByCustomer(
	ctx context.Context,
	customerID int64,
	pickupLat, pickupLng float64,
	pickupAddress string,
) (*model.Ride, error) {
	if err := validatePickupCoords(pickupLat, pickupLng); err != nil {
		return nil, err
	}

	customer, err := s.customers.FindByID(customerID)
	if err != nil {
		return nil, err
	}

	allowed, err := s.redis.AllowRateLimit(ctx, customer.LineUserID, 5)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, ErrRateLimited
	}

	active, err := s.rides.FindActiveByCustomer(customerID)
	if err != nil {
		return nil, err
	}
	if active != nil {
		return nil, ErrActiveRideExists
	}

	now := time.Now()
	ride := &model.Ride{
		CustomerID:    customer.ID,
		Status:        constants.RideStatusRequested,
		PickupPoint:   model.GeoPoint{Lat: pickupLat, Lng: pickupLng},
		PickupAddress: pickupAddress,
		RequestedAt:   now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.rides.Create(ride); err != nil {
		return nil, err
	}

	// 非同步派單（與 CreateFromLocation 一致）
	go func(rideID int64) {
		_ = s.dispatch.Dispatch(context.Background(), rideID)
	}(ride.ID)

	return ride, nil
}

// RideQueryService 訂單查詢
type RideQueryService struct {
	tracks *repository.TrackRepository
}

func NewRideQueryService(tracks *repository.TrackRepository) *RideQueryService {
	return &RideQueryService{tracks: tracks}
}

func (s *RideQueryService) TrackGeoJSON(rideID int64) (string, error) {
	geojson, err := s.tracks.GeoJSON(rideID)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`{"type":"Feature","properties":{"ride_id":%d},"geometry":%s}`, rideID, geojson), nil
}
