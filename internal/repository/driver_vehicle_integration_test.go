package repository

import (
	"strings"
	"testing"

	"line-fleet-dispatch/internal/model"
)

// O1 的 schema 保證：跑真 migration 後驗車種 CHECK 與車牌 partial unique index。
// 這兩條是清潔費（O6）與派單過濾（P3）的地基，值髒掉會直接影響計費，故釘在資料層。
func TestDriverVehicleSchema(t *testing.T) {
	db := newMigratedTestDB(t)

	newDriver := func(lineID, vType, plate string) *model.Driver {
		return &model.Driver{
			LineUserID:  lineID,
			Name:        "測試司機",
			VehicleType: vType,
			PlateNumber: plate,
		}
	}

	t.Run("既有司機（未填車輛）可正常建立，且多筆空車牌不互相衝突", func(t *testing.T) {
		// 這是 partial unique index 的存在理由：一般 UNIQUE 會讓第二筆空車牌撞唯一鍵，
		// 既有司機（plate_number='') 全部插不進去。
		if err := db.Create(newDriver("u_empty_1", "", "")).Error; err != nil {
			t.Fatalf("第一位未填車輛的司機應可建立: %v", err)
		}
		if err := db.Create(newDriver("u_empty_2", "", "")).Error; err != nil {
			t.Fatalf("第二位未填車輛的司機也應可建立（空車牌不受唯一約束）: %v", err)
		}
	})

	t.Run("車牌非空時唯一", func(t *testing.T) {
		if err := db.Create(newDriver("u_plate_1", "sedan", "ABC-1234")).Error; err != nil {
			t.Fatalf("建立第一台車應成功: %v", err)
		}
		err := db.Create(newDriver("u_plate_2", "suv", "ABC-1234")).Error
		if err == nil {
			t.Fatal("同一車牌不該能掛在兩個司機帳號上")
		}
		if !strings.Contains(strings.ToLower(err.Error()), "uq_drivers_plate_number") {
			t.Fatalf("應由 uq_drivers_plate_number 擋下，實際錯誤: %v", err)
		}
	})

	t.Run("車種白名單由 DB CHECK 把關", func(t *testing.T) {
		for _, ok := range []string{"", "sedan", "suv", "van7", "accessible", "pet"} {
			d := newDriver("u_ok_"+ok, ok, "PLATE-"+ok)
			if err := db.Create(d).Error; err != nil {
				t.Fatalf("車種 %q 應為合法值: %v", ok, err)
			}
		}
		err := db.Create(newDriver("u_bad", "spaceship", "ZZZ-9999")).Error
		if err == nil {
			t.Fatal("非白名單車種不該寫得進去")
		}
		if !strings.Contains(strings.ToLower(err.Error()), "chk_drivers_vehicle_type") {
			t.Fatalf("應由 chk_drivers_vehicle_type 擋下，實際錯誤: %v", err)
		}
	})
}
