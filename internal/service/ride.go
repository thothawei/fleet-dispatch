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
	stops     *repository.RideStopRepository
	redis     *redisstore.Store
	dispatch  *DispatchService
	audit     rideAuditor
}

// SetStops 注入停靠點 repo（供多乘客／多停靠點行程，N3）；可選——
// 未注入時帶 stops 的建單會被拒（見 CreateByCustomer），單點建單不受影響。
func (s *RideService) SetStops(stops *repository.RideStopRepository) {
	s.stops = stops
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
	// Stops 選填（N3）：多乘客／多停靠點行程。空 ＝ 傳統單點訂單，完全維持現行行為。
	// 有給時，pickup/dropoff 座標改由 stops 推導（第一個 pickup／最終 dropoff），
	// 呼叫端傳的 PickupLat/Lng 等欄位會被覆蓋。
	Stops []StopInput
}

// prepareStops 驗證停靠點並由它們推導 rides 的 pickup／dropoff（N3）。
// 回傳排序後的停靠點；單點訂單回 nil，req 不動。
//
// **相容性的關鍵**：多停靠點行程仍照樣寫 rides.pickup_point（第一個上車點）與
// dropoff_point（最終目的地）。派單找最近司機、司機導航、地圖、F3 路線里程退路、報表
// ——全部照原樣讀 rides，一行都不用改。ride_stops 是額外資訊，不是替代品。
func (s *RideService) prepareStops(req *CustomerCreateRequest) ([]StopInput, error) {
	if len(req.Stops) == 0 {
		return nil, nil // 單點訂單，維持現行行為
	}
	if s.stops == nil {
		// 沒注入 repo 卻收到 stops：與其默默當成單點訂單建出「少載四個人」的行程，
		// 不如明確失敗。
		return nil, ErrStopsUnavailable
	}
	if err := validateStops(req.Stops); err != nil {
		return nil, err
	}
	sorted := sortStopsBySeq(req.Stops)
	first, ok := firstPickup(sorted)
	if !ok {
		return nil, ErrUnpairedStop
	}
	final, ok := finalDropoff(sorted)
	if !ok {
		return nil, ErrUnpairedStop
	}
	req.PickupLat, req.PickupLng, req.PickupAddress = first.Lat, first.Lng, first.Address
	req.DropoffLat, req.DropoffLng, req.DropoffAddress = &final.Lat, &final.Lng, final.Address
	return sorted, nil
}

// CreateByCustomer 供已登入乘客（App）直接叫車：身分取自 JWT 的 customer_id。
// 下單前擋「同一乘客已有進行中訂單」，建立後沿用非同步派單。
func (s *RideService) CreateByCustomer(
	ctx context.Context,
	customerID int64,
	req CustomerCreateRequest,
) (*model.Ride, error) {
	// 多停靠點（N3）：先驗證再由 stops 推導 pickup／dropoff，讓後續流程與單點訂單完全一致。
	sortedStops, err := s.prepareStops(&req)
	if err != nil {
		return nil, err
	}
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
	// 停靠點必須在派單**之前**落庫：派單一發出，司機端就該看得到全程（N4）。
	// 寫失敗則整筆建單失敗——只有第一個上車點、少載其餘乘客的行程，比沒建成更糟。
	if len(sortedStops) > 0 {
		if err := s.stops.CreateForRide(ride.ID, toStopRows(sortedStops)); err != nil {
			return nil, err
		}
	}
	s.audit.record(ride.ID, nil, constants.RideStatusRequested,
		events.TypeRideRequested, events.ActorCustomer, idPtr(customer.ID), "app")

	// 非同步派單（與 CreateFromLocation 一致）
	go func(rideID int64) {
		_ = s.dispatch.Dispatch(context.Background(), rideID)
	}(ride.ID)

	return ride, nil
}

// toStopRows 轉成 repository 的落庫形狀。
func toStopRows(stops []StopInput) []repository.StopRow {
	rows := make([]repository.StopRow, 0, len(stops))
	for _, s := range stops {
		rows = append(rows, repository.StopRow{
			Seq:            s.Seq,
			Kind:           s.Kind,
			Lat:            s.Lat,
			Lng:            s.Lng,
			Address:        s.Address,
			PassengerLabel: s.PassengerLabel,
		})
	}
	return rows
}

// RideQueryService 訂單查詢
type RideQueryService struct {
	tracks  *repository.TrackRepository
	rides   *repository.RideRepository
	drivers *repository.DriverRepository
	stops   *repository.RideStopRepository
}

func NewRideQueryService(tracks *repository.TrackRepository, rides *repository.RideRepository) *RideQueryService {
	return &RideQueryService{tracks: tracks, rides: rides}
}

// SetStops 注入停靠點 repo（供司機端 active 帶 stops，N6）；可選——
// 未注入時單點與多停靠點行程皆可查，只是後者不帶 stops。
func (s *RideQueryService) SetStops(stops *repository.RideStopRepository) {
	s.stops = stops
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
func (s *RideQueryService) GetActiveRideByDriver(driverID int64) (*DriverRideView, error) {
	ride, err := s.rides.FindActiveByDriver(driverID)
	if err != nil || ride == nil {
		return nil, err
	}
	view := &DriverRideView{Ride: ride}
	// N6：司機要知道下一站是誰、在哪。單點訂單為 nil（omitempty），形狀不變。
	// 讀失敗不擋——司機仍能依 rides.pickup_point／dropoff_point 執行行程。
	if s.stops != nil {
		if stops, err := s.stops.ListByRide(ride.ID); err == nil {
			view.Stops = stopViews(stops)
		}
	}
	return view, nil
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
