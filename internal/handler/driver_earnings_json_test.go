package handler

import (
	"testing"

	"line-fleet-dispatch/internal/repository"
)

// TestDriverEarningsJSON 釘住司機收入回應的欄位（F7／O6）。
//
// 這支 handler 手動逐欄組 map——repo 的 struct 新增欄位時**不會**自動出現在回應裡。
// O6 的清潔費分項就這樣漏過一次：repository 層的 SQL 與 struct 都加了、整合測試也綠，
// 但 API 回應少了 total_cleaning_fee_cents，直到 live E2E 才抓到。
func TestDriverEarningsJSON(t *testing.T) {
	// 一趟指定寵物車的行程：車資 215、手續費 32（基準只有車資）、清潔費 43、實得 226。
	e := repository.DriverEarnings{
		TripCount:             1,
		TotalRevenueCents:     21500,
		TotalCommissionCents:  3200,
		TotalCleaningFeeCents: 4300,
		DriverNetCents:        22600,
		MembershipFeeCents:    300000,
	}
	got := driverEarningsJSON("2026-07", e)

	t.Run("清潔費分項不可缺", func(t *testing.T) {
		v, ok := got["total_cleaning_fee_cents"]
		if !ok {
			t.Fatal("回應必須含 total_cleaning_fee_cents，否則司機收入頁的等式對不上")
		}
		if v != int64(4300) {
			t.Fatalf("清潔費分項錯誤：%v", v)
		}
	})

	t.Run("等式成立：營業額 − 手續費 + 清潔費 = 實得", func(t *testing.T) {
		rev := got["total_revenue_cents"].(int64)
		comm := got["total_commission_cents"].(int64)
		clean := got["total_cleaning_fee_cents"].(int64)
		net := got["driver_net_cents"].(int64)
		if rev-comm+clean != net {
			t.Fatalf("等式失敗：%d − %d + %d = %d，但實得為 %d", rev, comm, clean, rev-comm+clean, net)
		}
	})

	t.Run("應付總公司不受清潔費影響", func(t *testing.T) {
		// ＝手續費 + 月會費；清潔費全額歸司機，不進這條。
		if got["owed_to_hq_cents"] != int64(3200+300000) {
			t.Fatalf("應付總公司錯誤：%v", got["owed_to_hq_cents"])
		}
	})

	t.Run("營業額不含清潔費", func(t *testing.T) {
		if got["total_revenue_cents"] != int64(21500) {
			t.Fatalf("營業額不該含清潔費：%v", got["total_revenue_cents"])
		}
	})
}
