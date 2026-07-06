package repository

import (
	"testing"
	"time"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/model"
)

func TestRideListRecent_依狀態篩選(t *testing.T) {
	db := newMigratedTestDB(t)
	rideRepo := NewRideRepository(db)
	custRepo := NewCustomerRepository(db)

	cust, err := custRepo.FindOrCreateByLineUserID("U_admin_test", "測試客")
	if err != nil {
		t.Fatalf("建立客戶失敗: %v", err)
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
	if err := rideRepo.Create(ride); err != nil {
		t.Fatalf("建立訂單失敗: %v", err)
	}

	rows, err := rideRepo.ListRecent(nil, 50)
	if err != nil {
		t.Fatalf("ListRecent 失敗: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("ListRecent 應至少回一筆")
	}

	st := int16(constants.RideStatusRequested)
	filtered, err := rideRepo.ListRecent(&st, 50)
	if err != nil {
		t.Fatalf("ListRecent(status) 失敗: %v", err)
	}
	for _, r := range filtered {
		if r.Status != constants.RideStatusRequested {
			t.Fatalf("篩選後出現非 REQUESTED 狀態: %+v", r)
		}
	}
}

func TestDriverListAll(t *testing.T) {
	db := newMigratedTestDB(t)
	repo := NewDriverRepository(db)
	if _, err := repo.CreateSimulated("後台測試司機", "U_admin_drv"); err != nil {
		t.Fatalf("建立司機失敗: %v", err)
	}
	all, err := repo.ListAll()
	if err != nil {
		t.Fatalf("ListAll 失敗: %v", err)
	}
	if len(all) == 0 {
		t.Fatal("ListAll 應至少回一筆")
	}
}
