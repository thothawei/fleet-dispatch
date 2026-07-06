package events

import (
	"encoding/json"
	"testing"
)

func TestEventJSON_含型別與Payload(t *testing.T) {
	ev := Event{
		Type:    TypeRideAccepted,
		RideID:  42,
		Payload: map[string]any{"driver_name": "阿明", "eta_sec": 300},
	}
	raw, err := ev.JSON()
	if err != nil {
		t.Fatalf("序列化失敗: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("反序列化失敗: %v", err)
	}
	if got["type"] != TypeRideAccepted {
		t.Errorf("type 錯誤: %v", got["type"])
	}
	if got["ride_id"].(float64) != 42 {
		t.Errorf("ride_id 錯誤: %v", got["ride_id"])
	}
	payload := got["payload"].(map[string]any)
	if payload["driver_name"] != "阿明" {
		t.Errorf("payload.driver_name 錯誤: %v", payload["driver_name"])
	}
}

func TestRecipient_角色常數(t *testing.T) {
	if RoleDriver != "driver" || RoleCustomer != "customer" || RoleAdmin != "admin" {
		t.Fatal("角色常數值不符預期")
	}
}
