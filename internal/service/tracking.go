package service

import (
	"context"

	"github.com/rs/zerolog/log"

	"line-fleet-dispatch/internal/constants"
	lineclient "line-fleet-dispatch/internal/line"
	"line-fleet-dispatch/internal/model"
	redisstore "line-fleet-dispatch/internal/redis"
	"line-fleet-dispatch/internal/repository"
)

const geofenceRadiusM = 100

// TrackingService 位置回報、圍籬偵測、軌跡記錄
type TrackingService struct {
	drivers   *repository.DriverRepository
	rides     *repository.RideRepository
	tracks    *repository.TrackRepository
	redis     *redisstore.Store
	line      *lineclient.Client
	dispatch  *DispatchService
	geofenced map[int64]bool
}

func NewTrackingService(
	drivers *repository.DriverRepository,
	rides *repository.RideRepository,
	tracks *repository.TrackRepository,
	redis *redisstore.Store,
	line *lineclient.Client,
	dispatch *DispatchService,
) *TrackingService {
	return &TrackingService{
		drivers:   drivers,
		rides:     rides,
		tracks:    tracks,
		redis:     redis,
		line:      line,
		dispatch:  dispatch,
		geofenced: make(map[int64]bool),
	}
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

	return s.handleActiveRide(ctx, driver, lat, lng)
}

func (s *TrackingService) handleActiveRide(ctx context.Context, driver *model.Driver, lat, lng float64) error {
	ride, err := s.rides.FindActiveByDriver(driver.ID)
	if err != nil || ride == nil {
		return nil
	}

	switch ride.Status {
	case constants.RideStatusAccepted:
		s.checkGeofence(ctx, ride, lat, lng)
		s.dispatch.NotifyCustomerETA(ctx, ride, lat, lng)
	case constants.RideStatusPickedUp:
		if err := s.tracks.Insert(ride.ID, driver.ID, lat, lng); err != nil {
			log.Error().Err(err).Int64("ride_id", ride.ID).Msg("寫入軌跡失敗")
		}
	}
	return nil
}

func (s *TrackingService) checkGeofence(ctx context.Context, ride *model.Ride, lat, lng float64) {
	if s.geofenced[ride.ID] {
		return
	}
	within, err := s.rides.IsWithinPickup(ride.ID, lat, lng, geofenceRadiusM)
	if err != nil || !within {
		return
	}
	s.geofenced[ride.ID] = true

	customerLineID, err := s.rides.GetCustomerLineUserID(ride.ID)
	if err != nil || customerLineID == "" {
		return
	}
	log.Info().Int64("ride_id", ride.ID).Msg("司機進入上車圍籬")
	_ = s.line.PushText(ctx, customerLineID, "司機已抵達上車點，請準備上車")
}

// PickUp 司機確認客戶上車
func (s *TrackingService) PickUp(ctx context.Context, rideID, driverID int64) error {
	ride, err := s.rides.GetByID(rideID)
	if err != nil {
		return err
	}
	if ride.DriverID == nil || *ride.DriverID != driverID {
		return ErrForbidden
	}
	if err := s.rides.MarkPickedUp(rideID); err != nil {
		return err
	}
	customerLineID, _ := s.rides.GetCustomerLineUserID(rideID)
	if customerLineID != "" {
		_ = s.line.PushText(ctx, customerLineID, "行程開始，祝您旅途愉快")
	}
	return nil
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
	if err := s.rides.CompleteRide(rideID, distanceM); err != nil {
		return err
	}
	_ = s.drivers.UpdateStatus(driverID, constants.DriverStatusIdle)
	delete(s.geofenced, rideID)

	customerLineID, _ := s.rides.GetCustomerLineUserID(rideID)
	if customerLineID != "" {
		_ = s.line.PushText(ctx, customerLineID, "行程已完成，感謝搭乘")
	}
	return nil
}
