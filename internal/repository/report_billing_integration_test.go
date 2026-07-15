package repository

import (
	"testing"
	"time"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/model"
)

// 用固定 UTC 時間，避開 completed_at::date 在容器（UTC session）與本機時區的邊界 flaky。
var billTestDay = time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)

const (
	billTestDate  = "2026-07-15"
	billTestMonth = "2026-07"
)

// TestBillingReports 驗證計費欄位 + 日/月報表 + 司機收入聚合（F2/F3/F5/F6/F7）與 migration seed（F1）。
func TestBillingReports(t *testing.T) {
	db := newMigratedTestDB(t) // Docker 不可用時內部 t.Skip

	// F1：migration 應種下單列預設費率設定。
	fees := NewFeeSettingsRepository(db)
	fs, err := fees.Get()
	if err != nil {
		t.Fatalf("讀 fleet_settings 失敗：%v", err)
	}
	if fs.CommissionBps != 1500 || fs.BaseFareCents != 8500 || fs.MonthlyMembershipFeeCents != 300000 {
		t.Fatalf("預設費率不符：bps=%d base=%d membership=%d", fs.CommissionBps, fs.BaseFareCents, fs.MonthlyMembershipFeeCents)
	}

	customers := NewCustomerRepository(db)
	reports := NewReportRepository(db)

	cust, err := customers.FindOrCreateByLineUserID("U_bill", "計費乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	driver := &model.Driver{LineUserID: "D_bill", Name: "計費司機", Status: 0, CreatedAt: billTestDay, UpdatedAt: billTestDay}
	if err := db.Create(driver).Error; err != nil {
		t.Fatalf("建立司機失敗：%v", err)
	}

	// 兩筆已完成行程，直接帶入定格車資（模擬 F3 完成計費的結果；金額皆整數元，手續費已捨去）。
	fares := []struct{ fare, commission, net int64 }{
		{18500, 2700, 15800}, // 5km；手續費 15%=2775 捨去到 2700
		{8500, 1200, 7300},   // 最低車資；手續費 1275 捨去到 1200
	}
	for _, f := range fares {
		fare, commission, net := f.fare, f.commission, f.net
		ride := &model.Ride{
			CustomerID:            cust.ID,
			DriverID:              &driver.ID,
			Status:                constants.RideStatusCompleted,
			PickupPoint:           model.GeoPoint{Lat: 25.03, Lng: 121.56},
			PickupAddress:         "台北車站",
			RequestedAt:           billTestDay,
			CompletedAt:           &billTestDay,
			DistanceM:             ptrInt(5000),
			FareAmountCents:       &fare,
			CommissionAmountCents: &commission,
			DriverNetAmountCents:  &net,
			CreatedAt:             billTestDay,
			UpdatedAt:             billTestDay,
		}
		if err := db.Create(ride).Error; err != nil {
			t.Fatalf("建立已完成行程失敗：%v", err)
		}
		// F9-3：月報表/司機收入改讀預聚合表，直接建的行程需觸發 rollup 填彙總（模擬完成時的重算）。
		if err := reports.RollupRideDay(ride.ID); err != nil {
			t.Fatalf("每日彙總 rollup 失敗：%v", err)
		}
	}

	wantRevenue := int64(18500 + 8500)   // 27000
	wantCommission := int64(2700 + 1200) // 3900
	wantNet := int64(15800 + 7300)       // 23100

	// F5 日報表
	daily, err := reports.DailyDriverStats(billTestDate)
	if err != nil {
		t.Fatalf("日報表查詢失敗：%v", err)
	}
	if len(daily) != 1 {
		t.Fatalf("日報表預期 1 位司機，得到 %d", len(daily))
	}
	if daily[0].TripCount != 2 || daily[0].TotalRevenueCents != wantRevenue ||
		daily[0].TotalCommissionCents != wantCommission || daily[0].DriverNetCents != wantNet {
		t.Fatalf("日報表金額不符：%+v", daily[0])
	}

	// F6 月報表（repo 只回聚合，會費由 handler 補）
	monthly, err := reports.MonthlyDriverStats(billTestMonth)
	if err != nil {
		t.Fatalf("月報表查詢失敗：%v", err)
	}
	if len(monthly) != 1 || monthly[0].TotalRevenueCents != wantRevenue ||
		monthly[0].TotalCommissionCents != wantCommission || monthly[0].DriverNetCents != wantNet {
		t.Fatalf("月報表金額不符：%+v", monthly)
	}

	// F7 司機收入
	earn, err := reports.DriverMonthlyEarnings(driver.ID, billTestMonth)
	if err != nil {
		t.Fatalf("司機收入查詢失敗：%v", err)
	}
	if earn.TripCount != 2 || earn.TotalRevenueCents != wantRevenue ||
		earn.TotalCommissionCents != wantCommission || earn.DriverNetCents != wantNet {
		t.Fatalf("司機收入不符：%+v", earn)
	}

	// 別的月份應為 0（驗半開區間邊界）
	empty, err := reports.DriverMonthlyEarnings(driver.ID, "2026-08")
	if err != nil {
		t.Fatalf("空月查詢失敗：%v", err)
	}
	if empty.TripCount != 0 || empty.TotalRevenueCents != 0 {
		t.Fatalf("空月應為 0，得到 %+v", empty)
	}

	// F9-3 冪等性：重跑 rollup（重算整桶）不應改變彙總。
	var anyRideID int64
	if err := db.Raw("SELECT id FROM rides WHERE driver_id = ? LIMIT 1", driver.ID).Scan(&anyRideID).Error; err != nil || anyRideID == 0 {
		t.Fatalf("取行程 id 失敗：%v", err)
	}
	if err := reports.RollupRideDay(anyRideID); err != nil {
		t.Fatalf("重跑 rollup 失敗：%v", err)
	}
	monthly2, err := reports.MonthlyDriverStats(billTestMonth)
	if err != nil {
		t.Fatalf("重跑後月報表查詢失敗：%v", err)
	}
	if len(monthly2) != 1 || monthly2[0].TripCount != 2 || monthly2[0].TotalRevenueCents != wantRevenue {
		t.Fatalf("重跑 rollup 後月報表變了（應冪等）：%+v", monthly2)
	}
}

// TestCompleteRideSnapshotsFare 驗證 CompleteRide 會定格寫入計費欄位（F2/F3）。
func TestCompleteRideSnapshotsFare(t *testing.T) {
	db := newMigratedTestDB(t)
	customers := NewCustomerRepository(db)
	rides := NewRideRepository(db)

	cust, err := customers.FindOrCreateByLineUserID("U_snap", "快照乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	driver := &model.Driver{LineUserID: "D_snap", Name: "快照司機", CreatedAt: billTestDay, UpdatedAt: billTestDay}
	if err := db.Create(driver).Error; err != nil {
		t.Fatalf("建立司機失敗：%v", err)
	}
	ride := &model.Ride{
		CustomerID:    cust.ID,
		DriverID:      &driver.ID,
		Status:        constants.RideStatusPickedUp, // CompleteRide 只認 PickedUp → Completed
		PickupPoint:   model.GeoPoint{Lat: 25.03, Lng: 121.56},
		PickupAddress: "台北車站",
		RequestedAt:   billTestDay,
		CreatedAt:     billTestDay,
		UpdatedAt:     billTestDay,
	}
	if err := db.Create(ride).Error; err != nil {
		t.Fatalf("建立行程失敗：%v", err)
	}

	fare, commission, net := int64(18500), int64(2700), int64(15800)
	if err := rides.CompleteRide(ride.ID, 5000, &fare, &commission, &net); err != nil {
		t.Fatalf("CompleteRide 失敗：%v", err)
	}

	got, err := rides.GetByID(ride.ID)
	if err != nil {
		t.Fatalf("讀回行程失敗：%v", err)
	}
	if got.Status != constants.RideStatusCompleted {
		t.Fatalf("狀態應為已完成，得到 %d", got.Status)
	}
	if got.FareAmountCents == nil || *got.FareAmountCents != fare ||
		got.CommissionAmountCents == nil || *got.CommissionAmountCents != commission ||
		got.DriverNetAmountCents == nil || *got.DriverNetAmountCents != net {
		t.Fatalf("計費欄位未正確定格：%+v", got)
	}
}

func ptrInt(v int) *int { return &v }
