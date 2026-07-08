package service

import (
	"math"
	"testing"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/repository"
)

// TestGeoPointReadRoundTrip 對真 PostGIS 驗證：寫入的 pickup_point 經 GORM 讀回不再是零值。
// 這是 GeoPoint.Scan（原為 no-op）修復後的端到端證據，補住乘客 GET /api/customer/rides/:id
// 回傳 (0,0) 的資料層缺口。
func TestGeoPointReadRoundTrip(t *testing.T) {
	db := newServiceTestDB(t)
	customers := repository.NewCustomerRepository(db)
	rides := repository.NewRideRepository(db)

	cust, err := customers.FindOrCreateByLineUserID("U_geo_roundtrip", "座標測試乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	ride := newTestRide(t, rides, cust.ID, constants.RideStatusRequested)

	got, err := rides.GetByID(ride.ID)
	if err != nil {
		t.Fatalf("讀回訂單失敗：%v", err)
	}
	// newTestRide 寫入 lat=25.03, lng=121.56
	if math.Abs(got.PickupPoint.Lat-25.03) > 1e-6 || math.Abs(got.PickupPoint.Lng-121.56) > 1e-6 {
		t.Fatalf("pickup_point 讀回錯誤：lat=%f lng=%f（預期 25.03, 121.56）",
			got.PickupPoint.Lat, got.PickupPoint.Lng)
	}
}
