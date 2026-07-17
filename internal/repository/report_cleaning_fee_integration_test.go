package repository

import (
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/model"
)

// TestCleaningFeeReports O6 的報表分項：清潔費不進營業額、不進抽成，但含在司機實得裡。
// 這是 F5/F6/F7 與 daily_driver_earnings 的一致性——少了分項欄，司機收入頁的
// 「營業額 − 手續費」會對不上實得（差額正是清潔費）。
func TestCleaningFeeReports(t *testing.T) {
	db := newMigratedTestDB(t)
	customers := NewCustomerRepository(db)
	reports := NewReportRepository(db)

	cust, err := customers.FindOrCreateByLineUserID("U_clean", "寵物乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	driver := &model.Driver{
		LineUserID: "D_clean", Name: "寵物車司機", Status: 0,
		VehicleType: constants.VehicleTypePet, PlateNumber: "CLEAN-01",
		CreatedAt: billTestDay, UpdatedAt: billTestDay,
	}
	if err := db.Create(driver).Error; err != nil {
		t.Fatalf("建立司機失敗：%v", err)
	}

	// 兩筆已完成行程：一筆乘客指定寵物車（有清潔費），一筆沒指定（無清潔費）。
	// 兩筆都由同一位寵物車司機完成——這正是「依乘客指定車種、不依司機車種」的分界。
	type rideSpec struct {
		requiredType          string
		fare, commission, net int64
		cleaning              int64
	}
	specs := []rideSpec{
		// 指定 pet：fare 18500、手續費 2700（基準只有 fare）、清潔費 3700、實得 18500−2700+3700
		{constants.VehicleTypePet, 18500, 2700, 19500, 3700},
		// 未指定：無清潔費，實得 = fare − commission
		{"", 8500, 1200, 7300, 0},
	}
	for _, s := range specs {
		fare, commission, net, cleaning := s.fare, s.commission, s.net, s.cleaning
		ride := &model.Ride{
			CustomerID:            cust.ID,
			DriverID:              &driver.ID,
			Status:                constants.RideStatusCompleted,
			PickupPoint:           model.GeoPoint{Lat: 25.03, Lng: 121.56},
			PickupAddress:         "台北車站",
			RequiredVehicleType:   s.requiredType,
			RequestedAt:           billTestDay,
			CompletedAt:           &billTestDay,
			DistanceM:             ptrInt(5000),
			FareAmountCents:       &fare,
			CommissionAmountCents: &commission,
			DriverNetAmountCents:  &net,
			CleaningFeeCents:      &cleaning,
			CreatedAt:             billTestDay,
			UpdatedAt:             billTestDay,
		}
		if err := db.Create(ride).Error; err != nil {
			t.Fatalf("建立已完成行程失敗：%v", err)
		}
		if err := reports.RollupRideDay(ride.ID); err != nil {
			t.Fatalf("每日彙總 rollup 失敗：%v", err)
		}
	}

	wantRevenue := int64(18500 + 8500)   // 27000——**不含**清潔費
	wantCommission := int64(2700 + 1200) // 3900——基準只有車資
	wantCleaning := int64(3700)          // 只有指定 pet 的那筆
	wantNet := int64(19500 + 7300)       // 26800——含清潔費

	t.Run("月報表分項", func(t *testing.T) {
		monthly, err := reports.MonthlyDriverStats(billTestMonth)
		if err != nil {
			t.Fatalf("月報表查詢失敗：%v", err)
		}
		if len(monthly) != 1 {
			t.Fatalf("預期 1 位司機，得到 %d", len(monthly))
		}
		m := monthly[0]
		if m.TotalRevenueCents != wantRevenue {
			t.Fatalf("營業額不該含清潔費：得到 %d，預期 %d", m.TotalRevenueCents, wantRevenue)
		}
		if m.TotalCommissionCents != wantCommission {
			t.Fatalf("手續費不符：得到 %d，預期 %d", m.TotalCommissionCents, wantCommission)
		}
		if m.TotalCleaningFeeCents != wantCleaning {
			t.Fatalf("清潔費分項不符：得到 %d，預期 %d", m.TotalCleaningFeeCents, wantCleaning)
		}
		if m.DriverNetCents != wantNet {
			t.Fatalf("司機實得應含清潔費：得到 %d，預期 %d", m.DriverNetCents, wantNet)
		}
		// 應付總公司＝手續費＋月會費，**不受清潔費影響**（本測試無會費帳單 → 0）。
		if m.OwedToHqCents != wantCommission {
			t.Fatalf("應付總公司不該受清潔費影響：得到 %d，預期 %d", m.OwedToHqCents, wantCommission)
		}
	})

	t.Run("司機收入分項與等式", func(t *testing.T) {
		earn, err := reports.DriverMonthlyEarnings(driver.ID, billTestMonth)
		if err != nil {
			t.Fatalf("司機收入查詢失敗：%v", err)
		}
		if earn.TotalRevenueCents != wantRevenue || earn.TotalCommissionCents != wantCommission ||
			earn.TotalCleaningFeeCents != wantCleaning || earn.DriverNetCents != wantNet {
			t.Fatalf("司機收入分項不符：%+v", earn)
		}
		// 司機收入頁靠這條等式呈現；少了清潔費分項就對不起來。
		if got := earn.TotalRevenueCents - earn.TotalCommissionCents + earn.TotalCleaningFeeCents; got != earn.DriverNetCents {
			t.Fatalf("等式失效：營業額 %d − 手續費 %d + 清潔費 %d = %d，但實得為 %d",
				earn.TotalRevenueCents, earn.TotalCommissionCents, earn.TotalCleaningFeeCents,
				got, earn.DriverNetCents)
		}
	})
}

