package constants

import "testing"

func TestNormalizePhone(t *testing.T) {
	cases := map[string]string{
		" (02) 2345-6789 ": "0223456789",
		"0912-345-678":     "0912345678",
		"+886 912 345 678": "+886912345678",
		"":                 "",
	}
	for in, want := range cases {
		if got := NormalizePhone(in); got != want {
			t.Errorf("NormalizePhone(%q) = %q，預期 %q", in, got, want)
		}
	}
}

func TestIsValidPhone(t *testing.T) {
	valid := []string{
		"0223456789",    // 市話含區碼
		"0912345678",    // 手機
		"+886912345678", // 國際格式
		"23456789",      // 8 碼市話（下界）
	}
	for _, s := range valid {
		if !IsValidPhone(s) {
			t.Errorf("IsValidPhone(%q) 應為 true", s)
		}
	}

	invalid := []string{
		"",                   // 空字串由呼叫端決定是否放行，這裡一律 false
		"1234567",            // 7 碼，短於下界
		"1234567890123456",   // 16 碼，超過 E.164 上界
		"０９１２３４５６７８", // 全形數字（NormalizePhone 不轉半形）
		"0912345678a",        // 夾雜英文字母
		"09123+45678",        // `+` 只允許出現在開頭
	}
	for _, s := range invalid {
		if IsValidPhone(s) {
			t.Errorf("IsValidPhone(%q) 應為 false", s)
		}
	}
}
