package service

import (
	"testing"
	"time"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/events"
	"line-fleet-dispatch/internal/model"
	"line-fleet-dispatch/internal/repository"
)

// 到站／跳過要即時推給乘客，否則乘客只能等下一次輪詢才知道司機走到第幾站。

type publishFixture struct {
	svc        *RideStopService
	pub        *fakePublisher
	rideID     int64
	driverID   int64
	customerID int64
	stopIDs    []int64
}

func newPublishFixture(t *testing.T, prefix string) *publishFixture {
	t.Helper()
	db := newServiceTestDB(t)
	customers := repository.NewCustomerRepository(db)
	drivers := repository.NewDriverRepository(db)
	rides := repository.NewRideRepository(db)
	stopsRepo := repository.NewRideStopRepository(db)

	cust, err := customers.FindOrCreateByLineUserID(prefix+"_cust", "乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	driver, err := drivers.FindOrCreate(prefix+"_drv", "司機")
	if err != nil {
		t.Fatalf("建立司機失敗：%v", err)
	}
	now := time.Now()
	ride := &model.Ride{
		CustomerID: cust.ID, Status: constants.RideStatusRequested,
		PickupPoint: model.GeoPoint{Lat: 25.03, Lng: 121.56}, PickupAddress: "台北車站",
		RequestedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := rides.Create(ride); err != nil {
		t.Fatalf("建立訂單失敗：%v", err)
	}
	if err := stopsRepo.CreateForRide(ride.ID, []repository.StopRow{
		{Seq: 1, Kind: constants.StopKindPickup, Lat: 25.03, Lng: 121.56, PassengerLabel: "A"},
		{Seq: 2, Kind: constants.StopKindDropoff, Lat: 25.05, Lng: 121.58, PassengerLabel: "A"},
	}); err != nil {
		t.Fatalf("建立停靠點失敗：%v", err)
	}
	if err := rides.AcceptRide(ride.ID, driver.ID, 300); err != nil {
		t.Fatalf("接單失敗：%v", err)
	}
	stops, err := stopsRepo.ListByRide(ride.ID)
	if err != nil {
		t.Fatalf("讀停靠點失敗：%v", err)
	}
	ids := make([]int64, 0, len(stops))
	for _, s := range stops {
		ids = append(ids, s.ID)
	}

	pub := &fakePublisher{}
	svc := NewRideStopService(rides, stopsRepo)
	svc.SetPublisher(pub)
	return &publishFixture{
		svc: svc, pub: pub,
		rideID: ride.ID, driverID: driver.ID, customerID: cust.ID, stopIDs: ids,
	}
}

func TestMarkArrived_推ride_stop_updated給乘客(t *testing.T) {
	f := newPublishFixture(t, "U_stop_pub")

	if err := f.svc.MarkArrived(f.driverID, f.rideID, f.stopIDs[0]); err != nil {
		t.Fatalf("標記到達失敗：%v", err)
	}
	if f.pub.count() != 1 {
		t.Fatalf("預期推 1 則事件，得到 %d", f.pub.count())
	}
	got := f.pub.recv[0]
	if got.Rec.Role != events.RoleCustomer || got.Rec.ID != f.customerID {
		t.Errorf("事件要送給本趟乘客，得到 %+v", got.Rec)
	}
	if got.Ev.Type != events.TypeRideStopUpdated || got.Ev.RideID != f.rideID {
		t.Errorf("事件型別／ride_id 不符：%+v", got.Ev)
	}
	// payload 帶整趟 stops：乘客端收到直接覆蓋，不必自己套用差異、也不怕漏收事件。
	stops, ok := got.Ev.Payload["stops"].([]map[string]any)
	if !ok || len(stops) != 2 {
		t.Fatalf("payload 應帶整趟 2 個停靠點，得到 %v", got.Ev.Payload["stops"])
	}
	if _, ok := stops[0]["arrived_at"]; !ok {
		t.Errorf("剛標記到達的站要帶 arrived_at：%v", stops[0])
	}
	if _, ok := stops[1]["arrived_at"]; ok {
		t.Errorf("未處理的站不該有 arrived_at：%v", stops[1])
	}
}

func TestMarkSkipped_也要推事件(t *testing.T) {
	f := newPublishFixture(t, "U_stop_pub_skip")

	if err := f.svc.MarkSkipped(f.driverID, f.rideID, f.stopIDs[0]); err != nil {
		t.Fatalf("標記跳過失敗：%v", err)
	}
	if f.pub.count() != 1 {
		t.Fatalf("預期推 1 則事件，得到 %d", f.pub.count())
	}
	stops := f.pub.recv[0].Ev.Payload["stops"].([]map[string]any)
	if _, ok := stops[0]["skipped_at"]; !ok {
		t.Errorf("跳過的站要帶 skipped_at：%v", stops[0])
	}
}

func TestMark_失敗時不推事件(t *testing.T) {
	f := newPublishFixture(t, "U_stop_pub_fail")

	// 重複標記：第二次回 ErrStopAlreadyHandled，狀態沒變就不該再推事件
	// （否則乘客端會收到「進度更新」卻什麼都沒變）。
	if err := f.svc.MarkArrived(f.driverID, f.rideID, f.stopIDs[0]); err != nil {
		t.Fatalf("第一次標記應成功：%v", err)
	}
	if err := f.svc.MarkArrived(f.driverID, f.rideID, f.stopIDs[0]); err == nil {
		t.Fatal("重複標記應回錯誤")
	}
	if f.pub.count() != 1 {
		t.Fatalf("失敗的標記不該推事件，總共推了 %d 則", f.pub.count())
	}
}

func TestMark_未注入publisher仍可標記(t *testing.T) {
	f := newPublishFixture(t, "U_stop_pub_nil")
	f.svc.SetPublisher(nil) // 推播是加值，不是標記的前提

	if err := f.svc.MarkArrived(f.driverID, f.rideID, f.stopIDs[0]); err != nil {
		t.Fatalf("未注入 publisher 時標記仍應成功：%v", err)
	}
}
