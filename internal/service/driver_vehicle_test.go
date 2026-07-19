package service

import (
	"errors"
	"testing"

	"line-fleet-dispatch/internal/repository"
)

// TestDriverSetVehicle 驗證 O2 的車輛資訊設定：驗證、正規化與唯一車牌。
func TestDriverSetVehicle(t *testing.T) {
	db := newServiceTestDB(t)
	driversRepo := repository.NewDriverRepository(db)
	reg := NewDriverRegistry(driversRepo)

	newDriver := func(t *testing.T, lineID string) int64 {
		t.Helper()
		d, err := driversRepo.FindOrCreate(lineID, "車輛測試司機")
		if err != nil {
			t.Fatalf("建立司機失敗：%v", err)
		}
		return d.ID
	}

	t.Run("車牌寫入前先正規化", func(t *testing.T) {
		// 車牌非空時有唯一索引；不正規化就存，同一台車會以「abc-1234」「ABC-1234」
		// 各佔一列，唯一性形同虛設。
		id := newDriver(t, "U_veh_norm")
		got, err := reg.SetVehicle(id, "sedan", " abc-1234 ")
		if err != nil {
			t.Fatalf("設定車輛資訊失敗：%v", err)
		}
		if got.PlateNumber != "ABC-1234" {
			t.Fatalf("回傳車牌應為正規化後的 ABC-1234，得到 %q", got.PlateNumber)
		}
		reread, err := driversRepo.FindByID(id)
		if err != nil {
			t.Fatalf("重讀司機失敗：%v", err)
		}
		if reread.PlateNumber != "ABC-1234" {
			t.Fatalf("DB 內車牌應為正規化後的 ABC-1234，得到 %q", reread.PlateNumber)
		}
		if !reread.HasVehicle() {
			t.Fatal("填妥車輛資訊後 HasVehicle() 應為 true（O2 的條件）")
		}
		// O5：填/改車輛後進 pending，還不能接單（要 admin 核准）。
		if got.VehicleReviewStatus != "pending" {
			t.Fatalf("設定車輛後應為 pending 待審核，得到 %q", got.VehicleReviewStatus)
		}
		if reread.VehicleReviewStatus != "pending" {
			t.Fatalf("DB 內審核狀態應為 pending，得到 %q", reread.VehicleReviewStatus)
		}
		if reread.VehicleApproved() {
			t.Fatal("待審核的司機 VehicleApproved() 應為 false（O5 gate）")
		}
	})

	t.Run("車種與車牌驗證失敗時不寫入", func(t *testing.T) {
		id := newDriver(t, "U_veh_invalid")
		if _, err := reg.SetVehicle(id, "spaceship", "ABC-9999"); !errors.Is(err, ErrInvalidVehicleType) {
			t.Fatalf("非白名單車種預期 ErrInvalidVehicleType，得到 %v", err)
		}
		if _, err := reg.SetVehicle(id, "", "ABC-9999"); !errors.Is(err, ErrInvalidVehicleType) {
			t.Fatalf("空車種預期 ErrInvalidVehicleType，得到 %v", err)
		}
		if _, err := reg.SetVehicle(id, "sedan", "ABC_9999"); !errors.Is(err, ErrInvalidPlateNumber) {
			t.Fatalf("非法字元車牌預期 ErrInvalidPlateNumber，得到 %v", err)
		}
		if _, err := reg.SetVehicle(id, "sedan", "  "); !errors.Is(err, ErrInvalidPlateNumber) {
			t.Fatalf("空白車牌預期 ErrInvalidPlateNumber，得到 %v", err)
		}
		// 驗證失敗不得留下半套資料——否則司機會通過 O3 gate 卻沒有有效車輛資訊。
		reread, err := driversRepo.FindByID(id)
		if err != nil {
			t.Fatalf("重讀司機失敗：%v", err)
		}
		if reread.HasVehicle() {
			t.Fatalf("驗證失敗不該寫入任何車輛資訊，得到 %q / %q", reread.VehicleType, reread.PlateNumber)
		}
	})

	t.Run("車牌被別的司機用走", func(t *testing.T) {
		ownerID := newDriver(t, "U_veh_owner")
		if _, err := reg.SetVehicle(ownerID, "pet", "PET-0001"); err != nil {
			t.Fatalf("第一位司機設定車牌應成功：%v", err)
		}
		otherID := newDriver(t, "U_veh_other")
		// 大小寫不同但正規化後同一張車牌，仍應撞唯一索引。
		_, err := reg.SetVehicle(otherID, "suv", "pet-0001")
		if !errors.Is(err, repository.ErrPlateTaken) {
			t.Fatalf("預期 repository.ErrPlateTaken，得到 %v", err)
		}
	})

	t.Run("司機不存在", func(t *testing.T) {
		if _, err := reg.SetVehicle(999999, "sedan", "NON-0001"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("預期 ErrNotFound，得到 %v", err)
		}
	})
}
