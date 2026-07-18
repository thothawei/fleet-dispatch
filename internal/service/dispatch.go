package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/rs/zerolog/log"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/events"
	lineclient "line-fleet-dispatch/internal/line"
	"line-fleet-dispatch/internal/model"
	"line-fleet-dispatch/internal/notify"
	redisstore "line-fleet-dispatch/internal/redis"
	"line-fleet-dispatch/internal/repository"
	"line-fleet-dispatch/internal/util"
)

// DispatchService 派單：找最近司機 + 推播接單邀請 + 逾時重派
type DispatchService struct {
	drivers   *repository.DriverRepository
	rides     *repository.RideRepository
	customers *repository.CustomerRepository
	redis     *redisstore.Store
	line      *lineclient.Client
	eta       *ETAService
	settings  *DispatchSettings
	publisher events.Publisher
	appNotify *notify.Dispatcher
	stops     *repository.RideStopRepository
	audit     rideAuditor
}

func NewDispatchService(
	drivers *repository.DriverRepository,
	rides *repository.RideRepository,
	customers *repository.CustomerRepository,
	redis *redisstore.Store,
	line *lineclient.Client,
	eta *ETAService,
	settings *DispatchSettings,
	publisher events.Publisher,
) *DispatchService {
	if settings == nil {
		settings = NewDispatchSettings(3000, 5, 20, 3, 5)
	}
	return &DispatchService{
		drivers:   drivers,
		rides:     rides,
		customers: customers,
		redis:     redis,
		line:      line,
		eta:       eta,
		settings:  settings,
		publisher: publisher,
	}
}

// SetAppNotifier 注入 App 推播（FCM/APNs stub）；可選，測試可不接。
func (s *DispatchService) SetAppNotifier(d *notify.Dispatcher) {
	s.appNotify = d
}

// SetRideEvents 注入訂單狀態審計寫入；可選，測試可不接。
func (s *DispatchService) SetRideEvents(repo *repository.RideEventRepository) {
	s.audit = rideAuditor{events: repo}
}

// SetStops 注入停靠點 repo（供 ride.assigned／ride.accepted 帶 stops，N4）；可選。
func (s *DispatchService) SetStops(stops *repository.RideStopRepository) {
	s.stops = stops
}

// rideStops 讀該趟停靠點；未注入 repo、單點訂單、或讀取失敗皆回 nil
// （呼叫端據此不放 stops 鍵）。
//
// 讀失敗刻意不擋派單：單點訂單本來就沒有停靠點，多停靠點則退化成「司機端看不到全程」，
// 仍可依 rides.pickup_point 前往第一個上車點——比整趟派不出去好。
func (s *DispatchService) rideStops(rideID int64) []model.RideStop {
	if s.stops == nil {
		return nil
	}
	stops, err := s.stops.ListByRide(rideID)
	if err != nil {
		log.Error().Err(err).Int64("ride_id", rideID).Msg("讀取停靠點失敗，本次 payload 不帶 stops")
		return nil
	}
	return stops
}

// publish nil-safe 事件發佈（未接 Hub 時靜默略過）
func (s *DispatchService) publish(rec events.Recipient, ev events.Event) {
	if s.publisher == nil {
		return
	}
	s.publisher.Publish(rec, ev)
}

// Dispatch 叫車後啟動派單（含逾時重派）
func (s *DispatchService) Dispatch(ctx context.Context, rideID int64) error {
	return s.dispatchRound(rideID, 1, map[int64]bool{})
}

