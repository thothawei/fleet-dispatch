package repository

import (
	"testing"
	"time"

	"gorm.io/gorm"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/model"
)

func newDriver(t *testing.T, db *gorm.DB, lineID, name string) int64 {
	t.Helper()
	d := &model.Driver{LineUserID: lineID, Name: name, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := db.Create(d).Error; err != nil {
		t.Fatalf("建立司機失敗：%v", err)
	}
	return d.ID
}

func seedCompletedRideAt(t *testing.T, db *gorm.DB, driverID, custID int64, at time.Time) {
	t.Helper()
	ride := &model.Ride{
		CustomerID:    custID,
		DriverID:      &driverID,
		Status:        constants.RideStatusCompleted,
		PickupPoint:   model.GeoPoint{Lat: 25.03, Lng: 121.56},
		PickupAddress: "台北車站",
		RequestedAt:   at,
		CompletedAt:   &at,
		CreatedAt:     at,
		UpdatedAt:     at,
	}
	if err := db.Create(ride).Error; err != nil {
		t.Fatalf("建立已完成行程失敗：%v", err)
	}
}

// TestMembershipInvoices 驗證 F8：只對當月有完成行程的司機開帳單、冪等、金額快照、狀態切換。
func TestMembershipInvoices(t *testing.T) {
	db := newMigratedTestDB(t) // Docker 不可用時內部 t.Skip

	customers := NewCustomerRepository(db)
	invoices := NewMembershipInvoiceRepository(db)

	cust, err := customers.FindOrCreateByLineUserID("U_mem", "會費乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}

	aug := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	sep := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)

	dA := newDriver(t, db, "D_A", "司機A")
	dB := newDriver(t, db, "D_B", "司機B")
	dSepOnly := newDriver(t, db, "D_S", "只有九月的司機")

	seedCompletedRideAt(t, db, dA, cust.ID, aug)
	seedCompletedRideAt(t, db, dA, cust.ID, aug) // A 同月兩趟 → 仍只一張帳單
	seedCompletedRideAt(t, db, dB, cust.ID, aug)
	seedCompletedRideAt(t, db, dSepOnly, cust.ID, sep) // 九月才有 → 八月不開帳單

	// 產生八月帳單：只有 A、B（各一張），金額快照 300000
	created, err := invoices.GenerateForMonth("2026-08", 300000)
	if err != nil {
		t.Fatalf("產生帳單失敗：%v", err)
	}
	if created != 2 {
		t.Fatalf("預期新建 2 張（A/B），實得 %d", created)
	}

	rows, err := invoices.List("2026-08", "")
	if err != nil {
		t.Fatalf("列帳單失敗：%v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("八月帳單預期 2 筆，實得 %d", len(rows))
	}
	for _, r := range rows {
		if r.AmountCents != 300000 || r.Status != "unpaid" || r.PaidAt != nil {
			t.Fatalf("帳單初始狀態不符：%+v", r)
		}
	}

	// 冪等：重跑不重複入帳
	created, err = invoices.GenerateForMonth("2026-08", 300000)
	if err != nil {
		t.Fatalf("重跑失敗：%v", err)
	}
	if created != 0 {
		t.Fatalf("重跑應新建 0 張，實得 %d", created)
	}

	// 補一個八月新活躍司機 → 重跑只補這一張，且不動既有帳單金額（快照）
	dC := newDriver(t, db, "D_C", "司機C")
	seedCompletedRideAt(t, db, dC, cust.ID, aug)
	created, err = invoices.GenerateForMonth("2026-08", 999) // 用不同金額重跑
	if err != nil {
		t.Fatalf("補帳單失敗：%v", err)
	}
	if created != 1 {
		t.Fatalf("補新司機應新建 1 張，實得 %d", created)
	}
	// A 的金額仍是 300000（ON CONFLICT DO NOTHING，不被 999 覆寫）
	all, _ := invoices.List("2026-08", "")
	for _, r := range all {
		if r.DriverID == dA && r.AmountCents != 300000 {
			t.Fatalf("A 的帳單金額被覆寫成 %d，快照失效", r.AmountCents)
		}
		if r.DriverID == dC && r.AmountCents != 999 {
			t.Fatalf("C 的帳單金額應為產生時的 999，實得 %d", r.AmountCents)
		}
	}

	// 標記 A 已繳
	var invA int64
	for _, r := range all {
		if r.DriverID == dA {
			invA = r.ID
		}
	}
	ok, err := invoices.SetPaid(invA, true)
	if err != nil || !ok {
		t.Fatalf("標記已繳失敗：ok=%v err=%v", ok, err)
	}

	paid, _ := invoices.List("2026-08", "paid")
	if len(paid) != 1 || paid[0].DriverID != dA || paid[0].Status != "paid" || paid[0].PaidAt == nil {
		t.Fatalf("已繳篩選不符：%+v", paid)
	}
	unpaid, _ := invoices.List("2026-08", "unpaid")
	if len(unpaid) != 2 { // B、C 仍未繳
		t.Fatalf("未繳預期 2 筆，實得 %d", len(unpaid))
	}

	// 取消已繳（回未繳，清 paid_at）
	if ok, _ := invoices.SetPaid(invA, false); !ok {
		t.Fatalf("取消已繳應成功")
	}
	back, _ := invoices.List("2026-08", "paid")
	if len(back) != 0 {
		t.Fatalf("取消後八月應無已繳帳單，實得 %d", len(back))
	}

	// 找不到的 id → false
	if ok, _ := invoices.SetPaid(999999, true); ok {
		t.Fatalf("不存在的帳單 SetPaid 應回 false")
	}
}
