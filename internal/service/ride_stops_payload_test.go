package service

import (
	"testing"
	"time"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/model"
)

func sampleStops() []model.RideStop {
	arrived := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	return []model.RideStop{
		{
			ID: 11, RideID: 5, Seq: 1, Kind: constants.StopKindPickup,
			Point:   model.GeoPoint{Lat: 25.0478, Lng: 121.5170},
			Address: "台北車站", PassengerLabel: "A", ArrivedAt: &arrived,
		},
		{
			ID: 12, RideID: 5, Seq: 2, Kind: constants.StopKindDropoff,
			Point:   model.GeoPoint{Lat: 25.0697, Lng: 121.5522},
			Address: "松山機場", PassengerLabel: "A",
		},
	}
}

// TestStopViews N4／N6：司機端要知道「第幾站、是誰、上車還下車、在哪」。
func TestStopViews(t *testing.T) {
	views := stopViews(sampleStops())
	if len(views) != 2 {
		t.Fatalf("預期 2 站，得到 %d", len(views))
	}

	first := views[0]
	if first["seq"] != 1 || first["kind"] != constants.StopKindPickup || first["passenger_label"] != "A" {
		t.Fatalf("第 1 站內容錯誤：%v", first)
	}
	// 座標攤平成 lat/lng，與 ride.assigned 既有的 pickup_lat/lng 一致，
	// App 端不必為 stops 另寫一套解析。
	if first["lat"] != 25.0478 || first["lng"] != 121.5170 {
		t.Fatalf("座標應攤平成 lat/lng：%v", first)
	}
	if first["address"] != "台北車站" {
		t.Fatalf("地址錯誤：%v", first)
	}
	// 已到達的站要帶時間，司機端據此顯示狀態。
	if _, ok := first["arrived_at"]; !ok {
		t.Fatalf("已到達的站應帶 arrived_at：%v", first)
	}
	// 未跳過就不帶該鍵。
	if _, ok := first["skipped_at"]; ok {
		t.Fatalf("未跳過不該帶 skipped_at：%v", first)
	}

	// 待處理的站兩個時間鍵都不帶。
	second := views[1]
	for _, k := range []string{"arrived_at", "skipped_at"} {
		if _, ok := second[k]; ok {
			t.Fatalf("待處理的站不該帶 %q：%v", k, second)
		}
	}
}

// TestStopViews_空回nil 單點訂單的 payload 不該多一個空 stops 陣列。
func TestStopViews_空回nil(t *testing.T) {
	if stopViews(nil) != nil {
		t.Fatal("nil 應回 nil")
	}
	if stopViews([]model.RideStop{}) != nil {
		t.Fatal("空 slice 應回 nil")
	}
}

// TestRideAssignedPayload_帶stops N4：司機**接單前**就要看得到全程，才知道要不要接。
func TestRideAssignedPayload_帶stops(t *testing.T) {
	ride := &model.Ride{
		PickupAddress: "台北車站",
		PickupPoint:   model.GeoPoint{Lat: 25.0478, Lng: 121.5170},
	}

	t.Run("多停靠點帶 stops", func(t *testing.T) {
		p := rideAssignedPayload(ride, ride.PickupAddress, 300, 1200, sampleStops())
		stops, ok := p["stops"].([]map[string]any)
		if !ok || len(stops) != 2 {
			t.Fatalf("應帶 2 站：%v", p["stops"])
		}
		// 既有欄位不得因為加了 stops 而變動。
		if p["pickup_lat"] != 25.0478 || p["eta_sec"] != 300 {
			t.Fatalf("既有欄位不該變：%v", p)
		}
	})

	t.Run("單點訂單不帶 stops 鍵", func(t *testing.T) {
		p := rideAssignedPayload(ride, ride.PickupAddress, 300, 1200, nil)
		if _, ok := p["stops"]; ok {
			t.Fatalf("單點訂單不該有 stops 鍵：%v", p)
		}
	})
}

// TestRideAcceptedDriverPayload_帶stops N6：接單後司機拿到全程，不必等 pickup 回應。
func TestRideAcceptedDriverPayload_帶stops(t *testing.T) {
	ride := &model.Ride{}

	p := rideAcceptedDriverPayload(ride, sampleStops())
	if stops, ok := p["stops"].([]map[string]any); !ok || len(stops) != 2 {
		t.Fatalf("應帶 2 站：%v", p["stops"])
	}

	if p := rideAcceptedDriverPayload(ride, nil); len(p) != 0 {
		t.Fatalf("單點無目的地訂單的 payload 應為空：%v", p)
	}
}