// dispatchRound 第 attempt 輪派單：擴大半徑、只派給「尚未派過的待命司機」；
// 逾時仍未被接單則排下一輪；達上限仍無人接 → 取消並通知客戶，確保不卡在 ASSIGNED。
// 各輪由 time.AfterFunc 依序觸發，同一 offered map 不會被並行存取。
func (s *DispatchService) dispatchRound(rideID int64, attempt int, offered map[int64]bool) error {
	ctx := context.Background()
	ride, err := s.rides.GetByID(rideID)
	if err != nil {
		log.Error().Err(err).Int64("ride_id", rideID).Msg("派單讀取訂單失敗")
		return err
	}
	// 已被接單 / 已取消 → 停止派單
	if ride.Status != constants.RideStatusRequested && ride.Status != constants.RideStatusAssigned {
		return nil
	}

	pickupLat, pickupLng, err := s.rides.GetPickupCoords(rideID)
	if err != nil {
		return err
	}

	radiusM, maxCount, offerTimeoutSec, maxAttempts, _ := s.settings.Snapshot()
	offerTimeout := time.Duration(offerTimeoutSec) * time.Second

	radius := radiusM * attempt // 每輪擴大搜尋半徑
	candidates, err := s.redis.NearbyDriverIDs(ctx, pickupLat, pickupLng, radius, maxCount*attempt)
	if err != nil {
		return err
	}

	// 篩掉「已派過的」「已拒接的」「非待命的」「未填車輛資訊的」，避免重複派給同一台車
	rejected := s.redis.RejectedDrivers(ctx, rideID)
	var targets []*model.Driver
	for _, id := range candidates {
		if offered[id] || rejected[id] {
			continue
		}
		driver, err := s.drivers.FindByID(id)
		if err != nil || driver.Status != constants.DriverStatusIdle {
			continue
		}
		// O3 gate：未填車種／車牌者不派單。既有司機一律如此，直到填完（拍板：無寬限期）。
		if !driver.HasVehicle() {
			continue
		}
		// P3：乘客指定車種時只派同車種的司機。**不降級**（P4 拍板）——寵物車／無障礙車是硬需求，
		// 默默改派一般車＝服務失敗還照收錢；找不到就讓它走 giveUpIfUnaccepted 明確取消。
		if ride.RequiredVehicleType != "" && driver.VehicleType != ride.RequiredVehicleType {
			continue
		}
		targets = append(targets, driver)
	}

	if len(targets) > 0 {
		if ride.Status == constants.RideStatusRequested {
			_ = s.rides.UpdateStatus(rideID, constants.RideStatusAssigned)
			s.audit.record(rideID, statusPtr(constants.RideStatusRequested), constants.RideStatusAssigned,
				events.TypeRideAssigned, events.ActorSystem, nil, "dispatch_offer")
			ride.Status = constants.RideStatusAssigned
		}
		for _, d := range targets {
			offered[d.ID] = true
			s.pushOffer(ctx, d, ride, pickupLat, pickupLng)
		}
		log.Info().Int64("ride_id", rideID).Int("attempt", attempt).Int("offered", len(targets)).Msg("已派單")
	} else {
		log.Warn().Int64("ride_id", rideID).Int("attempt", attempt).Msg("本輪無新可用司機")
	}

	// 逾時後：最後一輪 → 放棄並取消；否則 → 擴大重派
	if attempt >= maxAttempts {
		time.AfterFunc(offerTimeout, func() { s.giveUpIfUnaccepted(rideID) })
		return nil
	}
	time.AfterFunc(offerTimeout, func() { _ = s.dispatchRound(rideID, attempt+1, offered) })
	return nil
}

// putDropoff 把訂單的選填目的地寫入 WS payload；未指定時不放任何鍵。
// 座標是司機端導航的原始資料，地址僅供顯示，兩者都要帶。
func putDropoff(payload map[string]any, ride *model.Ride) {
	if ride.DropoffAddress != "" {
		payload["dropoff_address"] = ride.DropoffAddress
	}
	if ride.DropoffPoint != nil {
		payload["dropoff_lat"] = ride.DropoffPoint.Lat
		payload["dropoff_lng"] = ride.DropoffPoint.Lng
	}
}

