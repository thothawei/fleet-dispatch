package service

import (
	"testing"

	"line-fleet-dispatch/internal/events"
)

func TestTracking_publish為nil時不panic(t *testing.T) {
	s := &TrackingService{}
	s.publish(events.Recipient{Role: events.RoleAdmin, ID: 0}, events.Event{Type: events.TypeDriverLocation})
}

func TestTracking_publish轉發給Publisher(t *testing.T) {
	fp := &fakePublisher{}
	s := &TrackingService{publisher: fp}
	s.publish(events.Recipient{Role: events.RoleAdmin, ID: 0}, events.Event{Type: events.TypeDriverLocation})
	if fp.count() != 1 {
		t.Fatalf("預期發佈 1 則，得到 %d", fp.count())
	}
	if fp.recv[0].Rec.Role != events.RoleAdmin || fp.recv[0].Ev.Type != events.TypeDriverLocation {
		t.Fatalf("發佈內容錯誤: %+v", fp.recv[0])
	}
}
