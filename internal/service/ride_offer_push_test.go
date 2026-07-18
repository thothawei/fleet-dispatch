package service

import (
	"testing"

	"line-fleet-dispatch/internal/model"
)

// TestRideOfferPushData D1/A2：FCM data 值一律字串，App 被殺後點推播喚醒接單卡靠它。
func TestRideOfferPushData(t *testing.T) {
	dropoff := model.GeoPoint{Lat: 25.0697, Lng: 121.5522}
	ride := &model.Ride{
		ID:             42,
		PickupPoint:    model.GeoPoint{Lat: 25.0478, Lng: 121.5170},
		DropoffAddress: "松山機場",
		DropoffPoint:   &dropoff,
	}

	data := rideOfferPushData(ride, "台北車站", 300, 1200)

	// 值一律字串，App 端 fleetEventFromPushData 才轉回數值（漏字串化推播接單會崩）。
	want := map[string]string{
		"type":            "ride.assigned",
		"ride_id":         "42",
		"address":         "台北車站",
		"pickup_lat":      "25.0478",
		"pickup_lng":      "121.517",
		"eta_sec":         "300",
		"dist_m":          "1200",
		"dropoff_address": "松山機場",
		"dropoff_lat":     "25.0697",
		"dropoff_lng":     "121.5522",
	}
	for k, v := range want {
		if data[k] != v {
			t.Errorf("data[%q]=%q，預期 %q", k, data[k], v)
		}
	}
	// stops 這種結構化陣列**不放進 data**——App 接單後重讀 rides/active 補齊。
	if _, ok := data["stops"]; ok {
		t.Errorf("data 不該帶 stops（推播保持精簡）：%v", data)
	}
}

// TestRideOfferPushData_NoDropoff 未指定目的地時省略 dropoff 三鍵（缺鍵＝沒有該資訊）。
func TestRideOfferPushData_NoDropoff(t *testing.T) {
	ride := &model.Ride{
		ID:          7,
		PickupPoint: model.GeoPoint{Lat: 25.0, Lng: 121.0},
	}
	data := rideOfferPushData(ride, "上車點", 60, 100)

	for _, k := range []string{"dropoff_address", "dropoff_lat", "dropoff_lng"} {
		if _, ok := data[k]; ok {
			t.Errorf("未指定目的地時不該帶 %q：%v", k, data)
		}
	}
	if data["type"] != "ride.assigned" || data["ride_id"] != "7" {
		t.Errorf("必要欄位缺失：%v", data)
	}
}
