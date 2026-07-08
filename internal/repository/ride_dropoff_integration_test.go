package repository

import (
	"testing"
	"time"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/model"
)

// TestCreateWithDropoff_寫入並讀回 建立帶目的地的訂單後，GetDropoffCoords 應回相同座標與 dropoff_address。
func TestCreateWithDropoff_寫入並讀回(t *testing.T) {
	db := newMigratedTestDB(t) // Docker 不可用時內部 t.Skip
	rides := NewRideRepository(db)
	cust, err := NewCustomerRepository(db).FindOrCreateByLineUserID("U_dropoff_rt", "測試乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}

	now := time.Now()
	ride := &model.Ride{
		CustomerID:     cust.ID,
		Status:         constants.RideStatusRequested,
		PickupPoint:    model.GeoPoint{Lat: 25.03, Lng: 121.56},
		PickupAddress:  "台北車站",
		DropoffPoint:   &model.GeoPoint{Lat: 25.08, Lng: 121.57},
		DropoffAddress: "松山機場",
		RequestedAt:    now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := rides.Create(ride); err != nil {
		t.Fatalf("建立訂單失敗：%v", err)
	}

	lat, lng, ok, err := rides.GetDropoffCoords(ride.ID)
	if err != nil {
		t.Fatalf("讀取目的地座標失敗：%v", err)
	}
	if !ok {
		t.Fatalf("預期有目的地座標，卻得到 ok=false")
	}
	if !approxEq(lat, 25.08) || !approxEq(lng, 121.57) {
		t.Fatalf("目的地座標不符：得到 (%f, %f)，預期 (25.08, 121.57)", lat, lng)
	}

	got, err := rides.GetByID(ride.ID)
	if err != nil {
		t.Fatalf("讀取訂單失敗：%v", err)
	}
	if got.DropoffAddress != "松山機場" {
		t.Fatalf("dropoff_address 不符：得到 %q", got.DropoffAddress)
	}
}

// TestCreateWithoutDropoff_座標為NULL 未指定目的地時 dropoff_point 應為 NULL、GetDropoffCoords 回 ok=false。
func TestCreateWithoutDropoff_座標為NULL(t *testing.T) {
	db := newMigratedTestDB(t)
	rides := NewRideRepository(db)
	cust, err := NewCustomerRepository(db).FindOrCreateByLineUserID("U_dropoff_null", "測試乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}

	now := time.Now()
	ride := &model.Ride{
		CustomerID:    cust.ID,
		Status:        constants.RideStatusRequested,
		PickupPoint:   model.GeoPoint{Lat: 25.03, Lng: 121.56},
		PickupAddress: "台北車站",
		RequestedAt:   now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := rides.Create(ride); err != nil {
		t.Fatalf("建立訂單失敗：%v", err)
	}

	_, _, ok, err := rides.GetDropoffCoords(ride.ID)
	if err != nil {
		t.Fatalf("讀取目的地座標失敗：%v", err)
	}
	if ok {
		t.Fatalf("未指定目的地時預期 ok=false")
	}
}

func approxEq(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 1e-6
}
