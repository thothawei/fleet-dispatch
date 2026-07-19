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

// gateFixture 一台有車、一台沒車，兩台都在上車點附近待命。
type gateFixture struct {
	dispatch   *DispatchService
	publisher  *fakePublisher
	withID     int64 // 有車且已審核（可派單／可接單）
	withoutID  int64 // 沒填車輛
	pendingID  int64 // 填了但待審核（O5 gate 擋下）
	rideID     int64
	driversRep *repository.DriverRepository
	ridesRep   *repository.RideRepository
}

func newGateFixture(t *testing.T, prefix string) *gateFixture {
	t.Helper()
	db := newServiceTestDB(t)
	redisStore := newServiceTestRedis(t)
	drivers := repository.NewDriverRepository(db)
	rides := repository.NewRideRepository(db)
	customers := repository.NewCustomerRepository(db)
	fp := &fakePublisher{}
	// maxAttempts=1：只跑一輪，測試不必等重派。
	dispatch := NewDispatchService(drivers, rides, customers, redisStore,
		lineclient.NewClient(""), NewETAService(nil), NewDispatchSettings(3000, 5, 20, 1, 5), fp)

	withVehicle, err := drivers.FindOrCreate(prefix+"_with", "有車司機")
	if err != nil {
		t.Fatalf("建立司機失敗：%v", err)
	}
	if err := drivers.UpdateVehicle(withVehicle.ID, constants.VehicleTypeSedan, "GATE-0001"); err != nil {
		t.Fatalf("設定車輛資訊失敗：%v", err)
	}
	// O5：填車輛只到 pending，要 admin 核准才能接單——這台代表「已核准」。
	if err := drivers.UpdateVehicleReview(withVehicle.ID, constants.VehicleReviewApproved, ""); err != nil {
		t.Fatalf("核准車輛失敗：%v", err)
	}
	// 未填車輛：這正是既有司機在 O1 migration 後的狀態（兩欄皆為 ''）。
	withoutVehicle, err := drivers.FindOrCreate(prefix+"_without", "無車司機")
	if err != nil {
		t.Fatalf("建立司機失敗：%v", err)
	}
	// O5：填了但待審核（UpdateVehicle 後 review_status=pending，未核准）。
	pendingVehicle, err := drivers.FindOrCreate(prefix+"_pending", "待審核司機")
	if err != nil {
		t.Fatalf("建立司機失敗：%v", err)
	}
	if err := drivers.UpdateVehicle(pendingVehicle.ID, constants.VehicleTypeSedan, "GATE-PEND"); err != nil {
		t.Fatalf("設定車輛資訊失敗：%v", err)
	}

	ctx := context.Background()
	for _, id := range []int64{withVehicle.ID, withoutVehicle.ID, pendingVehicle.ID} {
		if err := redisStore.UpdateDriverLocation(ctx, id, 25.031, 121.561); err != nil {
			t.Fatalf("寫入司機位置失敗：%v", err)
		}
	}

	cust, err := customers.FindOrCreateByLineUserID(prefix+"_cust", "乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	ride := newTestRide(t, rides, cust.ID, constants.RideStatusRequested)

	return &gateFixture{
		dispatch:   dispatch,
		publisher:  fp,
		withID:     withVehicle.ID,
		withoutID:  withoutVehicle.ID,
		pendingID:  pendingVehicle.ID,
		rideID:     ride.ID,
		driversRep: drivers,
		ridesRep:   rides,
	}
}

// TestDispatch_只派給已審核的司機 O5 gate 的派單側：
// 沒填車輛、或填了但待審核者都不得進入候選（只有已核准者被派）。
func TestDispatch_只派給已審核的司機(t *testing.T) {
	f := newGateFixture(t, "U_gate_dispatch")

	if err := f.dispatch.Dispatch(context.Background(), f.rideID); err != nil {
		t.Fatalf("派單失敗：%v", err)
	}

	var offeredTo []int64
	for _, r := range f.publisher.recv {
		if r.Ev.Type == events.TypeRideAssigned && r.Rec.Role == events.RoleDriver {
			offeredTo = append(offeredTo, r.Rec.ID)
		}
	}
	if len(offeredTo) != 1 {
		t.Fatalf("預期只派給 1 位（已審核者），實際派給 %v", offeredTo)
	}
	if offeredTo[0] != f.withID {
		t.Fatalf("預期派給已審核司機 %d，實際派給 %d（未填與待審核都不該被派）", f.withID, offeredTo[0])
	}
}

// TestAcceptRide_待審核回ErrDriverNotApproved O5 gate 的接單側：
// 填了車輛但尚未核准者接單 → ErrDriverNotApproved（與「沒填」的 NoVehicle 分開）。
func TestAcceptRide_待審核回ErrDriverNotApproved(t *testing.T) {
	f := newGateFixture(t, "U_gate_pending")
	ctx := context.Background()

	_, err := f.dispatch.AcceptRide(ctx, f.rideID, f.pendingID, "")
	if !errors.Is(err, ErrDriverNotApproved) {
		t.Fatalf("預期 ErrDriverNotApproved，得到 %v", err)
	}
	// 被擋下不留副作用：訂單狀態不變。
	ride, err := f.ridesRep.GetByID(f.rideID)
	if err != nil {
		t.Fatalf("重讀訂單失敗：%v", err)
	}
	if ride.Status != constants.RideStatusRequested {
		t.Fatalf("被擋下的接單不該改變訂單狀態，得到 %d", ride.Status)
	}
	// gate 早退仍須釋放搶單鎖——用「已審核司機仍能接」釘住。
	if _, err := f.dispatch.AcceptRide(ctx, f.rideID, f.withID, ""); err != nil {
		t.Fatalf("已審核司機應能接單（gate 須釋放搶單鎖）：%v", err)
	}
}

// TestAcceptRide_未填車輛回ErrDriverNoVehicle O3 gate 的接單側：
// 擋的是直接打 API／LINE 的路徑——只靠 App 端跳轉擋不住。
func TestAcceptRide_未填車輛回ErrDriverNoVehicle(t *testing.T) {
	f := newGateFixture(t, "U_gate_accept")
	ctx := context.Background()

	_, err := f.dispatch.AcceptRide(ctx, f.rideID, f.withoutID, "")
	if !errors.Is(err, ErrDriverNoVehicle) {
		t.Fatalf("預期 ErrDriverNoVehicle，得到 %v", err)
	}

	// 被擋下時不得留下副作用：訂單狀態不變、司機沒被設成載客中。
	ride, err := f.ridesRep.GetByID(f.rideID)
	if err != nil {
		t.Fatalf("重讀訂單失敗：%v", err)
	}
	if ride.Status != constants.RideStatusRequested {
		t.Fatalf("被擋下的接單不該改變訂單狀態，得到 %d", ride.Status)
	}
	if ride.DriverID != nil {
		t.Fatalf("被擋下的接單不該綁定司機，得到 %v", *ride.DriverID)
	}
	d, err := f.driversRep.FindByID(f.withoutID)
	if err != nil {
		t.Fatalf("重讀司機失敗：%v", err)
	}
	if d.Status != constants.DriverStatusIdle {
		t.Fatalf("被擋下的司機狀態不該變動，得到 %d", d.Status)
	}

	// gate 早退時若漏放搶單鎖，這張單就再也沒人接得走——用「有車司機仍能接」釘住。
	if _, err := f.dispatch.AcceptRide(ctx, f.rideID, f.withID, ""); err != nil {
		t.Fatalf("有車司機應能接單（gate 須釋放搶單鎖）：%v", err)
	}
	ride, err = f.ridesRep.GetByID(f.rideID)
	if err != nil {
		t.Fatalf("重讀訂單失敗：%v", err)
	}
	if ride.Status != constants.RideStatusAccepted {
		t.Fatalf("有車司機接單後預期 Accepted，得到 %d", ride.Status)
	}
}
