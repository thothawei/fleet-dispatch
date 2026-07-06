package service

import (
	"sync"
	"testing"

	"line-fleet-dispatch/internal/events"
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