// rideAcceptedCustomerPayload 組裝給乘客的 ride.accepted payload（O4／O7）。
//
// 車種與車牌讓乘客在路邊對得到車；電話讓乘客能直接聯絡司機（O7 拍板：明碼）。
// **只送給該趟乘客**——本 payload 的收件人是 ride.CustomerID 一人，
// 司機電話絕不可出現在任何列表或廣播事件上。
//
// 送 code 而非顯示名：前端有自己的車種對照（O1 原則）。
// 車輛欄位在 O3 gate 下必定非空，但仍以 omitempty 式的判斷帶入——
// 舊資料或資料異常時，寧可少一個鍵，也不要讓 App 顯示空白車牌。
func rideAcceptedCustomerPayload(driver *model.Driver, etaSec int) map[string]any {
	payload := map[string]any{
		"driver_name": driver.Name,
		"eta_sec":     etaSec,
	}
	if driver.VehicleType != "" {
		payload["driver_vehicle_type"] = driver.VehicleType
	}
	if driver.PlateNumber != "" {
		payload["driver_plate_number"] = driver.PlateNumber
	}
	if driver.Phone != "" {
		payload["driver_phone"] = driver.Phone
	}
	return payload
}

// rideAssignedPayload 組裝 ride.assigned WS 事件 payload（含選填目的地）。
// pickup 座標與 dropoff 對稱一併帶上：司機端接單當下就要在地圖標出上車點，
// 光靠 address 字串無法定位（PickupPoint 為 not null，必定有值）。
// stops 為多乘客／多停靠點行程的全程（N4）：司機**接單前**就要看得到
// 「A 在哪上、B 在哪下、最終到哪」，才知道要不要接。單點訂單不放這個鍵。
//
// 注意（給日後做真 FCM 的人）：目前 App 推播只送 title/body（notify.SendRideOffer 是 stub），
// stops 只走 WS。真 FCM 上線時 data 值一律是字串，stops 這種結構**必須 JSON 字串化**，
// App 端解析要防禦（見 pitfall-fcm-data-all-strings：漏掉會讓推播接單直接崩）。
func rideAssignedPayload(ride *model.Ride, pickupAddress string, etaSec, distM int, stops []model.RideStop) map[string]any {
	payload := map[string]any{
		"address":    pickupAddress,
		"pickup_lat": ride.PickupPoint.Lat,
		"pickup_lng": ride.PickupPoint.Lng,
		"eta_sec":    etaSec,
		"dist_m":     distM,
	}
	putDropoff(payload, ride)
	if v := stopViews(stops); v != nil {
		payload["stops"] = v
	}
	return payload
}

// rideOfferPushData 組裝 FCM/APNs 推播的 data payload（App 被殺後點推播喚醒用）。
//
// 值**一律字串**（FCM data 限制）；App 端 fleetEventFromPushData 會把座標／eta／dist
// 轉回數值。與 rideAssignedPayload 對齊同一組欄位，但**不含 stops**——結構化陣列不放
// 進 data（見 pitfall-fcm-data-all-strings），App 接單後重讀 rides/active 補齊全程。
// dropoff 三鍵未指定目的地時省略（App 端缺鍵＝沒有該資訊）。
func rideOfferPushData(ride *model.Ride, pickupAddress string, etaSec, distM int) map[string]string {
	data := map[string]string{
		"type":       events.TypeRideAssigned,
		"ride_id":    strconv.FormatInt(ride.ID, 10),
		"address":    pickupAddress,
		"pickup_lat": strconv.FormatFloat(ride.PickupPoint.Lat, 'f', -1, 64),
		"pickup_lng": strconv.FormatFloat(ride.PickupPoint.Lng, 'f', -1, 64),
		"eta_sec":    strconv.Itoa(etaSec),
		"dist_m":     strconv.Itoa(distM),
	}
	if ride.DropoffAddress != "" {
		data["dropoff_address"] = ride.DropoffAddress
	}
	if ride.DropoffPoint != nil {
		data["dropoff_lat"] = strconv.FormatFloat(ride.DropoffPoint.Lat, 'f', -1, 64)
		data["dropoff_lng"] = strconv.FormatFloat(ride.DropoffPoint.Lng, 'f', -1, 64)
	}
	return data
}

