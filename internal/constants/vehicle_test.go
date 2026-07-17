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

func TestNormalizePlateNumber(t *testing.T) {
	cases := map[string]string{
		"abc-1234":   "ABC-1234",
		" ABC-1234 ": "ABC-1234",
		"abc 1234":   "ABC1234",
		"":           "",
	}
	for in, want := range cases {
		// 正規化是車牌唯一索引（uq_drivers_plate_number）的前提：
		// 不轉大寫去空白，同一台車會以多種寫法各佔一列。
		if got := NormalizePlateNumber(in); got != want {
			t.Fatalf("NormalizePlateNumber(%q) = %q，預期 %q", in, got, want)
		}
	}
}

func TestIsValidPlateNumber(t *testing.T) {
	// 寬鬆驗證（O2 拍板）：台灣車牌多代並存，只檢查長度與字元集，不綁樣式。
	for _, v := range []string{"ABC-1234", "1234-AB", "AB-1234", "EAA-0001", "123-AB", "AB", "0000000000"} {
		if !IsValidPlateNumber(v) {
			t.Fatalf("%q 應為合法車牌", v)
		}
	}
	for _, v := range []string{
		"",              // 空＝未設定，不得當合法值（會繞過 O3 gate）
		"A",             // 短於 MinPlateLen
		"ABCD-123456",   // 長於 MaxPlateLen
		"---",           // 全連字號、無英數字
		"ABC 1234",      // 未正規化（含空白）
		"ＡＢＣ-1234",      // 全形字元不視為合法，需以半形輸入
		"ABC_1234",      // 非允許字元
		"ABC-1234;DROP", // 過長且含非法字元
	} {
		if IsValidPlateNumber(v) {
			t.Fatalf("%q 不該被當成合法車牌", v)
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
