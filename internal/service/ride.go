package service

import (
	"context"
	"fmt"
	"time"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/events"
	"line-fleet-dispatch/internal/model"
	redisstore "line-fleet-dispatch/internal/redis"
	"line-fleet-dispatch/internal/repository"
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
	audit     rideAuditor
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

// SetRideEvents 注入訂單狀態審計寫入；可選。
func (s *RideService) SetRideEvents(repo *repository.RideEventRepository) {
	s.audit = rideAuditor{events: repo}
}

// CreateFromLocation 收到 LINE 位置訊息後建立訂單並觸發派單
func (s *RideService) CreateFromLocation(ctx context.Context, req RideRequest) (*model.Ride, error) {
	allowed, err := s.redis.AllowRateLimit(ctx, req.LineUserID, s.rateLimitPerMin())
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
	s.audit.record(ride.ID, nil, constants.RideStatusRequested,
		events.TypeRideRequested, events.ActorCustomer, idPtr(customer.ID), "line")

	// 非同步派單
	go func(rideID int64) {
		_ = s.dispatch.Dispatch(context.Background(), rideID)
	}(ride.ID)

	return ride, nil
}

func (s *RideService) rateLimitPerMin() int {
	if s.dispatch == nil || s.dispatch.settings == nil {
		return 5
	}
	_, _, _, _, rate := s.dispatch.settings.Snapshot()
	return rate
}

// CustomerCreateRequest 乘客 App 下單輸入（含選填目的地）。
type CustomerCreateRequest struct {
	PickupLat, PickupLng float64
	PickupAddress        string
	DropoffAddress       string
	DropoffLat, DropoffLng *float64
}

// CreateByCustomer 供已登入乘客（App）直接叫車：身分取自 JWT 的 customer_id。
// 下單前擋「同一乘客已有進行中訂單」，建立後沿用非同步派單。
func (s *RideService) CreateByCustomer(
	ctx context.Context,
	customerID int64,
	req CustomerCreateRequest,
) (*model.Ride, error) {
	if err := validatePickupCoords(req.PickupLat, req.PickupLng); err != nil {
		return nil, err
	}
	if err := validateOptionalDropoffCoords(req.DropoffLat, req.DropoffLng); err != nil {
		return nil, err
	}

	customer, err := s.customers.FindByID(customerID)
	if err != nil {
		return nil, err
	}

	allowed, err := s.redis.AllowRateLimit(ctx, customer.LineUserID, s.rateLimitPerMin())
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
		CustomerID:     customer.ID,
		Status:         constants.RideStatusRequested,
		PickupPoint:    model.GeoPoint{Lat: req.PickupLat, Lng: req.PickupLng},
		PickupAddress:  req.PickupAddress,
		DropoffAddress: req.DropoffAddress,
		RequestedAt:    now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if req.DropoffLat != nil && req.DropoffLng != nil {
		ride.DropoffPoint = &model.GeoPoint{Lat: *req.DropoffLat, Lng: *req.DropoffLng}
	}
	if err := s.rides.Create(ride); err != nil {
		return nil, err
	}
	s.audit.record(ride.ID, nil, constants.RideStatusRequested,
		events.TypeRideRequested, events.ActorCustomer, idPtr(customer.ID), "app")

	// 非同步派單（與 CreateFromLocation 一致）
	go func(rideID int64) {
		_ = s.dispatch.Dispatch(context.Background(), rideID)
	}(ride.ID)

	return ride, nil
}

// RideQueryService 訂單查詢
type RideQueryService struct {
	tracks *repository.TrackRepository
	rides  *repository.RideRepository
}

func NewRideQueryService(tracks *repository.TrackRepository, rides *repository.RideRepository) *RideQueryService {
	return &RideQueryService{tracks: tracks, rides: rides}
}

func (s *RideQueryService) TrackGeoJSON(rideID int64) (string, error) {
	geojson, err := s.tracks.GeoJSON(rideID)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`{"type":"Feature","properties":{"ride_id":%d},"geometry":%s}`, rideID, geojson), nil
}

// GetActiveRideByCustomer 找乘客目前進行中的訂單（App 啟動/重連取 ride_id 用），無進行中訂單回 (nil, nil)
func (s *RideQueryService) GetActiveRideByCustomer(customerID int64) (*model.Ride, error) {
	return s.rides.FindActiveByCustomer(customerID)
}

// GetActiveRideByDriver 找司機目前進行中的訂單（已接/載客中），供 App 中途重啟恢復行程；無則回 (nil, nil)
func (s *RideQueryService) GetActiveRideByDriver(driverID int64) (*model.Ride, error) {
	return s.rides.FindActiveByDriver(driverID)
}

// AuthorizeTrackAccess 授權軌跡端點的多角色存取：admin 全放行、本趟乘客、被指派司機皆可，
// 其餘回 ErrForbidden；訂單不存在回 ErrNotFound。
func (s *RideQueryService) AuthorizeTrackAccess(role string, subjectID, rideID int64) error {
	ride, err := s.rides.GetByID(rideID)
	if err != nil {
		return ErrNotFound
	}
	switch role {
	case "admin":
		return nil
	case "customer":
		if ride.CustomerID == subjectID {
			return nil
		}
	case "driver":
		if ride.DriverID != nil && *ride.DriverID == subjectID {
			return nil
		}
	}
	return ErrForbidden
}

// GetRideForCustomer 乘客查詢單一訂單，附 owner 檢查：訂單不存在回 ErrNotFound，非本人訂單回 ErrForbidden
func (s *RideQueryService) GetRideForCustomer(customerID, rideID int64) (*model.Ride, error) {
	ride, err := s.rides.GetByID(rideID)
	if err != nil {
		return nil, ErrNotFound
	}
	if ride.CustomerID != customerID {
		return nil, ErrForbidden
	}
	return ride, nil
}
