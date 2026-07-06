package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"

	"line-fleet-dispatch/internal/constants"
	lineclient "line-fleet-dispatch/internal/line"
	"line-fleet-dispatch/internal/model"
	redisstore "line-fleet-dispatch/internal/redis"
	"line-fleet-dispatch/internal/repository"
	"line-fleet-dispatch/internal/util"
)

// DispatchService 派單：找最近司機 + 推播接單邀請 + 逾時重派
type DispatchService struct {
	drivers      *repository.DriverRepository
	rides        *repository.RideRepository
	customers    *repository.CustomerRepository
	redis        *redisstore.Store
	line         *lineclient.Client
	eta          *ETAService
	radiusM      int
	maxCount     int
	offerTimeout time.Duration
	maxAttempts  int
}

func NewDispatchService(
	drivers *repository.DriverRepository,
	rides *repository.RideRepository,
	customers *repository.CustomerRepository,
	redis *redisstore.Store,
	line *lineclient.Client,
	eta *ETAService,
	radiusM, maxCount, offerTimeoutSec, maxAttempts int,
) *DispatchService {
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	return &DispatchService{
		drivers:      drivers,
		rides:        rides,
		customers:    customers,
		redis:        redis,
		line:         line,
		eta:          eta,
		radiusM:      radiusM,
		maxCount:     maxCount,
		offerTimeout: time.Duration(offerTimeoutSec) * time.Second,
		maxAttempts:  maxAttempts,
	}
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

	radius := s.radiusM * attempt // 每輪擴大搜尋半徑
	candidates, err := s.redis.NearbyDriverIDs(ctx, pickupLat, pickupLng, radius, s.maxCount*attempt)
	if err != nil {
		return err
	}

	// 篩掉「已派過的」「已拒接的」「非待命的」，避免重複派給同一台車
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
		targets = append(targets, driver)
	}

	if len(targets) > 0 {
		if ride.Status == constants.RideStatusRequested {
			_ = s.rides.UpdateStatus(rideID, constants.RideStatusAssigned)
		}
		for _, d := range targets {
			offered[d.ID] = true
			s.pushOffer(ctx, d, rideID, ride.PickupAddress, pickupLat, pickupLng)
		}
		log.Info().Int64("ride_id", rideID).Int("attempt", attempt).Int("offered", len(targets)).Msg("已派單")
	} else {
		log.Warn().Int64("ride_id", rideID).Int("attempt", attempt).Msg("本輪無新可用司機")
	}

	// 逾時後：最後一輪 → 放棄並取消；否則 → 擴大重派
	if attempt >= s.maxAttempts {
		time.AfterFunc(s.offerTimeout, func() { s.giveUpIfUnaccepted(rideID) })
		return nil
	}
	time.AfterFunc(s.offerTimeout, func() { _ = s.dispatchRound(rideID, attempt+1, offered) })
	return nil
}

// pushOffer 推播單一司機接單邀請（附 ETA 與導航連結）
func (s *DispatchService) pushOffer(ctx context.Context, driver *model.Driver, rideID int64, address string, pickupLat, pickupLng float64) {
	etaSec, distM := 300, 1000
	if driverLat, driverLng, ok := s.redis.GetDriverLocation(ctx, driver.ID); ok {
		etaSec, distM = s.eta.PickupETA(ctx, driverLat, driverLng, pickupLat, pickupLng)
	}
	navURL := util.GoogleMapsNavURL(pickupLat, pickupLng)
	msg := fmt.Sprintf("新派單 #%d\n上車點：%s\n距離約 %d 公尺，ETA %d 分鐘\n導航：%s",
		rideID, address, distM, etaSec/60, navURL)
	if err := s.line.PushRideOffer(ctx, driver.LineUserID, rideID, msg); err != nil {
		log.Error().Err(err).Int64("driver_id", driver.ID).Msg("推播派單失敗")
	}
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
	if err := s.rides.UpdateStatus(rideID, constants.RideStatusCancelled); err != nil {
		log.Error().Err(err).Int64("ride_id", rideID).Msg("逾時取消訂單失敗")
		return
	}
	s.redis.ClearRejected(ctx, rideID)
	log.Warn().Int64("ride_id", rideID).Msg("逾時無人接單，訂單自動取消")
	if customerLineID, err := s.rides.GetCustomerLineUserID(rideID); err == nil && customerLineID != "" {
		_ = s.line.PushText(ctx, customerLineID, "抱歉，附近暫無可用司機，請稍後再試")
	}
}

// CancelByCustomer 客戶主動取消進行中的訂單（尚未上車前）
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
	if ride.Status == constants.RideStatusPickedUp {
		return "行程已開始，無法取消", nil
	}

	ok, err := s.rides.CancelRide(ride.ID,
		[]int16{constants.RideStatusRequested, constants.RideStatusAssigned, constants.RideStatusAccepted})
	if err != nil {
		return "", err
	}
	if !ok {
		return "訂單狀態已變更，無法取消", nil
	}
	s.releaseAndReset(ctx, ride.ID, ride.DriverID)
	log.Info().Int64("ride_id", ride.ID).Msg("客戶取消訂單")

	// 若已派給司機，通知司機
	if ride.DriverID != nil {
		if d, e := s.drivers.FindByID(*ride.DriverID); e == nil {
			_ = s.line.PushText(ctx, d.LineUserID, fmt.Sprintf("訂單 #%d 已被乘客取消", ride.ID))
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
