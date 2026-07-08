package service

import (
	"testing"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/events"
)

func TestStatusPtrHelpers(t *testing.T) {
	s := statusPtr(constants.RideStatusAccepted)
	if s == nil || *s != constants.RideStatusAccepted {
		t.Fatalf("statusPtr 錯誤: %v", s)
	}
	id := idPtr(42)
	if id == nil || *id != 42 {
		t.Fatalf("idPtr 錯誤: %v", id)
	}
}

func TestRideAuditor_NilSafe(t *testing.T) {
	var a *rideAuditor
	a.record(1, nil, constants.RideStatusRequested, events.TypeRideRequested, events.ActorCustomer, nil, "")

	a2 := &rideAuditor{}
	a2.record(1, statusPtr(0), 1, events.TypeRideAssigned, events.ActorSystem, nil, "x")
}

func TestEventTypeConstants_D4(t *testing.T) {
	if events.TypeRideRequested == "" || events.TypeRideRedispatched == "" {
		t.Fatal("D4 新增事件型別不可為空")
	}
	if events.ActorSystem != "system" || events.ActorAdmin != "admin" {
		t.Fatal("actor 常數不符預期")
	}
}