// rideAcceptedDriverPayload 組裝 ride.accepted 推給「司機端」的 payload；
// 司機接單後直接拿到目的地與全程停靠點，不必等 pickup 回應。
func rideAcceptedDriverPayload(ride *model.Ride, stops []model.RideStop) map[string]any {
	payload := map[string]any{}
	putDropoff(payload, ride)
	if v := stopViews(stops); v != nil {
		payload["stops"] = v
	}
	return payload
}

// pushOffer 推播單一司機接單邀請（附 ETA 與導航連結）
func (s *DispatchService) pushOffer(ctx context.Context, driver *model.Driver, ride *model.Ride, pickupLat, pickupLng float64) {
	etaSec, distM := 300, 1000
	if driverLat, driverLng, ok := s.redis.GetDriverLocation(ctx, driver.ID); ok {
		etaSec, distM = s.eta.PickupETA(ctx, driverLat, driverLng, pickupLat, pickupLng)
	}
	navURL := util.GoogleMapsNavURL(pickupLat, pickupLng)
	msg := fmt.Sprintf("新派單 #%d\n上車點：%s\n距離約 %d 公尺，ETA %d 分鐘\n導航：%s",
		ride.ID, ride.PickupAddress, distM, etaSec/60, navURL)
	if ride.DropoffAddress != "" {
		msg += fmt.Sprintf("\n目的地：%s", ride.DropoffAddress)
	}
	if err := s.line.PushRideOffer(ctx, driver.LineUserID, ride.ID, msg); err != nil {
		log.Error().Err(err).Int64("driver_id", driver.ID).Msg("推播派單失敗")
	}
	// App 推播（FCM/APNs）：與 LINE 並存；無 token／stub 時靜默略過。
	// data 讓 App 被殺後點推播能直接開接單卡（stops 不放，接單後重讀 active 補齊）。
	if s.appNotify != nil {
		s.appNotify.NotifyDriverRideOffer(
			ctx, driver.ID, ride.ID,
			fmt.Sprintf("新派單 #%d", ride.ID),
			msg,
			rideOfferPushData(ride, ride.PickupAddress, etaSec, distM),
		)
	}
	s.publish(events.Recipient{Role: events.RoleDriver, ID: driver.ID}, events.Event{
		Type:    events.TypeRideAssigned,
		RideID:  ride.ID,
		Payload: rideAssignedPayload(ride, ride.PickupAddress, etaSec, distM, s.rideStops(ride.ID)),
	})
}

// giveUpIfUnaccepted 逾時仍無人接單 → 取消訂單並通知客戶（避免永久停在 ASSIGNED）
func (s *DispatchService) giveUpIfUnaccepted(rideID int64) {
	ctx := context.Background()
	ride, err := s.rides.GetByID(rideID)
	if err != nil {
		return
	}
	if ride.Status != constants.RideStatusRequested && ride.Status != constants.RideStatusAssigned {
		return // 已被接單或已取消
	}
	from := ride.Status
	if err := s.rides.UpdateStatus(rideID, constants.RideStatusCancelled); err != nil {
		log.Error().Err(err).Int64("ride_id", rideID).Msg("逾時取消訂單失敗")
		return
	}
	s.audit.record(rideID, statusPtr(from), constants.RideStatusCancelled,
		events.TypeRideCancelled, events.ActorSystem, nil, "dispatch_timeout")
	s.redis.ClearRejected(ctx, rideID)
	log.Warn().Int64("ride_id", rideID).Msg("逾時無人接單，訂單自動取消")

	reason, text, payload := giveUpCancelInfo(ride)
	if customerLineID, err := s.rides.GetCustomerLineUserID(rideID); err == nil && customerLineID != "" {
		_ = s.line.PushText(ctx, customerLineID, text)
	}
	log.Warn().Int64("ride_id", rideID).Str("cancel_reason", reason).Msg("逾時取消原因")
	s.publish(events.Recipient{Role: events.RoleCustomer, ID: ride.CustomerID}, events.Event{
		Type:    events.TypeRideCancelled,
		RideID:  rideID,
		Payload: payload,
	})
}

