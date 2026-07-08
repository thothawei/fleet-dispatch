package service

import (
	"sync"
	"testing"

	"line-fleet-dispatch/internal/events"
	"line-fleet-dispatch/internal/model"
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

func TestRideAssignedPayload_含Dropoff(t *testing.T) {
	lat, lng := 25.08, 121.57
	ride := &model.Ride{
		PickupAddress:  "台北101",
		DropoffAddress: "松山機場",
		DropoffPoint:   &model.GeoPoint{Lat: lat, Lng: lng},
	}
	payload := rideAssignedPayload(ride, ride.PickupAddress, 300, 1200)
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
	payload := rideAssignedPayload(ride, ride.PickupAddress, 300, 1200)
	if _, ok := payload["dropoff_address"]; ok {
		t.Fatal("不應有 dropoff_address")
	}
	if _, ok := payload["dropoff_lat"]; ok {
		t.Fatal("不應有 dropoff_lat")
	}
}
