package service

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/events"
	lineclient "line-fleet-dispatch/internal/line"
	"line-fleet-dispatch/internal/model"
	redisstore "line-fleet-dispatch/internal/redis"
	"line-fleet-dispatch/internal/repository"
	"line-fleet-dispatch/internal/util"
)

const geofenceRadiusM = 100

// etaPushState 記錄上次推播 ETA 的時間與司機位置，用於節流
type etaPushState struct {
	at  time.Time
	lat float64
	lng float64
}

// TrackingService 位置回報、圍籬偵測、軌跡記錄、ETA 推播節流
type TrackingService struct {
	drivers   *repository.DriverRepository
	rides     *repository.RideRepository
	tracks    *repository.TrackRepository
	redis     *redisstore.Store
	line      *lineclient.Client
	dispatch  *DispatchService
	publisher events.Publisher
	audit     rideAuditor
	fees      *FeeSettings

	etaMinInterval   time.Duration
	etaDistThreshold float64

	mu        sync.Mutex
	geofenced map[int64]bool
	etaPushed map[int64]etaPushState
}

func NewTrackingService(
	drivers *repository.DriverRepository,
	rides *repository.RideRepository,
	tracks *repository.TrackRepository,
	redis *redisstore.Store,
	line *lineclient.Client,
	dispatch *DispatchService,
	etaMinIntervalSec, etaDistThresholdM int,
	publisher events.Publisher,
) *TrackingService {
	return &TrackingService{
		drivers:          drivers,
		rides:            rides,
		tracks:           tracks,
		redis:            redis,
		line:             line,
		dispatch:         dispatch,
		publisher:        publisher,
		etaMinInterval:   time.Duration(etaMinIntervalSec) * time.Second,
		etaDistThreshold: float64(etaDistThresholdM),
		geofenced:        make(map[int64]bool),
		etaPushed:        make(map[int64]etaPushState),
	}
}

// SetRideEvents 注入訂單狀態審計寫入；可選。
func (s *TrackingService) SetRideEvents(repo *repository.RideEventRepository) {
	s.audit = rideAuditor{events: repo}
}

// SetFeeSettings 注入費率設定，完成行程時據此定格計費；可選（未注入則不計費）。
func (s *TrackingService) SetFeeSettings(fees *FeeSettings) {
	s.fees = fees
}

// publish nil-safe 事件發佈
func (s *TrackingService) publish(rec events.Recipient, ev events.Event) {
	if s.publisher == nil {
		return
	}
	s.publisher.Publish(rec, ev)
}

// ReportDriverLocation 司機回報位置
func (s *TrackingService) ReportDriverLocation(ctx context.Context, driverID int64, lat, lng float64) error {
	driver, err := s.drivers.FindByID(driverID)
	if err != nil {
		return err
	}

	if err := s.redis.UpdateDriverLocation(ctx, driverID, lat, lng); err != nil {
		return err
	}

	s.publish(events.Recipient{Role: events.RoleAdmin, ID: 0}, events.Event{
		Type:    events.TypeDriverLocation,
		Payload: map[string]any{"driver_id": driverID, "lat": lat, "lng": lng},
	})

	return s.handleActiveRide(ctx, driver, lat, lng)
}

func (s *TrackingService) handleActiveRide(ctx context.Context, driver *model.Driver, lat, lng float64) error {
	ride, err := s.rides.FindActiveByDriver(driver.ID)
	if err != nil || ride == nil {
		return nil
	}

	switch ride.Status {
	case constants.RideStatusAccepted:
		s.checkGeofence(ctx, ride, lat, lng) // 圍籬抵達即時通知，不節流
		if s.shouldPushETA(ride.ID, lat, lng) {
			log.Info().Int64("ride_id", ride.ID).Msg("推播客戶 ETA")
			s.dispatch.NotifyCustomerETA(ctx, ride, lat, lng)
		}
	case constants.RideStatusPickedUp:
		if err := s.tracks.Insert(ride.ID, driver.ID, lat, lng); err != nil {
			log.Error().Err(err).Int64("ride_id", ride.ID).Msg("寫入軌跡失敗")
		}
	}
	return nil
}

// shouldPushETA 節流：距上次推播超過 etaMinInterval，或司機移動超過 etaDistThreshold 才推；首次一定推
func (s *TrackingService) shouldPushETA(rideID int64, lat, lng float64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev, ok := s.etaPushed[rideID]
	if ok &&
		time.Since(prev.at) < s.etaMinInterval &&
		util.HaversineM(prev.lat, prev.lng, lat, lng) < s.etaDistThreshold {
		return false
	}
	s.etaPushed[rideID] = etaPushState{at: time.Now(), lat: lat, lng: lng}
	return true
}

