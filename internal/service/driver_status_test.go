package service

import (
	"errors"
	"testing"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/repository"
)

// TestDriverGoOnlineOffline 驗證 P1 #6 顯式上下線的狀態轉移與載客中守門。
func TestDriverGoOnlineOffline(t *testing.T) {
	db := newServiceTestDB(t)
	driversRepo := repository.NewDriverRepository(db)
	reg := NewDriverRegistry(driversRepo)

	d, err := driversRepo.FindOrCreate("U_status_1", "狀態測試司機")
	if err != nil {
		t.Fatalf("建立司機失敗：%v", err)
	}
	// 新建司機預設待命（Idle）
	if d.Status != constants.DriverStatusIdle {
		t.Fatalf("預期新司機為 Idle，得到 %d", d.Status)
	}

	// 下線 → Offline
	got, err := reg.GoOffline(d.ID)
	if err != nil {
		t.Fatalf("下線失敗：%v", err)
	}
	if got.Status != constants.DriverStatusOffline {
		t.Fatalf("下線後預期 Offline，得到 %d", got.Status)
	}

	// 上線 → Idle
	got, err = reg.GoOnline(d.ID)
	if err != nil {
		t.Fatalf("上線失敗：%v", err)
	}
	if got.Status != constants.DriverStatusIdle {
		t.Fatalf("上線後預期 Idle，得到 %d", got.Status)
	}
}

// TestDriverGoOffline_載客中被拒 載客中不得下線，避免遺失進行中行程
func TestDriverGoOffline_載客中被拒(t *testing.T) {
	db := newServiceTestDB(t)
	driversRepo := repository.NewDriverRepository(db)
	reg := NewDriverRegistry(driversRepo)

	d, err := driversRepo.FindOrCreate("U_status_ontrip", "載客中司機")
	if err != nil {
		t.Fatalf("建立司機失敗：%v", err)
	}
	if err := driversRepo.UpdateStatus(d.ID, constants.DriverStatusOnTrip); err != nil {
		t.Fatalf("設為載客中失敗：%v", err)
	}

	_, err = reg.GoOffline(d.ID)
	if !errors.Is(err, ErrDriverOnTrip) {
		t.Fatalf("載客中下線預期 ErrDriverOnTrip，得到 %v", err)
	}
}

// TestDriverGoOnline_載客中維持原狀 上線不應把載客中司機降回待命
func TestDriverGoOnline_載客中維持原狀(t *testing.T) {
	db := newServiceTestDB(t)
	driversRepo := repository.NewDriverRepository(db)
	reg := NewDriverRegistry(driversRepo)

	d, err := driversRepo.FindOrCreate("U_status_online_ontrip", "載客中司機2")
	if err != nil {
		t.Fatalf("建立司機失敗：%v", err)
	}
	if err := driversRepo.UpdateStatus(d.ID, constants.DriverStatusOnTrip); err != nil {
		t.Fatalf("設為載客中失敗：%v", err)
	}

	got, err := reg.GoOnline(d.ID)
	if err != nil {
		t.Fatalf("上線失敗：%v", err)
	}
	if got.Status != constants.DriverStatusOnTrip {
		t.Fatalf("載客中上線應維持 OnTrip，得到 %d", got.Status)
	}
}

// TestGetActiveRideByDriver 驗收 P1 #7：司機當前進行中訂單（App 重啟恢復用），無則回 nil。
func TestGetActiveRideByDriver(t *testing.T) {
	db := newServiceTestDB(t)
	customers := repository.NewCustomerRepository(db)
	driversRepo := repository.NewDriverRepository(db)
	tracks := repository.NewTrackRepository(db)
	rides := repository.NewRideRepository(db)
	q := NewRideQueryService(tracks, rides)

	drv, err := driversRepo.FindOrCreate("U_active_driver", "有行程司機")
	if err != nil {
		t.Fatalf("建立司機失敗：%v", err)
	}

	// 無進行中訂單 → nil
	got, err := q.GetActiveRideByDriver(drv.ID)
	if err != nil {
		t.Fatalf("查詢失敗：%v", err)
	}
	if got != nil {
		t.Fatalf("預期無進行中訂單回 nil，得到 ride id=%d", got.ID)
	}

	// 指派一筆已接訂單 → 回該筆
	cust, err := customers.FindOrCreateByLineUserID("U_active_driver_cust", "乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	ride := newTestRide(t, rides, cust.ID, constants.RideStatusRequested)
	if err := rides.AcceptRide(ride.ID, drv.ID, 300); err != nil {
		t.Fatalf("指派司機失敗：%v", err)
	}

	got, err = q.GetActiveRideByDriver(drv.ID)
	if err != nil {
		t.Fatalf("查詢失敗：%v", err)
	}
	if got == nil || got.ID != ride.ID {
		t.Fatalf("預期回進行中訂單 id=%d，得到 %v", ride.ID, got)
	}
}
