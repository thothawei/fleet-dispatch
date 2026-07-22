package constants

import "strings"

// 電話長度界線（正規化後的數字位數）。台灣手機 10 碼、市話含區碼 9–10 碼，
// 放寬到 8–15 是為了容納市話短碼與 E.164 國際碼（最長 15 位）。
const (
	MinPhoneDigits = 8
	MaxPhoneDigits = 15
)

// NormalizePhone 去掉人類書寫用的分隔符（空白、`-`、`(`、`)`），保留開頭的 `+`。
// 這個值會被乘客端直接組成 `tel:` 連結撥出（O7 電話明碼），
// 留著分隔符在部分機型會撥不出去。
func NormalizePhone(s string) string {
	s = strings.TrimSpace(s)
	plus := strings.HasPrefix(s, "+")
	var b strings.Builder
	if plus {
		b.WriteByte('+')
	}
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// IsValidPhone 寬鬆驗證：正規化後只允許「可選的 `+` ＋ 8–15 位數字」。
// 刻意不綁「09 開頭」之類的台灣樣式——車隊可能有市話或境外號碼，
// 硬綁會誤擋真號碼，而打不通的號碼本來就只能靠乘客回報，不是後端擋得住的。
// 傳入值須為 NormalizePhone 的輸出。
func IsValidPhone(s string) bool {
	digits := strings.TrimPrefix(s, "+")
	if len(digits) < MinPhoneDigits || len(digits) > MaxPhoneDigits {
		return false
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
