package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/rs/zerolog/log"

	"line-fleet-dispatch/internal/constants"
	lineclient "line-fleet-dispatch/internal/line"
	"line-fleet-dispatch/internal/model"
	redisstore "line-fleet-dispatch/internal/redis"
	"line-fleet-dispatch/internal/repository"
	"line-fleet-dispatch/internal/util"
)

// DispatchService 派單：找最近司機 + 推播接單邀請
type DispatchService struct {
	drivers  *repository.DriverRepository
	rides    *repository.RideRepository
	redis    *redisstore.Store
	line     *lineclient.Client
	eta      *ETAService
	radiusM  int
	maxCount int
}

func NewDispatchService(
	drivers *repository.DriverRepository,
	rides *repository.RideRepository,
	redis *redisstore.Store,
	line *lineclient.Client,
	eta *ETAService,
	radiusM, maxCount int,
) *DispatchService {
	return &DispatchService{
		drivers:  drivers,
		rides:    rides,
		redis:    redis,
		line:     line,
		eta:      eta,
		radiusM:  radiusM,
		maxCount: maxCount,
	}
}

// Dispatch 叫車後自動派單
func (s *DispatchService) Dispatch(ctx context.Context, rideID int64) error {
	ride, err := s.rides.GetByID(rideID)
	if err != nil {
		return err
	}
	if ride.Status != constants.RideStatusRequested {
		return nil
	}

	pickupLat, pickupLng, err := s.rides.GetPickupCoords(rideID)
	if err != nil {
		return err
	}

	driverIDs, err := s.redis.NearbyDriverIDs(ctx, pickupLat, pickupLng, s.radiusM, s.maxCount)
	if err != nil {
		return err
	}
	if len(driverIDs) == 0 {
		log.Warn().Int64("ride_id", rideID).Msg("附近無可用司機")
		return nil
	}

	_ = s.rides.UpdateStatus(rideID, constants.RideStatusAssigned)

	for _, driverID := range driverIDs {
		driver, err := s.drivers.FindByID(driverID)
		if err != nil || driver.Status != constants.DriverStatusIdle {
			continue
		}

		driverLat, driverLng, ok := s.redis.GetDriverLocation(ctx, driverID)
		etaSec, distM := 300, 1000
		if ok {
			etaSec, distM = s.eta.PickupETA(ctx, driverLat, driverLng, pickupLat, pickupLng)
		}

		navURL := util.GoogleMapsNavURL(pickupLat, pickupLng)
		msg := fmt.Sprintf("新派單 #%d\n上車點：%s\n距離約 %d 公尺，ETA %d 分鐘\n導航：%s",
			rideID, ride.PickupAddress, distM, etaSec/60, navURL)

		if err := s.line.PushRideOffer(ctx, driver.LineUserID, rideID, msg); err != nil {
			log.Error().Err(err).Int64("driver_id", driverID).Msg("推播派單失敗")
		}
	}
	return nil
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

	if err := s.rides.AcceptRide(rideID, driverID, etaSec); err != nil {
		s.redis.ReleaseRideLock(ctx, rideID)
		return "", errors.New("接單失敗，請重試")
	}
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

	return "接單成功", nil
}

// NotifyCustomerETA 司機位置更新時通知客戶 ETA
func (s *DispatchService) NotifyCustomerETA(ctx context.Context, ride *model.Ride, driverLat, driverLng float64) {
	if ride.Status != constants.RideStatusAccepted {
		return
	}
	pickupLat, pickupLng, err := s.rides.GetPickupCoords(ride.ID)
	if err != nil {
		return
	}
	etaSec, distM := s.eta.PickupETA(ctx, driverLat, driverLng, pickupLat, pickupLng)
	customerLineID, err := s.rides.GetCustomerLineUserID(ride.ID)
	if err != nil || customerLineID == "" {
		return
	}
	_ = s.line.PushText(ctx, customerLineID, FormatETAMessage(distM, etaSec))
}