func (s *TrackingService) checkGeofence(ctx context.Context, ride *model.Ride, lat, lng float64) {
	s.mu.Lock()
	already := s.geofenced[ride.ID]
	s.mu.Unlock()
	if already {
		return
	}

	within, err := s.rides.IsWithinPickup(ride.ID, lat, lng, geofenceRadiusM)
	if err != nil || !within {
		return
	}

	s.mu.Lock()
	if s.geofenced[ride.ID] { // 併發時再檢查一次，避免重複通知
		s.mu.Unlock()
		return
	}
	s.geofenced[ride.ID] = true
	s.mu.Unlock()

	customerLineID, err := s.rides.GetCustomerLineUserID(ride.ID)
	if err != nil || customerLineID == "" {
		return
	}
	log.Info().Int64("ride_id", ride.ID).Msg("司機進入上車圍籬")
	_ = s.line.PushText(ctx, customerLineID, "司機已抵達上車點，請準備上車")
	actor := (*int64)(nil)
	if ride.DriverID != nil {
		actor = ride.DriverID
	}
	s.audit.record(ride.ID, statusPtr(constants.RideStatusAccepted), constants.RideStatusAccepted,
		events.TypeDriverArrived, events.ActorDriver, actor, "geofence")
	s.publish(events.Recipient{Role: events.RoleCustomer, ID: ride.CustomerID}, events.Event{
		Type:   events.TypeDriverArrived,
		RideID: ride.ID,
	})
}

// PickUpResult 回給司機端上車後導航去目的地所需的資訊。
// HasDropoffPoint 為 false 時代表該訂單未指定目的地座標（DropoffAddress 可能仍有值）。
type PickUpResult struct {
	DropoffAddress  string
	DropoffLat      float64
	DropoffLng      float64
	HasDropoffPoint bool
}

// PickUp 司機確認客戶上車，回傳目的地資訊供司機端顯示「導航去目的地」。
func (s *TrackingService) PickUp(ctx context.Context, rideID, driverID int64) (*PickUpResult, error) {
	ride, err := s.rides.GetByID(rideID)
	if err != nil {
		return nil, err
	}
	if ride.DriverID == nil || *ride.DriverID != driverID {
		return nil, ErrForbidden
	}
	if err := s.rides.MarkPickedUp(rideID); err != nil {
		return nil, err
	}
	s.audit.record(rideID, statusPtr(constants.RideStatusAccepted), constants.RideStatusPickedUp,
		events.TypeRidePickedUp, events.ActorDriver, idPtr(driverID), "")
	s.publish(events.Recipient{Role: events.RoleCustomer, ID: ride.CustomerID}, events.Event{
		Type:   events.TypeRidePickedUp,
		RideID: rideID,
	})
	customerLineID, _ := s.rides.GetCustomerLineUserID(rideID)
	if customerLineID != "" {
		_ = s.line.PushText(ctx, customerLineID, "行程開始，祝您旅途愉快")
	}

	res := &PickUpResult{DropoffAddress: ride.DropoffAddress}
	if ride.DropoffPoint != nil {
		res.DropoffLat = ride.DropoffPoint.Lat
		res.DropoffLng = ride.DropoffPoint.Lng
		res.HasDropoffPoint = true
	}
	return res, nil
}

// Complete 完成行程
func (s *TrackingService) Complete(ctx context.Context, rideID, driverID int64) error {
	ride, err := s.rides.GetByID(rideID)
	if err != nil {
		return err
	}
	if ride.DriverID == nil || *ride.DriverID != driverID {
		return ErrForbidden
	}
	distanceM, _ := s.rides.TrackDistanceM(rideID)

	// 費率快照：完成當下依當前費率算好車資/手續費/實得，定格寫進本筆 ride。
	// 未注入費率設定時（如部分測試）三者留 NULL，不影響完成流程。
	var fareCents, commissionCents, driverNetCents *int64
	if s.fees != nil {
		f, c, n := s.fees.Quote(distanceM)
		fareCents, commissionCents, driverNetCents = &f, &c, &n
	}
	if err := s.rides.CompleteRide(rideID, distanceM, fareCents, commissionCents, driverNetCents); err != nil {
		return err
	}
	s.audit.record(rideID, statusPtr(constants.RideStatusPickedUp), constants.RideStatusCompleted,
		events.TypeRideCompleted, events.ActorDriver, idPtr(driverID), "")
	_ = s.drivers.UpdateStatus(driverID, constants.DriverStatusIdle)

	completedPayload := map[string]any{"distance_m": distanceM}
	if fareCents != nil {
		completedPayload["fare_amount_cents"] = *fareCents // 供乘客端完成卡顯示車資（E2）
	}
	s.publish(events.Recipient{Role: events.RoleCustomer, ID: ride.CustomerID}, events.Event{
		Type:    events.TypeRideCompleted,
		RideID:  rideID,
		Payload: completedPayload,
	})
	s.publish(events.Recipient{Role: events.RoleDriver, ID: driverID}, events.Event{
		Type:   events.TypeRideCompleted,
		RideID: rideID,
	})

	s.mu.Lock()
	delete(s.geofenced, rideID)
	delete(s.etaPushed, rideID)
	s.mu.Unlock()

	customerLineID, _ := s.rides.GetCustomerLineUserID(rideID)
	if customerLineID != "" {
		_ = s.line.PushText(ctx, customerLineID, "行程已完成，感謝搭乘")
	}
	return nil
}
