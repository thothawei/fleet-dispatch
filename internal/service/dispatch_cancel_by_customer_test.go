package service

import (
	"context"
	"errors"
	"testing"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/events"
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
	dispatch := NewDispatchService(drivers, rides, customers, redisStore, lineclient.NewClient(""), nil, NewDispatchSettings(3000, 5, 20, 1, 5), nil)

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
	dispatch := NewDispatchService(drivers, rides, customers, redisStore, lineclient.NewClient(""), nil, NewDispatchSettings(3000, 5, 20, 1, 5), nil)

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
	dispatch := NewDispatchService(drivers, rides, customers, redisStore, lineclient.NewClient(""), nil, NewDispatchSettings(3000, 5, 20, 1, 5), nil)

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
	dispatch := NewDispatchService(drivers, rides, customers, redisStore, lineclient.NewClient(""), nil, NewDispatchSettings(3000, 5, 20, 1, 5), nil)

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

// TestCancelByCustomerID_通知App司機 乘客取消**已接**的訂單時，App 司機也必須收到事件。
//
// 這條先前只推 LINE（`line.PushText`），App 司機一則 WS 事件都收不到。
// 司機端**沒有任何輪詢**（進行中行程全靠 WS），所以行程卡會一直留在畫面上——
// 他會繼續開往上車點接一個已經取消的乘客，直到按下「乘客已上車」被後端擋下才知道；
// 而後端這時早已把他放回 Idle。這是 dispatch#52（司機放棄只通知 LINE）在司機側的鏡像。
func TestCancelByCustomerID_通知App司機(t *testing.T) {
	db := newServiceTestDB(t)
	redisStore := newServiceTestRedis(t)
	customers := repository.NewCustomerRepository(db)
	drivers := repository.NewDriverRepository(db)
	rides := repository.NewRideRepository(db)
	fp := &fakePublisher{}
	dispatch := NewDispatchService(drivers, rides, customers, redisStore,
		lineclient.NewClient(""), nil, NewDispatchSettings(3000, 5, 20, 1, 5), fp)

	cust, err := customers.FindOrCreateByLineUserID("U_cancel_notify_cust", "乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	drv, err := drivers.FindOrCreate("U_cancel_notify_drv", "司機")
	if err != nil {
		t.Fatalf("建立司機失敗：%v", err)
	}
	ride := newTestRide(t, rides, cust.ID, constants.RideStatusRequested)
	if err := rides.AcceptRide(ride.ID, drv.ID, 300); err != nil {
		t.Fatalf("接單失敗：%v", err)
	}

	if _, err := dispatch.CancelByCustomerID(context.Background(), cust.ID, ride.ID); err != nil {
		t.Fatalf("乘客取消應成功：%v", err)
	}

	var toCustomer, toDriver *events.Event
	for i := range fp.recv {
		r := fp.recv[i]
		if r.Ev.Type != events.TypeRideCancelled {
			continue
		}
		switch {
		case r.Rec.Role == events.RoleCustomer && r.Rec.ID == cust.ID:
			toCustomer = &fp.recv[i].Ev
		case r.Rec.Role == events.RoleDriver && r.Rec.ID == drv.ID:
			toDriver = &fp.recv[i].Ev
		}
	}
	if toCustomer == nil {
		t.Fatalf("乘客沒收到 %s；實際發佈：%+v", events.TypeRideCancelled, fp.recv)
	}
	if toDriver == nil {
		t.Fatalf("**司機**沒收到 %s——他的行程卡會一直留著；實際發佈：%+v",
			events.TypeRideCancelled, fp.recv)
	}
	if toDriver.RideID != ride.ID {
		t.Fatalf("司機收到的事件 ride_id 應為 %d，得到 %d", ride.ID, toDriver.RideID)
	}
}

// TestCancelByCustomerID_未派單時不推司機 還沒有司機的訂單不該憑空推給誰。
func TestCancelByCustomerID_未派單時不推司機(t *testing.T) {
	db := newServiceTestDB(t)
	redisStore := newServiceTestRedis(t)
	customers := repository.NewCustomerRepository(db)
	drivers := repository.NewDriverRepository(db)
	rides := repository.NewRideRepository(db)
	fp := &fakePublisher{}
	dispatch := NewDispatchService(drivers, rides, customers, redisStore,
		lineclient.NewClient(""), nil, NewDispatchSettings(3000, 5, 20, 1, 5), fp)

	cust, err := customers.FindOrCreateByLineUserID("U_cancel_nodrv_cust", "乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	ride := newTestRide(t, rides, cust.ID, constants.RideStatusRequested)

	if _, err := dispatch.CancelByCustomerID(context.Background(), cust.ID, ride.ID); err != nil {
		t.Fatalf("乘客取消應成功：%v", err)
	}

	for _, r := range fp.recv {
		if r.Rec.Role == events.RoleDriver {
			t.Fatalf("未派單的訂單不該推事件給司機，卻推了：%+v", r)
		}
	}
}
