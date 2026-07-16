package constants

import "testing"

func TestIsValidVehicleType(t *testing.T) {
	for _, v := range []string{"sedan", "suv", "van7", "accessible", "pet"} {
		if !IsValidVehicleType(v) {
			t.Fatalf("%q 應為合法車種", v)
		}
	}
	// 空字串＝未設定，**不算合法車種**：避免 API 收到空值就當合法而繞過 O3 的 gate。
	for _, v := range []string{"", "SEDAN", "car", "寵物", "pet "} {
		if IsValidVehicleType(v) {
			t.Fatalf("%q 不該被當成合法車種", v)
		}
	}
}

func TestVehicleTypesIsDefensiveCopy(t *testing.T) {
	got := VehicleTypes()
	if len(got) != 5 {
		t.Fatalf("預期 5 種車種，得到 %d", len(got))
	}
	got[0] = "tampered"
	if VehicleTypes()[0] != VehicleTypeSedan {
		t.Fatal("VehicleTypes() 必須回副本，呼叫端不得改動白名單")
	}
}
