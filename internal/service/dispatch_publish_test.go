package service

import (
	"context"
	"sync"
	"testing"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/events"
	"line-fleet-dispatch/internal/model"
	"line-fleet-dispatch/internal/repository"
)

// fakePublisher 記錄收到的發佈，供斷言
type fakePublisher struct {
	mu   sync.Mutex
	recv []struct {
		Rec events.Recipient
		Ev  events.Event
	}
}

func (f *fakePublisher) Publish(rec events.Recipient, ev events.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recv = append(f.recv, struct {
		Rec events.Recipient
		Ev  events.Event
	}{rec, ev})
}

func (f *fakePublisher) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.recv)
}

func TestDispatch_publish為nil時不panic(t *testing.T) {
	s := &DispatchService{} // publisher 為 nil
	// 不應 panic
	s.publish(events.Recipient{Role: events.RoleCustomer, ID: 1}, events.Event{Type: events.TypeRideAccepted})
}

func TestDispatch_publish轉發給Publisher(t *testing.T) {
	fp := &fakePublisher{}
	s := &DispatchService{publisher: fp}
	s.publish(events.Recipient{Role: events.RoleDriver, ID: 9}, events.Event{Type: events.TypeRideAssigned, RideID: 5})
	if fp.count() != 1 {
		t.Fatalf("預期發佈 1 則，得到 %d", fp.count())
	}
	if fp.recv[0].Rec.ID != 9 || fp.recv[0].Ev.Type != events.TypeRideAssigned {
		t.Fatalf("發佈內容錯誤: %+v", fp.recv[0])
	}
}

func TestRideAssignedPayload_含Pickup座標(t *testing.T) {
	// 司機端要在地圖標出上車點，只有 address 字串無法定位。
	ride := &model.Ride{
		PickupAddress: "台北101",
		PickupPoint:   model.GeoPoint{Lat: 25.033, Lng: 121.5654},
	}
	payload := rideAssignedPayload(ride, ride.PickupAddress, 300, 1200, nil)
	if payload["pickup_lat"] != 25.033 || payload["pickup_lng"] != 121.5654 {
		t.Fatalf("pickup 座標錯誤: %v", payload)
	}
}

func TestRideAssignedPayload_含Dropoff(t *testing.T) {
	lat, lng := 25.08, 121.57
	ride := &model.Ride{
		PickupAddress:  "台北101",
		DropoffAddress: "松山機場",
		DropoffPoint:   &model.GeoPoint{Lat: lat, Lng: lng},
	}
	payload := rideAssignedPayload(ride, ride.PickupAddress, 300, 1200, nil)
	if payload["address"] != "台北101" {
		t.Fatalf("address 錯誤: %v", payload["address"])
	}
	if payload["dropoff_address"] != "松山機場" {
		t.Fatalf("dropoff_address 錯誤: %v", payload["dropoff_address"])
	}
	if payload["dropoff_lat"] != lat || payload["dropoff_lng"] != lng {
		t.Fatalf("dropoff 座標錯誤: %v", payload)
	}
}

func TestRideAssignedPayload_無Dropoff時省略欄位(t *testing.T) {
	ride := &model.Ride{PickupAddress: "台北101"}
	payload := rideAssignedPayload(ride, ride.PickupAddress, 300, 1200, nil)
	if _, ok := payload["dropoff_address"]; ok {
		t.Fatal("不應有 dropoff_address")
	}
	if _, ok := payload["dropoff_lat"]; ok {
		t.Fatal("不應有 dropoff_lat")
	}
}

func TestRideAcceptedDriverPayload_帶目的地座標(t *testing.T) {
	lat, lng := 25.08, 121.57
	ride := &model.Ride{
		DropoffAddress: "松山機場",
		DropoffPoint:   &model.GeoPoint{Lat: lat, Lng: lng},
	}
	payload := rideAcceptedDriverPayload(ride, nil)
	if payload["dropoff_address"] != "松山機場" {
		t.Fatalf("dropoff_address 錯誤: %v", payload["dropoff_address"])
	}
	if payload["dropoff_lat"] != lat || payload["dropoff_lng"] != lng {
		t.Fatalf("dropoff 座標錯誤: %v", payload)
	}
}

func TestRideAcceptedDriverPayload_無Dropoff時為空(t *testing.T) {
	payload := rideAcceptedDriverPayload(&model.Ride{}, nil)
	if len(payload) != 0 {
		t.Fatalf("無目的地時 payload 應為空: %v", payload)
	}
}

func TestNotifyCustomerETA_發佈driverLocation給乘客(t *testing.T) {
	fp := &fakePublisher{}
	db := newServiceTestDB(t)
	rides := repository.NewRideRepository(db)
	customers := repository.NewCustomerRepository(db)
	cust, err := customers.FindOrCreateByLineUserID("U_eta_ws", "乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	ride := newTestRide(t, rides, cust.ID, constants.RideStatusAccepted)
	s := &DispatchService{
		rides:     rides,
		eta:       NewETAService(nil),
		publisher: fp,
	}
	s.NotifyCustomerETA(context.Background(), ride, 25.05, 121.52)
	if fp.count() != 1 {
		t.Fatalf("預期 WS 發佈 1 則，得到 %d", fp.count())
	}
	got := fp.recv[0]
	if got.Rec.Role != events.RoleCustomer || got.Rec.ID != cust.ID {
		t.Fatalf("收件人錯誤: %+v", got.Rec)
	}
	if got.Ev.Type != events.TypeDriverLocation || got.Ev.RideID != ride.ID {
		t.Fatalf("事件錯誤: %+v", got.Ev)
	}
	if got.Ev.Payload["lat"] != 25.05 || got.Ev.Payload["lng"] != 121.52 {
		t.Fatalf("座標錯誤: %v", got.Ev.Payload)
	}
	if _, ok := got.Ev.Payload["eta_sec"]; !ok {
		t.Fatal("應含 eta_sec")
	}
	if _, ok := got.Ev.Payload["dist_m"]; !ok {
		t.Fatal("應含 dist_m")
	}
}
