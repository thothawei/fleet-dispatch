package repository

import (
	"testing"
	"time"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/model"
)

func TestFindActiveByCustomer_守門(t *testing.T) {
	db := newMigratedTestDB(t) // Docker 不可用時內部 t.Skip
	customers := NewCustomerRepository(db)
	rides := NewRideRepository(db)

	cust, err := customers.FindOrCreateByLineUserID("U_active_guard", "測試乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}

	// 尚無訂單 → 回 (nil, nil)
	got, err := rides.FindActiveByCustomer(cust.ID)
	if err != nil {
		t.Fatalf("查詢失敗：%v", err)
	}
	if got != nil {
		t.Fatalf("預期無進行中訂單，卻得到 ride id=%d", got.ID)
	}

	// 建一筆 Requested → 應被視為進行中
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
	got, err = rides.FindActiveByCustomer(cust.ID)
	if err != nil {
		t.Fatalf("查詢失敗：%v", err)
	}
	if got == nil || got.ID != ride.ID {
		t.Fatalf("預期回進行中訂單 id=%d，得到 %v", ride.ID, got)
	}
}