// giveUpCancelInfo 組出逾時取消的原因、給乘客的文案與 WS payload（P4）。
// 指定了車種卻沒人接，多半是「附近沒有這種車」而非「沒有司機」——泛用文案會誤導乘客
// 一直重試，而正確的引導是「改用不指定車種」。payload 帶機器可讀欄位，
// App 端不必去 parse 文案字串。
func giveUpCancelInfo(ride *model.Ride) (reason, text string, payload map[string]any) {
	if ride.RequiredVehicleType == "" {
		return constants.CancelReasonNoDriver,
			"抱歉，附近暫無可用司機，請稍後再試",
			map[string]any{"cancel_reason": constants.CancelReasonNoDriver}
	}
	name := constants.VehicleTypeDisplayName(ride.RequiredVehicleType)
	return constants.CancelReasonNoVehicleOfType,
		fmt.Sprintf("抱歉，附近暫無%s，請稍後再試", name),
		map[string]any{
			"cancel_reason":         constants.CancelReasonNoVehicleOfType,
			"required_vehicle_type": ride.RequiredVehicleType,
		}
}

// CancelByCustomer 客戶主動取消進行中的訂單（尚未上車前）——LINE 入口，以 line_user_id 找目前進行中的訂單
func (s *DispatchService) CancelByCustomer(ctx context.Context, lineUserID string) (string, error) {
	customer, err := s.customers.FindByLineUserID(lineUserID)
	if err != nil {
		return "您目前沒有進行中的叫車", nil
	}
	ride, err := s.rides.FindActiveByCustomer(customer.ID)
	if err != nil {
		return "", err
	}
	if ride == nil {
		return "您目前沒有進行中的叫車", nil
	}
	return s.cancelActiveRide(ctx, ride, events.ActorCustomer, idPtr(customer.ID), "customer_cancel")
}

// CancelByCustomerID App 端入口：依 JWT 取得的 customer_id + 路徑帶的 ride_id 取消，
// 須先驗證訂單擁有者（非本人回 ErrForbidden，訂單不存在回 ErrNotFound），
// 找到訂單後沿用與 CancelByCustomer 相同的取消核心，不重寫派單/取消邏輯。
func (s *DispatchService) CancelByCustomerID(ctx context.Context, customerID, rideID int64) (string, error) {
	ride, err := s.rides.GetByID(rideID)
	if err != nil {
		return "", ErrNotFound
	}
	if ride.CustomerID != customerID {
		return "", ErrForbidden
	}
	return s.cancelActiveRide(ctx, ride, events.ActorCustomer, idPtr(customerID), "customer_cancel")
}

// cancelActiveRide 取消訂單的共用核心：條件式取消（避免與 accept/complete 競態）、
// 釋放搶單鎖、司機回待命、通知客戶與司機。
func (s *DispatchService) cancelActiveRide(
	ctx context.Context,
	ride *model.Ride,
	actorRole string,
	actorID *int64,
	note string,
) (string, error) {
	if ride.Status == constants.RideStatusPickedUp {
		return "行程已開始，無法取消", nil
	}

	from := ride.Status
	ok, err := s.rides.CancelRide(ride.ID,
		[]int16{constants.RideStatusRequested, constants.RideStatusAssigned, constants.RideStatusAccepted})
	if err != nil {
		return "", err
	}
	if !ok {
		return "訂單狀態已變更，無法取消", nil
	}
	s.audit.record(ride.ID, statusPtr(from), constants.RideStatusCancelled,
		events.TypeRideCancelled, actorRole, actorID, note)
	s.releaseAndReset(ctx, ride.ID, ride.DriverID)
	log.Info().Int64("ride_id", ride.ID).Str("actor", actorRole).Msg("訂單已取消")
	s.publish(events.Recipient{Role: events.RoleCustomer, ID: ride.CustomerID}, events.Event{
		Type:   events.TypeRideCancelled,
		RideID: ride.ID,
	})

	// 若已派給司機，通知司機
	if ride.DriverID != nil {
		if d, e := s.drivers.FindByID(*ride.DriverID); e == nil {
			_ = s.line.PushText(ctx, d.LineUserID, fmt.Sprintf("訂單 #%d 已被取消", ride.ID))
		}
	}
	return "已為您取消叫車", nil
}

