package repository

import (
	"math"
	"testing"
	"time"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/model"
)

// TestRideDropoff_寫入讀回 建立帶目的地的訂單後，dropoff_point 座標與 dropoff_address 應原樣讀回。
//
// 這是 repository 層的直接覆蓋：service 層測試（ride_create_customer_test）走的是
// 建單流程，若 Create 的兩條 SQL 分支（有／無 dropoff）走錯，錯誤會被上層邏輯掩蓋。
func TestRideDropoff_寫入讀回(t *testing.T) {
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

	got, err := rides.GetByID(ride.ID)
	if err != nil {
		t.Fatalf("讀取訂單失敗：%v", err)
	}
	if got.DropoffPoint == nil {
		t.Fatal("預期有目的地座標，卻讀回 nil")
	}
	if math.Abs(got.DropoffPoint.Lat-25.08) > 1e-6 || math.Abs(got.DropoffPoint.Lng-121.57) > 1e-6 {
		t.Fatalf("目的地座標不符：得到 (%f, %f)", got.DropoffPoint.Lat, got.DropoffPoint.Lng)
	}
	if got.DropoffAddress != "松山機場" {
		t.Fatalf("dropoff_address 不符：得到 %q", got.DropoffAddress)
	}
}

// TestRideDropoff_未指定時為NULL 未指定目的地的訂單，DropoffPoint 必須讀回 nil 而非零值座標。
//
// 這條守的是 GeoPoint.Scan 曾經的坑：NULL 被當成「掃描成功」而留下 (0, 0)，
// 導航與計費就會把非洲外海的那個點當成真目的地，且**不會有任何錯誤**。
// 判斷「有沒有目的地」的唯一依據是指標是否為 nil，故此斷言不可弱化為座標比較。
func TestRideDropoff_未指定時為NULL(t *testing.T) {
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

	got, err := rides.GetByID(ride.ID)
	if err != nil {
		t.Fatalf("讀取訂單失敗：%v", err)
	}
	if got.DropoffPoint != nil {
		t.Fatalf("未指定目的地時 DropoffPoint 應為 nil，卻得到 (%f, %f)",
			got.DropoffPoint.Lat, got.DropoffPoint.Lng)
	}
}