// TestFleetSettingsPetCleaningFeeCap O6：上限 30% 寫進 DB CHECK，不只靠 API 驗證。
// 這是乘客實際被收的錢，設定寫錯的後果是超收。
func TestFleetSettingsPetCleaningFeeCap(t *testing.T) {
	db := newMigratedTestDB(t)

	// 邊界值 3000（30%）應可寫入。
	if err := db.Exec(`UPDATE fleet_settings SET pet_cleaning_fee_bps = 3000 WHERE id = 1`).Error; err != nil {
		t.Fatalf("3000 bps（上限）應為合法值：%v", err)
	}
	err := db.Exec(`UPDATE fleet_settings SET pet_cleaning_fee_bps = 3001 WHERE id = 1`).Error
	if err == nil {
		t.Fatal("超過 30% 的清潔費率不該寫得進去")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "chk_fleet_settings_pet_cleaning_fee_bps") {
		t.Fatalf("應由 chk_fleet_settings_pet_cleaning_fee_bps 擋下，實際錯誤：%v", err)
	}
	if err := db.Exec(`UPDATE fleet_settings SET pet_cleaning_fee_bps = -1 WHERE id = 1`).Error; err == nil {
		t.Fatal("負的清潔費率不該寫得進去")
	}
}

// TestPetCleaningFeeMigrationReversible P1 之後的第二個 migration：驗 000019 可逆。
func TestPetCleaningFeeMigrationReversible(t *testing.T) {
	db, m := newMigrateHandle(t)
	defer m.Close()

	hasColumn := func(table, col string) bool {
		var n int64
		db.Raw(`SELECT count(*) FROM information_schema.columns
		        WHERE table_name=? AND column_name=?`, table, col).Scan(&n)
		return n > 0
	}
	hasConstraint := func(name string) bool {
		var n int64
		db.Raw(`SELECT count(*) FROM pg_constraint WHERE conname=?`, name).Scan(&n)
		return n > 0
	}

	for _, tc := range []struct{ table, col string }{
		{"fleet_settings", "pet_cleaning_fee_bps"},
		{"rides", "cleaning_fee_cents"},
		{"daily_driver_earnings", "cleaning_fee_cents"},
	} {
		if !hasColumn(tc.table, tc.col) {
			t.Fatalf("up 後 %s 應有欄位 %s", tc.table, tc.col)
		}
	}
	if !hasConstraint("chk_fleet_settings_pet_cleaning_fee_bps") {
		t.Fatal("up 後應有清潔費上限 CHECK")
	}

	// 以版本表達意圖（比照 P1）：日後在 O6 之後再加 migration，這個測試仍驗 O6 的 down。
	if err := m.Migrate(18); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("down 到 version 18 失敗：%v", err)
	}
	for _, tc := range []struct{ table, col string }{
		{"fleet_settings", "pet_cleaning_fee_bps"},
		{"rides", "cleaning_fee_cents"},
		{"daily_driver_earnings", "cleaning_fee_cents"},
	} {
		if hasColumn(tc.table, tc.col) {
			t.Fatalf("down 後 %s 不應還有欄位 %s", tc.table, tc.col)
		}
	}
	if hasConstraint("chk_fleet_settings_pet_cleaning_fee_bps") {
		t.Fatal("down 後不應還有清潔費 CHECK")
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("再次 up 失敗：%v", err)
	}
	if !hasColumn("rides", "cleaning_fee_cents") || !hasConstraint("chk_fleet_settings_pet_cleaning_fee_bps") {
		t.Fatal("再次 up 後 O6 的欄位與 CHECK 應全部回來")
	}
}
