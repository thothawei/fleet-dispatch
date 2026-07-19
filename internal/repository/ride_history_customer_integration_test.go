package repository

import (
	"testing"
	"time"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/model"
)

// TestListRecentByCustomer 乘客「我的行程」歷史：只回本人、新到舊、有司機帶司機名。
func TestListRecentByCustomer(t *testing.T) {
	db := newMigratedTestDB(t)
	rideRepo := NewRideRepository(db)
	custRepo := NewCustomerRepository(db)
	driverRepo := NewDriverRepository(db)

	me, err := custRepo.FindOrCreateByLineUserID("U_hist_me", "本人")
	if err != nil {
		t.Fatalf("建立客戶失敗: %v", err)
	}
	other, err := custRepo.FindOrCreateByLineUserID("U_hist_other", "別人")
	if err != nil {
		t.Fatalf("建立另一客戶失敗: %v", err)
	}
	driver, err := driverRepo.FindOrCreate("U_hist_driver", "阿明司機")
	if err != nil {
		t.Fatalf("建立司機失敗: %v", err)
	}

	day := func(s string) time.Time {
		d, _ := time.Parse("2006-01-02", s)
		return d
	}
	// Create 的 raw INSERT 只寫基本欄位（driver_id／fare 由 Accept/Complete 後填），
	// 故 withDriver 直接 UPDATE 補上，隔離測 ListRecentByCustomer 的 JOIN／投影。
	mk := func(cust int64, addr string, status int16, when time.Time, withDriver bool) int64 {
		ride := &model.Ride{
			CustomerID:     cust,
			Status:         status,
			PickupPoint:    model.GeoPoint{Lat: 25.03, Lng: 121.56},
			PickupAddress:  addr,
			DropoffAddress: "某目的地",
			RequestedAt:    when,
			CreatedAt:      when,
			UpdatedAt:      when,
		}
		if err := rideRepo.Create(ride); err != nil {
			t.Fatalf("建立訂單 %s 失敗: %v", addr, err)
		}
		if withDriver {
			if err := db.Exec("UPDATE rides SET driver_id = ?, fare_amount_cents = ? WHERE id = ?",
				driver.ID, 21500, ride.ID).Error; err != nil {
				t.Fatalf("設定司機/車資失敗: %v", err)
			}
		}
		return ride.ID
	}

	// 本人 2 筆（一筆有司機已完成、一筆取消無司機）＋別人 1 筆。
	mk(me.ID, "本人-舊", constants.RideStatusCompleted, day("2026-07-01"), true)
	newestID := mk(me.ID, "本人-新", constants.RideStatusCancelled, day("2026-07-05"), false)
	mk(other.ID, "別人的", constants.RideStatusCompleted, day("2026-07-03"), true)

	rows, err := rideRepo.ListRecentByCustomer(me.ID, 0)
	if err != nil {
		t.Fatalf("ListRecentByCustomer 失敗: %v", err)
	}

	// 只回本人 2 筆，不含別人。
	if len(rows) != 2 {
		t.Fatalf("應只回本人 2 筆，得 %d：%+v", len(rows), rows)
	}
	// 新到舊：第一筆是最新那筆。
	if rows[0].ID != newestID || rows[0].PickupAddress != "本人-新" {
		t.Fatalf("應新到舊，首筆為本人-新，得 %+v", rows[0])
	}
	// 取消且無司機：driver_id/driver_name 皆為 nil → 前端不顯示「聯絡司機」。
	if rows[0].DriverID != nil || rows[0].DriverName != nil {
		t.Fatalf("無司機的行程不該帶 driver 欄位，得 driver_id=%v name=%v", rows[0].DriverID, rows[0].DriverName)
	}
	// 有司機的舊行程：driver_name 帶出（LEFT JOIN drivers）。
	if rows[1].DriverName == nil || *rows[1].DriverName != "阿明司機" {
		t.Fatalf("有司機的行程應帶司機名，得 %+v", rows[1])
	}
	if rows[1].FareAmountCents == nil || *rows[1].FareAmountCents != 21500 {
		t.Fatalf("車資應帶出，得 %+v", rows[1].FareAmountCents)
	}
}

// TestListRecentByCustomer_limit 限制筆數。
func TestListRecentByCustomer_limit(t *testing.T) {
	db := newMigratedTestDB(t)
	rideRepo := NewRideRepository(db)
	custRepo := NewCustomerRepository(db)

	me, err := custRepo.FindOrCreateByLineUserID("U_hist_limit", "本人")
	if err != nil {
		t.Fatalf("建立客戶失敗: %v", err)
	}
	for i := 0; i < 5; i++ {
		ride := &model.Ride{
			CustomerID:    me.ID,
			Status:        constants.RideStatusCompleted,
			PickupPoint:   model.GeoPoint{Lat: 25.03, Lng: 121.56},
			PickupAddress: "行程",
			RequestedAt:   time.Now(),
			CreatedAt:     time.Now(),
			UpdatedAt:     time.Now(),
		}
		if err := rideRepo.Create(ride); err != nil {
			t.Fatalf("建立訂單失敗: %v", err)
		}
	}

	rows, err := rideRepo.ListRecentByCustomer(me.ID, 3)
	if err != nil {
		t.Fatalf("ListRecentByCustomer 失敗: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("limit=3 應回 3 筆，得 %d", len(rows))
	}
}