// DeclineOffer 司機拒接派單邀請：記錄後重派會跳過此司機，訂單仍留在可派狀態
func (s *DispatchService) DeclineOffer(ctx context.Context, rideID, driverID int64) error {
	log.Info().Int64("ride_id", rideID).Int64("driver_id", driverID).Msg("司機拒單")
	return s.redis.RejectRideDriver(ctx, rideID, driverID)
}

// CancelByDriver 司機放棄「已接」的訂單：司機回待命、標記拒接、訂單重新派單
func (s *DispatchService) CancelByDriver(ctx context.Context, rideID, driverID int64) (string, error) {
	ride, err := s.rides.GetByID(rideID)
	if err != nil {
		return "", err
	}
	if ride.DriverID == nil || *ride.DriverID != driverID {
		return "", ErrForbidden
	}
	if ride.Status != constants.RideStatusAccepted {
		return "此訂單目前無法放棄", nil
	}
	_ = s.redis.RejectRideDriver(ctx, rideID, driverID) // 重派時跳過這位放棄的司機
	s.redis.ReleaseRideLock(ctx, rideID)
	_ = s.drivers.UpdateStatus(driverID, constants.DriverStatusIdle)
	if err := s.rides.ResetToRequested(rideID); err != nil {
		return "", err
	}
	s.audit.record(rideID, statusPtr(constants.RideStatusAccepted), constants.RideStatusRequested,
		events.TypeRideRedispatched, events.ActorDriver, idPtr(driverID), "driver_abandon")
	log.Warn().Int64("ride_id", rideID).Int64("driver_id", driverID).Msg("司機放棄訂單，重新派單")
	go func() { _ = s.Dispatch(context.Background(), rideID) }()

	if customerLineID, e := s.rides.GetCustomerLineUserID(rideID); e == nil && customerLineID != "" {
		_ = s.line.PushText(ctx, customerLineID, "司機取消了行程，正在為您重新派車")
	}
	return "已放棄此訂單", nil
}

// releaseAndReset 取消後的共用收尾：釋放搶單鎖、清拒接集合、司機回待命
func (s *DispatchService) releaseAndReset(ctx context.Context, rideID int64, driverID *int64) {
	s.redis.ReleaseRideLock(ctx, rideID)
	s.redis.ClearRejected(ctx, rideID)
	if driverID != nil {
		_ = s.drivers.UpdateStatus(*driverID, constants.DriverStatusIdle)
	}
}

