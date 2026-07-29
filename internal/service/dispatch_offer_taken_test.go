package service

import (
	"context"
	"testing"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/events"
	lineclient "line-fleet-dispatch/internal/line"
	"line-fleet-dispatch/internal/repository"
)

// 同一張單會**同時**推給半徑內每一位待命司機，只有一位搶得到。
// 先前沒搶到的人手機上留著一張全螢幕接單卡，而**沒有任何事件收得掉它**——
// ride.accepted 只送給接到的那位，逾時取消也只通知乘客。
// 他得自己按下去、拿到「手慢了，這單已被其他司機接走」才會消失。
//
// 這組測試釘住補送的兩則：接單 → ride.taken 給沒接到的人；取消 → ride.cancelled 給所有人。

func TestAcceptRide_通知沒搶到的司機(t *testing.T) {
	db := newServiceTestDB(t)
	redisStore := newServiceTestRedis(t)
	customers := repository.NewCustomerRepository(db)
	drivers := repository.NewDriverRepository(db)
	rides := repository.NewRideRepository(db)
	fp := &fakePublisher{}
	dispatch := NewDispatchService(drivers, rides, customers, redisStore,
		lineclient.NewClient(""), NewETAService(nil), NewDispatchSettings(3000, 5, 20, 1, 5), fp)

	cust, err := customers.FindOrCreateByLineUserID("U_taken_cust", "乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	winner, err := drivers.FindOrCreate("U_taken_win", "搶到的司機")
	if err != nil {
		t.Fatalf("建立司機失敗：%v", err)
	}
	loser, err := drivers.FindOrCreate("U_taken_lose", "沒搶到的司機")
	if err != nil {
		t.Fatalf("建立司機失敗：%v", err)
	}
	if err := drivers.UpdateStatus(winner.ID, constants.DriverStatusIdle); err != nil {
		t.Fatalf("設定司機狀態失敗：%v", err)
	}
	// O5 gate：沒有已核准的車輛就接不了單。
	if err := drivers.UpdateVehicle(winner.ID, constants.VehicleTypeSedan, "TAKEN-01"); err != nil {
		t.Fatalf("設定車輛失敗：%v", err)
	}
	if err := drivers.UpdateVehicleReview(winner.ID, constants.VehicleReviewApproved, ""); err != nil {
		t.Fatalf("核准車輛失敗：%v", err)
	}
	ride := newTestRide(t, rides, cust.ID, constants.RideStatusAssigned)

	ctx := context.Background()
	// 模擬本輪派單：兩位都收到了 offer
	for _, id := range []int64{winner.ID, loser.ID} {
		if err := redisStore.OfferRideDriver(ctx, ride.ID, id); err != nil {
			t.Fatalf("記錄派單對象失敗：%v", err)
		}
	}

	if _, err := dispatch.AcceptRide(ctx, ride.ID, winner.ID, ""); err != nil {
		t.Fatalf("接單應成功：%v", err)
	}

	var takenTo []int64
	for _, r := range fp.recv {
		if r.Ev.Type == events.TypeRideTaken && r.Rec.Role == events.RoleDriver {
			takenTo = append(takenTo, r.Rec.ID)
		}
	}
	if len(takenTo) != 1 || takenTo[0] != loser.ID {
		t.Fatalf("ride.taken 應只送給沒搶到的司機 %d，實際送給 %v（全部事件：%+v）",
			loser.ID, takenTo, fp.recv)
	}
	// 接到的那位收的是 ride.accepted，不該再收一則「被別人接走」
	for _, r := range fp.recv {
		if r.Ev.Type == events.TypeRideTaken && r.Rec.ID == winner.ID {
			t.Fatalf("接到單的司機不該收到 ride.taken：%+v", r)
		}
	}
	// 集合用過即清，重派時不會誤發給舊名單
	if left := redisStore.OfferedDrivers(ctx, ride.ID); len(left) != 0 {
		t.Fatalf("接單後 offered 集合應清空，仍有 %v", left)
	}
}

func TestGiveUp逾時取消_通知收過offer的司機(t *testing.T) {
	db := newServiceTestDB(t)
	redisStore := newServiceTestRedis(t)
	customers := repository.NewCustomerRepository(db)
	drivers := repository.NewDriverRepository(db)
	rides := repository.NewRideRepository(db)
	fp := &fakePublisher{}
	dispatch := NewDispatchService(drivers, rides, customers, redisStore,
		lineclient.NewClient(""), NewETAService(nil), NewDispatchSettings(3000, 5, 20, 1, 5), fp)

	cust, err := customers.FindOrCreateByLineUserID("U_giveup_cust", "乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	drv, err := drivers.FindOrCreate("U_giveup_drv", "收到邀請的司機")
	if err != nil {
		t.Fatalf("建立司機失敗：%v", err)
	}
	ride := newTestRide(t, rides, cust.ID, constants.RideStatusAssigned)

	ctx := context.Background()
	if err := redisStore.OfferRideDriver(ctx, ride.ID, drv.ID); err != nil {
		t.Fatalf("記錄派單對象失敗：%v", err)
	}

	dispatch.giveUpIfUnaccepted(ride.ID)

	var gotDriver bool
	for _, r := range fp.recv {
		if r.Ev.Type == events.TypeRideCancelled && r.Rec.Role == events.RoleDriver && r.Rec.ID == drv.ID {
			gotDriver = true
		}
	}
	if !gotDriver {
		t.Fatalf("逾時取消時收過 offer 的司機也要收到 %s，否則接單卡留著；實際：%+v",
			events.TypeRideCancelled, fp.recv)
	}
}
