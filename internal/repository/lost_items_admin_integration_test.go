package repository

import (
	"testing"
	"time"

	"gorm.io/gorm"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/model"
)

func seedRideForLostItem(t *testing.T, db *gorm.DB, driverID, custID int64) int64 {
	t.Helper()
	now := time.Now()
	ride := &model.Ride{
		CustomerID:    custID,
		DriverID:      &driverID,
		Status:        constants.RideStatusCompleted,
		PickupPoint:   model.GeoPoint{Lat: 25.03, Lng: 121.56},
		PickupAddress: "台北車站",
		RequestedAt:   now,
		CompletedAt:   &now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := db.Create(ride).Error; err != nil {
		t.Fatalf("建立已完成行程失敗：%v", err)
	}
	return ride.ID
}

// TestLostItemAdminList 驗證後台總覽 ListAll：JOIN 司機/乘客姓名、狀態篩選、新的在前。
func TestLostItemAdminList(t *testing.T) {
	db := newMigratedTestDB(t) // Docker 不可用時內部 t.Skip

	customers := NewCustomerRepository(db)
	items := NewLostItemRepository(db)

	cust, err := customers.FindOrCreateByLineUserID("U_lost_admin", "協尋乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	driverID := newDriver(t, db, "D_lost_admin", "協尋司機")

	mk := func(status string, feeCents int64) int64 {
		rideID := seedRideForLostItem(t, db, driverID, cust.ID)
		item := &model.LostItemRequest{
			RideID: rideID, CustomerID: cust.ID, DriverID: driverID,
			Description: "黑色雨傘", FeeCents: feeCents, FeeBps: 1000, Status: status,
		}
		if err := items.Create(item); err != nil {
			t.Fatalf("建立協尋單（%s）失敗：%v", status, err)
		}
		return item.ID
	}

	openID := mk(constants.LostItemStatusOpen, 850)
	foundID := mk(constants.LostItemStatusFound, 1200)
	mk(constants.LostItemStatusReturned, 900)

	// 全部：3 筆、新的在前、姓名 JOIN 正確
	rows, err := items.ListAll("")
	if err != nil {
		t.Fatalf("ListAll 失敗：%v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("預期 3 筆，實得 %d", len(rows))
	}
	if rows[0].ID <= rows[1].ID || rows[1].ID <= rows[2].ID {
		t.Fatalf("預期新的在前（id 遞減），實得 %d, %d, %d", rows[0].ID, rows[1].ID, rows[2].ID)
	}
	for _, r := range rows {
		if r.CustomerName != "協尋乘客" || r.DriverName != "協尋司機" {
			t.Fatalf("姓名 JOIN 錯誤：customer=%q driver=%q", r.CustomerName, r.DriverName)
		}
	}

	// 狀態篩選：open 只回 open 那筆
	openRows, err := items.ListAll(constants.LostItemStatusOpen)
	if err != nil {
		t.Fatalf("ListAll(open) 失敗：%v", err)
	}
	if len(openRows) != 1 || openRows[0].ID != openID {
		t.Fatalf("open 篩選預期只有 id=%d，實得 %+v", openID, openRows)
	}
	if openRows[0].FeeCents != 850 {
		t.Fatalf("open 單處理費預期 850，實得 %d", openRows[0].FeeCents)
	}

	foundRows, err := items.ListAll(constants.LostItemStatusFound)
	if err != nil {
		t.Fatalf("ListAll(found) 失敗：%v", err)
	}
	if len(foundRows) != 1 || foundRows[0].ID != foundID {
		t.Fatalf("found 篩選預期只有 id=%d，實得 %+v", foundID, foundRows)
	}
}
