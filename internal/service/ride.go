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

// CustomerCreateRequest 乘客 App 下單輸入（含選填目的地與選填指定車種）。
type CustomerCreateRequest struct {
	PickupLat, PickupLng   float64
	PickupAddress          string
	DropoffAddress         string
	DropoffLat, DropoffLng *float64
	// RequiredVehicleType 選填（P2）：'' ＝不指定，維持現行行為（任何車種都可派）。
	RequiredVehicleType string
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
	// 指定車種為選填；有給就必須是白名單 code（DB CHECK 是最後防線，但錯誤要在這裡
	// 變成 400 而不是讓 INSERT 撞 CHECK 回 500）。
	if req.RequiredVehicleType != "" && !constants.IsValidVehicleType(req.RequiredVehicleType) {
		return nil, ErrInvalidVehicleType
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
		CustomerID:          customer.ID,
		Status:              constants.RideStatusRequested,
		PickupPoint:         model.GeoPoint{Lat: req.PickupLat, Lng: req.PickupLng},
		PickupAddress:       req.PickupAddress,
		DropoffAddress:      req.DropoffAddress,
		RequiredVehicleType: req.RequiredVehicleType,
		RequestedAt:         now,
		CreatedAt:           now,
		UpdatedAt:           now,
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
	tracks  *repository.TrackRepository
	rides   *repository.RideRepository
	drivers *repository.DriverRepository
}

func NewRideQueryService(tracks *repository.TrackRepository, rides *repository.RideRepository) *RideQueryService {
	return &RideQueryService{tracks: tracks, rides: rides}
}

// SetDrivers 注入司機 repo（供乘客端查看司機姓名／電話，O4／O7）；可選——
// 未注入時查詢仍可用，只是不帶司機聯絡資訊。
func (s *RideQueryService) SetDrivers(drivers *repository.DriverRepository) {
	s.drivers = drivers
}

// CustomerRideView 乘客視角的訂單：ride 全欄位（含 O7 車輛快照）＋司機姓名／電話。
//
// 內嵌 *model.Ride 讓 JSON 攤平，既有欄位一個不少——App 讀到的形狀只多不變。
//
// **車種／車牌讀 ride 的快照**（司機換車後歷史行程不變），
// **電話讀 drivers 的即時值**（司機換號碼後，乘客要撥得通的是新號碼）。
// 兩者的正確來源不同，不要「順手」統一。
type CustomerRideView struct {
	*model.Ride
	DriverName string `json:"driver_name,omitempty"`
	// DriverPhone 明碼（O7 拍板）。**僅該趟乘客可見**——本 struct 只由
	// 「乘客查自己訂單」的路徑產生，絕不可用於任何列表或對其他角色的回應。
	DriverPhone string `json:"driver_phone,omitempty"`
}

// withDriverContact 補上司機姓名／電話；未接單或未注入 drivers 時原樣回傳。
func (s *RideQueryService) withDriverContact(ride *model.Ride) *CustomerRideView {
	if ride == nil {
		return nil
	}
	view := &CustomerRideView{Ride: ride}
	if ride.DriverID == nil || s.drivers == nil {
		return view
	}
	d, err := s.drivers.FindByID(*ride.DriverID)
	if err != nil {
		return view // 司機查不到不該讓整筆訂單查詢失敗
	}
	view.DriverName = d.Name
	view.DriverPhone = d.Phone
	return view
}

func (s *RideQueryService) TrackGeoJSON(rideID int64) (string, error) {
	geojson, err := s.tracks.GeoJSON(rideID)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`{"type":"Feature","properties":{"ride_id":%d},"geometry":%s}`, rideID, geojson), nil
}

// GetActiveRideByCustomer 找乘客目前進行中的訂單（App 啟動/重連取 ride_id 用），無進行中訂單回 (nil, nil)
func (s *RideQueryService) GetActiveRideByCustomer(customerID int64) (*CustomerRideView, error) {
	ride, err := s.rides.FindActiveByCustomer(customerID)
	if err != nil || ride == nil {
		return nil, err
	}
	return s.withDriverContact(ride), nil
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
func (s *RideQueryService) GetRideForCustomer(customerID, rideID int64) (*CustomerRideView, error) {
	ride, err := s.rides.GetByID(rideID)
	if err != nil {
		return nil, ErrNotFound
	}
	// 授權必須在補司機聯絡資訊**之前**——這是司機電話明碼外流與否的分界。
	// 非本人訂單一律 403，拿不到任何司機資訊。
	if ride.CustomerID != customerID {
		return nil, ErrForbidden
	}
	// 這條路徑也是遺失物協尋回頭查「當時搭哪台車、怎麼聯絡司機」的來源（O7），
	// 沒有時間限制：行程完成很久之後，本人仍查得到。
	return s.withDriverContact(ride), nil
}
