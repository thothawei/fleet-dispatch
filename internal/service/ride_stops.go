package service

import (
	"errors"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/events"
	"line-fleet-dispatch/internal/model"
	"line-fleet-dispatch/internal/repository"
)

var (
	// ErrStopAlreadyHandled 該停靠點已標記過到達或跳過（N7）。
	// 到達時間是計費與稽核的原始資料，不接受覆寫。
	ErrStopAlreadyHandled = errors.New("此停靠點已標記過")
	// ErrBadStopState 行程狀態不允許標記停靠點（已完成／已取消時計費已定格）。
	ErrBadStopState = errors.New("行程狀態不允許標記停靠點")
)

const timeLayoutRFC3339 = "2006-01-02T15:04:05Z07:00"

// stopView 送給司機端的單一停靠點（N4／N6）。
// 座標攤平成 lat／lng——與 ride.assigned 既有的 pickup_lat/lng 一致，
// App 端不必為 stops 另寫一套座標解析。
func stopView(s model.RideStop) map[string]any {
	v := map[string]any{
		"id":              s.ID,
		"seq":             s.Seq,
		"kind":            s.Kind,
		"lat":             s.Point.Lat,
		"lng":             s.Point.Lng,
		"passenger_label": s.PassengerLabel,
	}
	if s.Address != "" {
		v["address"] = s.Address
	}
	// 只在真的發生時帶——司機端據此顯示「已到達／已跳過」，兩者皆無＝待處理。
	if s.ArrivedAt != nil {
		v["arrived_at"] = s.ArrivedAt.Format(timeLayoutRFC3339)
	}
	if s.SkippedAt != nil {
		v["skipped_at"] = s.SkippedAt.Format(timeLayoutRFC3339)
	}
	return v
}

// stopViews 轉整趟停靠點；空 slice 回 nil，讓呼叫端可以「沒有就不放這個鍵」
// （單點訂單的 payload 不該多一個空 stops 陣列）。
func stopViews(stops []model.RideStop) []map[string]any {
	if len(stops) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(stops))
	for _, s := range stops {
		out = append(out, stopView(s))
	}
	return out
}

// StopViews 對外公開的停靠點序列化（admin 訂單詳情用）。
// 刻意與司機／乘客端共用同一個 stopView——三端看到的停靠點形狀必須一模一樣，
// 否則客服在後台看到的站序／到達時間會與司機 App 說法不同。
func StopViews(stops []model.RideStop) []map[string]any {
	return stopViews(stops)
}

// DriverRideView 司機視角的進行中訂單：ride 全欄位 ＋ 全程停靠點（N6）。
// 內嵌 *model.Ride 讓 JSON 攤平，既有欄位一個不少——App 讀到的形狀只多不變。
// 單點訂單的 Stops 為 nil（omitempty），不會多出一個空陣列。
type DriverRideView struct {
	*model.Ride
	Stops []map[string]any `json:"stops,omitempty"`
}

// RideStopService 停靠點的讀取與標記（N6／N7）。
type RideStopService struct {
	rides     *repository.RideRepository
	stops     *repository.RideStopRepository
	publisher events.Publisher
}

func NewRideStopService(rides *repository.RideRepository, stops *repository.RideStopRepository) *RideStopService {
	return &RideStopService{rides: rides, stops: stops}
}

// SetPublisher 注入事件發佈器，讓到站／跳過即時推給乘客；可選——
// 未注入時標記照樣成功，乘客只是要等下一次輪詢才看得到進度。
func (s *RideStopService) SetPublisher(p events.Publisher) {
	s.publisher = p
}

// ListForDriver 司機讀取自己那趟的停靠點；非被指派司機回 ErrForbidden。
func (s *RideStopService) ListForDriver(driverID, rideID int64) ([]model.RideStop, error) {
	ride, err := s.rides.GetByID(rideID)
	if err != nil {
		return nil, ErrNotFound
	}
	if ride.DriverID == nil || *ride.DriverID != driverID {
		return nil, ErrForbidden
	}
	return s.stops.ListByRide(rideID)
}

// MarkArrived 司機標記已到達該停靠點（N7）。
func (s *RideStopService) MarkArrived(driverID, rideID, stopID int64) error {
	return s.mark(driverID, rideID, stopID, s.stops.MarkArrived)
}

// MarkSkipped 司機標記乘客未出現、跳過該停靠點（N7，2026-07-17 拍板）。
// 被跳過的站不計入 N5 的計費路線——沒去就沒開那段路。
func (s *RideStopService) MarkSkipped(driverID, rideID, stopID int64) error {
	return s.mark(driverID, rideID, stopID, s.stops.MarkSkipped)
}

// mark 到達／跳過共用的授權與狀態檢查。
//
// 授權在標記之前，且**同時驗 ride 歸屬與 stop 歸屬**：stop_id 是全域遞增的，
// 只驗 ride 的話，司機可以拿自己的 ride_id 去改別人行程的停靠點狀態。
func (s *RideStopService) mark(driverID, rideID, stopID int64, do func(int64) (bool, error)) error {
	ride, err := s.rides.GetByID(rideID)
	if err != nil {
		return ErrNotFound
	}
	if ride.DriverID == nil || *ride.DriverID != driverID {
		return ErrForbidden
	}
	// 行程已完成／取消就不該再改停靠點狀態（計費已定格，改了也不會重算）。
	if ride.Status != constants.RideStatusAccepted && ride.Status != constants.RideStatusPickedUp {
		return ErrBadStopState
	}
	stop, err := s.stops.FindByID(stopID)
	if err != nil {
		return ErrNotFound
	}
	if stop.RideID != rideID {
		return ErrForbidden
	}
	ok, err := do(stopID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrStopAlreadyHandled // 已到達或已跳過，不覆寫
	}
	s.publishStops(ride.ID, ride.CustomerID)
	return nil
}

// publishStops 把**整趟**最新停靠點推給乘客（司機端剛做完動作、會自己重讀 active）。
//
// 帶整批而非單一 stop：乘客端收到直接覆蓋即可，不必在客戶端套用差異，
// 也不怕漏收某一則事件後狀態就永遠對不上。
// 讀失敗／未注入 publisher 都不影響標記本身——標記已經寫進 DB 了。
func (s *RideStopService) publishStops(rideID, customerID int64) {
	if s.publisher == nil {
		return
	}
	stops, err := s.stops.ListByRide(rideID)
	if err != nil {
		return
	}
	s.publisher.Publish(
		events.Recipient{Role: events.RoleCustomer, ID: customerID},
		events.Event{
			Type:    events.TypeRideStopUpdated,
			RideID:  rideID,
			Payload: map[string]any{"stops": stopViews(stops)},
		},
	)
}
