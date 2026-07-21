package constants

import (
	"strings"
	"unicode"
)

const (
	// MinPhoneLen / MaxPhoneLen 為正規化後的長度界線（不含 `+`）。
	// 下界取 8：台灣市話最短為 8 碼（不含區碼的台北號碼）。
	// 上界取 15：E.164 規定的國際號碼最大長度。
	MinPhoneLen = 8
	MaxPhoneLen = 15
)

// NormalizePhone 去掉空白與人類可讀的分隔符號（`-`、`(`、`)`），保留開頭的 `+`。
// 乘客端是拿這個值直接組 `tel:` 撥號用的，留著分隔符號不影響撥號，
// 但正規化後才好做長度與字元驗證，也避免同一支號碼存成多種寫法。
func NormalizePhone(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsSpace(r) || r == '-' || r == '(' || r == ')' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// IsValidPhone 寬鬆驗證（比照 IsValidPlateNumber 的取捨）：只檢查長度與字元集，
// 不綁「09 開頭」這類特定樣式——司機可能留市話或國際號碼，硬綁會誤擋真號碼。
// 傳入值須為 NormalizePhone 的輸出（全形數字不會被轉半形，會在此被判為非法）。
// 空字串代表「清除電話」，由呼叫端自行決定是否允許，不在這裡放行。
func IsValidPhone(s string) bool {
	digits := s
	if strings.HasPrefix(digits, "+") {
		digits = digits[1:]
	}
	if len(digits) < MinPhoneLen || len(digits) > MaxPhoneLen {
		return false
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
