package service

import (
	"context"
	"errors"
	"testing"

	"line-fleet-dispatch/internal/constants"
	lineclient "line-fleet-dispatch/internal/line"
	"line-fleet-dispatch/internal/repository"
)

// TestCancelByCustomerID_他人訂單回Forbidden 驗收條件 #4 前置：非本人訂單須被拒絕
func TestCancelByCustomerID_他人訂單回Forbidden(t *testing.T) {
	db := newServiceTestDB(t)
	redisStore := newServiceTestRedis(t)
	customers := repository.NewCustomerRepository(db)
	drivers := repository.NewDriverRepository(db)
	rides := repository.NewRideRepository(db)
	dispatch := NewDispatchService(drivers, rides, customers, redisStore, lineclient.NewClient(""), nil, 3000, 5, 20, 1, nil)

	owner, err := customers.FindOrCreateByLineUserID("U_cancel_owner", "乘客A")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	other, err := customers.FindOrCreateByLineUserID("U_cancel_other", "乘客B")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	ride := newTestRide(t, rides, owner.ID, constants.RideStatusRequested)

	_, err = dispatch.CancelByCustomerID(context.Background(), other.ID, ride.ID)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("非本人訂單預期 ErrForbidden，得到 %v", err)
	}

	// 訂單狀態應完全不受影響
	got, gerr := rides.GetByID(ride.ID)
	if gerr != nil {
		t.Fatalf("查詢訂單失敗：%v", gerr)
	}
	if got.Status != constants.RideStatusRequested {
		t.Fatalf("非本人取消失敗後訂單狀態不應變更，預期 %d 得到 %d", constants.RideStatusRequested, got.Status)
	}
}

// TestCancelByCustomerID_不存在訂單回NotFound
func TestCancelByCustomerID_不存在訂單回NotFound(t *testing.T) {
	db := newServiceTestDB(t)
	redisStore := newServiceTestRedis(t)
	customers := repository.NewCustomerRepository(db)
	drivers := repository.NewDriverRepository(db)
	rides := repository.NewRideRepository(db)
	dispatch := NewDispatchService(drivers, rides, customers, redisStore, lineclient.NewClient(""), nil, 3000, 5, 20, 1, nil)

	_, err := dispatch.CancelByCustomerID(context.Background(), 1, 999999)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在的訂單預期 ErrNotFound，得到 %v", err)
	}
}

// TestCancelByCustomerID_已上車無法取消 行程已開始（PICKED_UP）時應拒絕、狀態不變
func TestCancelByCustomerID_已上車無法取消(t *testing.T) {
	db := newServiceTestDB(t)
	redisStore := newServiceTestRedis(t)
	customers := repository.NewCustomerRepository(db)
	drivers := repository.NewDriverRepository(db)
	rides := repository.NewRideRepository(db)
	dispatch := NewDispatchService(drivers, rides, customers, redisStore, lineclient.NewClient(""), nil, 3000, 5, 20, 1, nil)

	cust, err := customers.FindOrCreateByLineUserID("U_cancel_pickedup", "測試乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	ride := newTestRide(t, rides, cust.ID, constants.RideStatusPickedUp)

	msg, err := dispatch.CancelByCustomerID(context.Background(), cust.ID, ride.ID)
	if err != nil {
		t.Fatalf("預期無錯誤（以文字訊息拒絕），得到 %v", err)
	}
	if msg != "行程已開始，無法取消" {
		t.Fatalf("預期拒絕訊息，得到 %q", msg)
	}
	got, gerr := rides.GetByID(ride.ID)
	if gerr != nil {
		t.Fatalf("查詢訂單失敗：%v", gerr)
	}
	if got.Status != constants.RideStatusPickedUp {
		t.Fatalf("已上車拒絕取消後狀態不應變更，得到 %d", got.Status)
	}
}

// TestCancelByCustomerID_本人取消釋放鎖與司機回待命 驗收條件 #4：取消複用既有 service、釋放搶單鎖
func TestCancelByCustomerID_本人取消釋放鎖與司機回待命(t *testing.T) {
	db := newServiceTestDB(t)
	redisStore := newServiceTestRedis(t)
	customers := repository.NewCustomerRepository(db)
	drivers := repository.NewDriverRepository(db)
	rides := repository.NewRideRepository(db)
	dispatch := NewDispatchService(drivers, rides, customers, redisStore, lineclient.NewClient(""), nil, 3000, 5, 20, 1, nil)

	cust, err := customers.FindOrCreateByLineUserID("U_cancel_ok", "測試乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	driver, err := drivers.FindOrCreate("U_cancel_driver", "測試司機")
	if err != nil {
		t.Fatalf("建立司機失敗：%v", err)
	}

	ride := newTestRide(t, rides, cust.ID, constants.RideStatusRequested)
	// 模擬司機已接單：訂單轉 ACCEPTED、綁定 driver_id，司機轉 ON_TRIP
	if err := rides.AcceptRide(ride.ID, driver.ID, 300); err != nil {
		t.Fatalf("模擬接單失敗：%v", err)
	}
	if err := drivers.UpdateStatus(driver.ID, constants.DriverStatusOnTrip); err != nil {
		t.Fatalf("設定司機狀態失敗：%v", err)
	}

	ctx := context.Background()
	// 模擬搶單鎖仍持有中
	locked, lerr := redisStore.TryLockRide(ctx, ride.ID, driver.ID)
	if lerr != nil || !locked {
		t.Fatalf("預先搶鎖失敗：locked=%v err=%v", locked, lerr)
	}

	msg, err := dispatch.CancelByCustomerID(ctx, cust.ID, ride.ID)
	if err != nil {
		t.Fatalf("本人取消不應出錯，得到 %v", err)
	}
	if msg != "已為您取消叫車" {
		t.Fatalf("預期成功取消訊息，得到 %q", msg)
	}

	// 訂單狀態應變為 CANCELLED
	got, gerr := rides.GetByID(ride.ID)
	if gerr != nil {
		t.Fatalf("查詢訂單失敗：%v", gerr)
	}
	if got.Status != constants.RideStatusCancelled {
		t.Fatalf("預期訂單狀態 CANCELLED(%d)，得到 %d", constants.RideStatusCancelled, got.Status)
	}

	// 司機應回待命
	d, derr := drivers.FindByID(driver.ID)
	if derr != nil {
		t.Fatalf("查詢司機失敗：%v", derr)
	}
	if d.Status != constants.DriverStatusIdle {
		t.Fatalf("預期司機回待命 IDLE(%d)，得到 %d", constants.DriverStatusIdle, d.Status)
	}

	// 搶單鎖應已釋放：其他司機應能立即搶到
	relocked, rerr := redisStore.TryLockRide(ctx, ride.ID, 999)
	if rerr != nil {
		t.Fatalf("驗證鎖釋放時出錯：%v", rerr)
	}
	if !relocked {
		t.Fatalf("預期取消後搶單鎖已釋放，其他司機應能搶到鎖")
	}
}
