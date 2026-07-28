package service

import (
	"context"
	"testing"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/events"
	lineclient "line-fleet-dispatch/internal/line"
	"line-fleet-dispatch/internal/repository"
)

// TestCancelByDriver_通知App乘客 司機放棄訂單時，App 乘客也必須收到事件。
//
// 這條先前只推 LINE（`line.PushText`），App 乘客一則 WS 事件都收不到——
// 司機卡片停在畫面上，直到最多 15 秒後的輪詢才無聲退回「配對中」。
// 2026-07-28 跨端對帳實測抓到：司機放棄時乘客端 WS 零事件。
func TestCancelByDriver_通知App乘客(t *testing.T) {
	db := newServiceTestDB(t)
	redisStore := newServiceTestRedis(t)
	customers := repository.NewCustomerRepository(db)
	drivers := repository.NewDriverRepository(db)
	rides := repository.NewRideRepository(db)
	fp := &fakePublisher{}
	dispatch := NewDispatchService(drivers, rides, customers, redisStore,
		lineclient.NewClient(""), nil, NewDispatchSettings(3000, 5, 20, 1, 5), fp)

	cust, err := customers.FindOrCreateByLineUserID("U_abandon_cust", "乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	drv, err := drivers.FindOrCreate("U_abandon_drv", "司機")
	if err != nil {
		t.Fatalf("建立司機失敗：%v", err)
	}
	ride := newTestRide(t, rides, cust.ID, constants.RideStatusRequested)
	if err := rides.AcceptRide(ride.ID, drv.ID, 300); err != nil {
		t.Fatalf("接單失敗：%v", err)
	}

	if _, err := dispatch.CancelByDriver(context.Background(), ride.ID, drv.ID); err != nil {
		t.Fatalf("放棄訂單應成功：%v", err)
	}

	var got *events.Event
	for i := range fp.recv {
		r := fp.recv[i]
		if r.Rec.Role == events.RoleCustomer && r.Rec.ID == cust.ID &&
			r.Ev.Type == events.TypeRideRedispatched {
			got = &fp.recv[i].Ev
			break
		}
	}
	if got == nil {
		t.Fatalf("乘客沒收到 %s 事件；實際發佈：%+v", events.TypeRideRedispatched, fp.recv)
	}
	if got.RideID != ride.ID {
		t.Fatalf("事件 ride_id 應為 %d，得到 %d", ride.ID, got.RideID)
	}

	// **不可**用 ride.cancelled：行程沒有取消，只是回到派單中。
	// 送 cancelled 會讓 App 顯示「行程已取消」並清掉整筆訂單，比沒通知更糟。
	for _, r := range fp.recv {
		if r.Rec.Role == events.RoleCustomer && r.Ev.Type == events.TypeRideCancelled {
			t.Fatalf("司機放棄不該對乘客送 %s", events.TypeRideCancelled)
		}
	}

	// 訂單確實回到待派、司機回待命
	after, err := rides.GetByID(ride.ID)
	if err != nil {
		t.Fatalf("查詢訂單失敗：%v", err)
	}
	if after.Status != constants.RideStatusRequested {
		t.Fatalf("放棄後訂單應回 Requested，得到 %d", after.Status)
	}
	d2, err := drivers.FindByID(drv.ID)
	if err != nil {
		t.Fatalf("查詢司機失敗：%v", err)
	}
	if d2.Status != constants.DriverStatusIdle {
		t.Fatalf("放棄後司機應回待命，得到 %d", d2.Status)
	}
}
