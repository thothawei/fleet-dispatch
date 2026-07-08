package service

import (
	"context"
	"errors"
	"testing"

	"line-fleet-dispatch/internal/constants"
	lineclient "line-fleet-dispatch/internal/line"
	"line-fleet-dispatch/internal/repository"
)

func TestSetDriverEnabled_停用與啟用(t *testing.T) {
	db := newServiceTestDB(t)
	drivers := repository.NewDriverRepository(db)
	rides := repository.NewRideRepository(db)
	customers := repository.NewCustomerRepository(db)
	redisStore := newServiceTestRedis(t)
	settings := NewDispatchSettings(3000, 5, 20, 1, 5)
	dispatch := NewDispatchService(drivers, rides, customers, redisStore, lineclient.NewClient(""), nil, settings, nil)
	ops := NewAdminOperations(drivers, dispatch, redisStore, settings)

	d, err := drivers.FindOrCreate("U_admin_disable", "待停用司機")
	if err != nil {
		t.Fatalf("建立司機失敗：%v", err)
	}
	ctx := context.Background()
	if err := redisStore.UpdateDriverLocation(ctx, d.ID, 25.03, 121.56); err != nil {
		t.Fatalf("寫入 GEO 失敗：%v", err)
	}

	got, err := ops.SetDriverEnabled(ctx, d.ID, false)
	if err != nil {
		t.Fatalf("停用失敗：%v", err)
	}
	if got.Status != constants.DriverStatusDisabled {
		t.Fatalf("預期 Disabled，得到 %d", got.Status)
	}

	reg := NewDriverRegistry(drivers)
	if _, err := reg.GoOnline(d.ID); !errors.Is(err, ErrDriverDisabled) {
		t.Fatalf("停用後上線預期 ErrDriverDisabled，得到 %v", err)
	}

	got, err = ops.SetDriverEnabled(ctx, d.ID, true)
	if err != nil {
		t.Fatalf("啟用失敗：%v", err)
	}
	if got.Status != constants.DriverStatusOffline {
		t.Fatalf("啟用後預期 Offline，得到 %d", got.Status)
	}

	got, err = reg.GoOnline(d.ID)
	if err != nil {
		t.Fatalf("啟用後上線失敗：%v", err)
	}
	if got.Status != constants.DriverStatusIdle {
		t.Fatalf("上線後預期 Idle，得到 %d", got.Status)
	}
}

func TestSetDriverEnabled_載客中不可停用(t *testing.T) {
	db := newServiceTestDB(t)
	drivers := repository.NewDriverRepository(db)
	ops := NewAdminOperations(drivers, nil, nil, nil)

	d, err := drivers.FindOrCreate("U_admin_ontrip", "載客司機")
	if err != nil {
		t.Fatalf("建立司機失敗：%v", err)
	}
	if err := drivers.UpdateStatus(d.ID, constants.DriverStatusOnTrip); err != nil {
		t.Fatalf("設狀態失敗：%v", err)
	}

	_, err = ops.SetDriverEnabled(context.Background(), d.ID, false)
	if !errors.Is(err, ErrDriverOnTrip) {
		t.Fatalf("載客中停用預期 ErrDriverOnTrip，得到 %v", err)
	}
}
