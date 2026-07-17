package service

import (
	"testing"

	"line-fleet-dispatch/internal/constants"
)

// newFeeSettingsWithCleaning 白箱組出含清潔費率的快取。
func newFeeSettingsWithCleaning(base, perKm, minFare int64, bps, cleaningBps int) *FeeSettings {
	return &FeeSettings{
		baseFareCents:     base,
		perKmFareCents:    perKm,
		minFareCents:      minFare,
		commissionBps:     bps,
		petCleaningFeeBps: cleaningBps,
	}
}

// TestQuote寵物車清潔費 O6 的算式：清潔費依 fare 比例、捨去到整數元，且**不計入抽成**。
func TestQuote寵物車清潔費(t *testing.T) {
	// 起步 85 元、每公里 20 元、手續費 15%、清潔費 20%。
	fs := newFeeSettingsWithCleaning(8500, 2000, 8500, 1500, 2000)

	// 5km → fare = 8500 + 20*5 元 = 18500
	q := fs.Quote(5000, constants.VehicleTypePet)

	if q.FareCents != 18500 {
		t.Fatalf("fare 應為 18500，得到 %d", q.FareCents)
	}
	// 清潔費 = floorNtd(18500 * 2000/10000) = floorNtd(3700) = 3700
	if q.CleaningFeeCents != 3700 {
		t.Fatalf("清潔費應為 3700，得到 %d", q.CleaningFeeCents)
	}
	// 抽成基準**只有 fare**：floorNtd(18500*0.15=2775) = 2700。
	// 若誤把清潔費計入基準，會變成 floorNtd(22200*0.15=3330)=3300。
	if q.CommissionCents != 2700 {
		t.Fatalf("手續費基準應只含車資（預期 2700），得到 %d——清潔費不計入抽成", q.CommissionCents)
	}
	// 清潔費全額歸司機：18500 − 2700 + 3700 = 19500
	if q.DriverNetCents != 19500 {
		t.Fatalf("司機實得應為 19500（fare−commission+cleaning），得到 %d", q.DriverNetCents)
	}
	for label, v := range map[string]int64{
		"fare": q.FareCents, "commission": q.CommissionCents,
		"cleaning": q.CleaningFeeCents, "net": q.DriverNetCents,
	} {
		if v%100 != 0 {
			t.Fatalf("%s=%d 不是整數元（應為 100 的倍數）", label, v)
		}
	}
}

// TestQuote清潔費只看乘客指定車種 O6 最關鍵的語意：加收依 rides.required_vehicle_type，
// **不是** drivers.vehicle_type。乘客沒指定寵物車、卻剛好被派到寵物車司機時不得加收——
// 那位乘客沒有要求寵物服務。Quote 只吃乘客指定的車種，司機車種根本傳不進來。
func TestQuote清潔費只看乘客指定車種(t *testing.T) {
	fs := newFeeSettingsWithCleaning(8500, 2000, 8500, 1500, 2000)

	for _, vType := range []string{"", constants.VehicleTypeSedan, constants.VehicleTypeSUV,
		constants.VehicleTypeVan7, constants.VehicleTypeAccessible} {
		q := fs.Quote(5000, vType)
		if q.CleaningFeeCents != 0 {
			t.Fatalf("乘客指定 %q 時不該有清潔費，得到 %d", vType, q.CleaningFeeCents)
		}
		// 沒有清潔費時，回到原本的等式。
		if q.DriverNetCents != q.FareCents-q.CommissionCents {
			t.Fatalf("無清潔費時 net 應為 fare−commission，得到 %d", q.DriverNetCents)
		}
	}
}

// TestQuote清潔費率為零 後台未設定清潔費（預設 0）時，指定寵物車也不加收。
func TestQuote清潔費率為零(t *testing.T) {
	fs := newFeeSettingsWithCleaning(8500, 2000, 8500, 1500, 0)
	q := fs.Quote(5000, constants.VehicleTypePet)
	if q.CleaningFeeCents != 0 {
		t.Fatalf("費率為 0 時不該加收，得到 %d", q.CleaningFeeCents)
	}
}

// TestQuote清潔費捨去到整數元 台幣無小數：清潔費捨去（利乘客）。
func TestQuote清潔費捨去到整數元(t *testing.T) {
	// fare = 8500；清潔費 = 8500 * 777/10000 = 660.45 分 → floorNtd → 600（NT$6）
	fs := newFeeSettingsWithCleaning(8500, 2000, 8500, 1500, 777)
	q := fs.Quote(0, constants.VehicleTypePet)
	if q.FareCents != 8500 {
		t.Fatalf("fare 應為 8500，得到 %d", q.FareCents)
	}
	if q.CleaningFeeCents != 600 {
		t.Fatalf("清潔費應捨去到 600，得到 %d", q.CleaningFeeCents)
	}
}

// TestCustomerJSON白名單 P5：乘客可讀的費率絕不可外洩內部費率。
func TestCustomerJSON白名單(t *testing.T) {
	fs := &FeeSettings{
		baseFareCents:             8500,
		perKmFareCents:            2000,
		minFareCents:              8500,
		commissionBps:             1500,
		monthlyMembershipFeeCents: 300000,
		lostItemFeeBps:            1000,
		petCleaningFeeBps:         2000,
	}
	got := fs.CustomerJSON()

	if got["pet_cleaning_fee_bps"] != 2000 {
		t.Fatalf("乘客應讀得到清潔費率，得到 %v", got)
	}
	// 白名單的重點：內部費率一個都不能出現。
	for _, leaked := range []string{
		"commission_bps",               // 手續費——乘客不該知道平台抽多少
		"monthly_membership_fee_cents", // 月會費——司機與平台之間的事
		"base_fare_cents", "per_km_fare_cents", "min_fare_cents", "lost_item_fee_bps",
	} {
		if _, ok := got[leaked]; ok {
			t.Fatalf("CustomerJSON 外洩了內部費率 %q：%v", leaked, got)
		}
	}
	if len(got) != 1 {
		t.Fatalf("CustomerJSON 應只有白名單欄位，得到 %v", got)
	}
}
