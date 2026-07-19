package service

import (
	"context"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/events"
	"line-fleet-dispatch/internal/model"
	redisstore "line-fleet-dispatch/internal/redis"
	"line-fleet-dispatch/internal/repository"
)

// AdminOperations 後台寫入操作（司機審核、派單參數、強制取消）。
type AdminOperations struct {
	drivers  *repository.DriverRepository
	dispatch *DispatchService
	redis    *redisstore.Store
	settings *DispatchSettings
}

func NewAdminOperations(
	drivers *repository.DriverRepository,
	dispatch *DispatchService,
	redis *redisstore.Store,
	settings *DispatchSettings,
) *AdminOperations {
	return &AdminOperations{
		drivers:  drivers,
		dispatch: dispatch,
		redis:    redis,
		settings: settings,
	}
}

// SetDriverEnabled 後台啟用/停用司機。停用時設 Disabled、移出 GEO；載客中不可停用。
// 啟用時恢復 Offline（須自行上線）。
func (a *AdminOperations) SetDriverEnabled(ctx context.Context, driverID int64, enabled bool) (*model.Driver, error) {
	d, err := a.drivers.FindByID(driverID)
	if err != nil {
		return nil, ErrNotFound
	}
	if !enabled {
		if d.Status == constants.DriverStatusOnTrip {
			return nil, ErrDriverOnTrip
		}
		if err := a.drivers.UpdateStatus(driverID, constants.DriverStatusDisabled); err != nil {
			return nil, err
		}
		_ = a.redis.RemoveDriverLocation(ctx, driverID)
		d.Status = constants.DriverStatusDisabled
		return d, nil
	}
	if d.Status == constants.DriverStatusDisabled {
		if err := a.drivers.UpdateStatus(driverID, constants.DriverStatusOffline); err != nil {
			return nil, err
		}
		d.Status = constants.DriverStatusOffline
	}
	return d, nil
}

// ReviewDriverVehicle 後台審核司機車輛（O5）。approve→approved（清原因）；
// reject→rejected+原因。只有「待審核」的司機能審——避免誤審已核准者或沒填車輛者。
// 退回必須附原因（司機要知道哪裡不對才能修）。
func (a *AdminOperations) ReviewDriverVehicle(driverID int64, approve bool, note string) (*model.Driver, error) {
	d, err := a.drivers.FindByID(driverID)
	if err != nil {
		return nil, ErrNotFound
	}
	if d.VehicleReviewStatus != constants.VehicleReviewPending {
		return nil, ErrVehicleNotPending
	}
	status, resultNote := constants.VehicleReviewApproved, ""
	if !approve {
		if note == "" {
			return nil, ErrRejectNoteRequired
		}
		status, resultNote = constants.VehicleReviewRejected, note
	}
	if err := a.drivers.UpdateVehicleReview(driverID, status, resultNote); err != nil {
		return nil, err
	}
	d.VehicleReviewStatus = status
	d.VehicleReviewNote = resultNote
	return d, nil
}

// CancelRideByAdmin 後台強制取消訂單（沿用 cancelActiveRide 核心，已上車不可取消）。
func (a *AdminOperations) CancelRideByAdmin(ctx context.Context, rideID int64) (string, error) {
	ride, err := a.dispatch.rides.GetByID(rideID)
	if err != nil {
		return "", ErrNotFound
	}
	return a.dispatch.cancelActiveRide(ctx, ride, events.ActorAdmin, nil, "admin_force_cancel")
}