// AcceptRide 司機接單（搶單鎖）
func (s *DispatchService) AcceptRide(ctx context.Context, rideID, driverID int64, replyToken string) (string, error) {
	ok, err := s.redis.TryLockRide(ctx, rideID, driverID)
	if err != nil {
		return "", err
	}
	if !ok {
		return "手慢了，這單已被其他司機接走", nil
	}

	ride, err := s.rides.GetByID(rideID)
	if err != nil {
		s.redis.ReleaseRideLock(ctx, rideID)
		return "", err
	}
	if ride.Status != constants.RideStatusRequested && ride.Status != constants.RideStatusAssigned {
		s.redis.ReleaseRideLock(ctx, rideID)
		return "手慢了，這單已被其他司機接走", nil
	}

	driver, err := s.drivers.FindByID(driverID)
	if err != nil {
		s.redis.ReleaseRideLock(ctx, rideID)
		return "", err
	}
	// O3 gate：未填車輛資訊者不得接單。派單已先過濾（dispatchRound），這裡擋的是
	// 直接打 API／LINE 的路徑——只靠 App 端跳轉擋不住。
	// 排在狀態檢查之前：這是接單資格問題，回「非待命狀態」會讓司機不知道要去填車輛。
	if !driver.HasVehicle() {
		s.redis.ReleaseRideLock(ctx, rideID)
		return "", ErrDriverNoVehicle
	}
	if driver.Status != constants.DriverStatusIdle {
		s.redis.ReleaseRideLock(ctx, rideID)
		return "您目前無法接單（非待命狀態）", nil
	}

	pickupLat, pickupLng, _ := s.rides.GetPickupCoords(rideID)
	driverLat, driverLng, locOK := s.redis.GetDriverLocation(ctx, driverID)
	etaSec := 300
	if locOK {
		etaSec, _ = s.eta.PickupETA(ctx, driverLat, driverLng, pickupLat, pickupLng)
	}

	from := ride.Status
	if err := s.rides.AcceptRide(rideID, driverID, etaSec); err != nil {
		s.redis.ReleaseRideLock(ctx, rideID)
		return "", errors.New("接單失敗，請重試")
	}
	s.audit.record(rideID, statusPtr(from), constants.RideStatusAccepted,
		events.TypeRideAccepted, events.ActorDriver, idPtr(driverID), "")
	_ = s.drivers.UpdateStatus(driverID, constants.DriverStatusOnTrip)

	navURL := util.GoogleMapsNavURL(pickupLat, pickupLng)
	driverMsg := fmt.Sprintf("接單成功！請前往上車點\n導航：%s", navURL)
	if replyToken != "" {
		_ = s.line.ReplyText(ctx, replyToken, driverMsg)
	} else {
		_ = s.line.PushText(ctx, driver.LineUserID, driverMsg)
	}

	// 通知客戶
	customerLineID, _ := s.rides.GetCustomerLineUserID(rideID)
	if customerLineID != "" {
		customerMsg := fmt.Sprintf("司機 %s 已接單！%s", driver.Name, FormatETAMessage(0, etaSec))
		_ = s.line.PushText(ctx, customerLineID, customerMsg)
	}

	s.publish(events.Recipient{Role: events.RoleCustomer, ID: ride.CustomerID}, events.Event{
		Type:    events.TypeRideAccepted,
		RideID:  rideID,
		Payload: rideAcceptedCustomerPayload(driver, etaSec),
	})
	s.publish(events.Recipient{Role: events.RoleDriver, ID: driverID}, events.Event{
		Type:    events.TypeRideAccepted,
		RideID:  rideID,
		Payload: rideAcceptedDriverPayload(ride, s.rideStops(rideID)),
	})

	return "接單成功", nil
}

// NotifyCustomerETA 司機位置更新時通知客戶 ETA（LINE 推播 + WS driver.location 給乘客 App 地圖追蹤）
func (s *DispatchService) NotifyCustomerETA(ctx context.Context, ride *model.Ride, driverLat, driverLng float64) {
	if ride.Status != constants.RideStatusAccepted {
		return
	}
	pickupLat, pickupLng, err := s.rides.GetPickupCoords(ride.ID)
	if err != nil {
		return
	}
	etaSec, distM := s.eta.PickupETA(ctx, driverLat, driverLng, pickupLat, pickupLng)
	s.publish(events.Recipient{Role: events.RoleCustomer, ID: ride.CustomerID}, events.Event{
		Type:   events.TypeDriverLocation,
		RideID: ride.ID,
		Payload: map[string]any{
			"lat":     driverLat,
			"lng":     driverLng,
			"eta_sec": etaSec,
			"dist_m":  distM,
		},
	})
	customerLineID, err := s.rides.GetCustomerLineUserID(ride.ID)
	if err != nil || customerLineID == "" {
		return
	}
	_ = s.line.PushText(ctx, customerLineID, FormatETAMessage(distM, etaSec))
}
