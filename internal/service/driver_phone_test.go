package service

import (
	"errors"
	"testing"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/repository"
)

// TestDriverSetPhone 驗證司機聯絡電話的寫入路徑。
// 這條路徑在此之前不存在——乘客端的撥號按鈕（O7）讀 drivers.phone，
// 但沒有任何 API 能寫它，所以按鈕永遠不顯示。
func TestDriverSetPhone(t *testing.T) {
	db := newServiceTestDB(t)
	driversRepo := repository.NewDriverRepository(db)
	reg := NewDriverRegistry(driversRepo)

	newDriver := func(t *testing.T, lineID string) int64 {
		t.Helper()
		d, err := driversRepo.FindOrCreate(lineID, "電話測試司機")
		if err != nil {
			t.Fatalf("建立司機失敗：%v", err)
		}
		return d.ID
	}

	t.Run("寫入前先正規化分隔符號", func(t *testing.T) {
		id := newDriver(t, "U_phone_norm")
		got, err := reg.SetPhone(id, " (02) 2345-6789 ")
		if err != nil {
			t.Fatalf("設定電話應成功：%v", err)
		}
		if got.Phone != "0223456789" {
			t.Fatalf("電話未正規化，得到 %q", got.Phone)
		}
		// 重讀確認真的落地，而不是只改了回傳的物件
		reread, err := driversRepo.FindByID(id)
		if err != nil {
			t.Fatalf("重讀司機失敗：%v", err)
		}
		if reread.Phone != "0223456789" {
			t.Fatalf("DB 裡的電話是 %q", reread.Phone)
		}
	})

	t.Run("空字串代表清除電話", func(t *testing.T) {
		id := newDriver(t, "U_phone_clear")
		if _, err := reg.SetPhone(id, "0912345678"); err != nil {
			t.Fatalf("先設定電話應成功：%v", err)
		}
		got, err := reg.SetPhone(id, "")
		if err != nil {
			t.Fatalf("清除電話應成功：%v", err)
		}
		if got.Phone != "" {
			t.Fatalf("電話應被清空，得到 %q", got.Phone)
		}
	})

	t.Run("格式錯誤要擋下", func(t *testing.T) {
		id := newDriver(t, "U_phone_bad")
		for _, bad := range []string{"12345", "09一二三四五六七八", "0912-345-678a"} {
			if _, err := reg.SetPhone(id, bad); !errors.Is(err, ErrInvalidPhone) {
				t.Fatalf("%q 應回 ErrInvalidPhone，得到 %v", bad, err)
			}
		}
	})

	// 這是本功能存在的理由之一：電話若併進 SetVehicle，
	// 司機改一次號碼就會被 O5 gate 踢回「審核中」而無法接單。
	t.Run("改電話不影響車輛審核狀態", func(t *testing.T) {
		id := newDriver(t, "U_phone_no_review_reset")
		if _, err := reg.SetVehicle(id, "sedan", "PHN-0001"); err != nil {
			t.Fatalf("設定車輛應成功：%v", err)
		}
		if err := driversRepo.UpdateVehicleReview(id, constants.VehicleReviewApproved, ""); err != nil {
			t.Fatalf("模擬審核通過失敗：%v", err)
		}

		if _, err := reg.SetPhone(id, "0912345678"); err != nil {
			t.Fatalf("設定電話應成功：%v", err)
		}

		after, err := driversRepo.FindByID(id)
		if err != nil {
			t.Fatalf("重讀司機失敗：%v", err)
		}
		if after.VehicleReviewStatus != constants.VehicleReviewApproved {
			t.Fatalf("改電話後審核狀態變成 %q，司機會被鎖出接單", after.VehicleReviewStatus)
		}
		if !after.VehicleApproved() {
			t.Fatal("改電話後 VehicleApproved() 應仍為 true")
		}
	})
}
