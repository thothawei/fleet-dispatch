package constants

import (
	"strings"
	"unicode"
)

// 車種 code（O1 定案 2026-07-16）。後端只認 code，顯示名稱由前端對應
// （轎車／休旅／七人座／無障礙／寵物用車）。
// 值域同時由 DB CHECK `chk_drivers_vehicle_type` 把關（migration 000017）——
// 這是清潔費（O6）與派單車種過濾（P3）的判斷依據，值髒掉會直接影響計費。
const (
	VehicleTypeSedan      = "sedan"      // 轎車
	VehicleTypeSUV        = "suv"        // 休旅
	VehicleTypeVan7       = "van7"       // 七人座
	VehicleTypeAccessible = "accessible" // 無障礙
	VehicleTypePet        = "pet"        // 寵物用車（乘客指定此車種時加收清潔費，見 O6／P）
)

// 車輛審核狀態（O5 定案 2026-07-19）。司機填/改車輛後進 Pending，admin 核准才能接單。
// 值域由 DB CHECK `chk_drivers_vehicle_review_status` 把關（migration 000022）。
// 導入時既有已填車輛的司機祖父化為 Approved（migration 一次性 UPDATE），不因導入被鎖出。
const (
	VehicleReviewNone     = ""         // 未提交（沒填車輛）
	VehicleReviewPending  = "pending"  // 待審核（填了/改了，等 admin）
	VehicleReviewApproved = "approved" // 已核准（可接單）
	VehicleReviewRejected = "rejected" // 已退回（附原因，可重填）
)

// vehicleTypes 白名單；順序即前端建議的顯示順序。
var vehicleTypes = []string{
	VehicleTypeSedan,
	VehicleTypeSUV,
	VehicleTypeVan7,
	VehicleTypeAccessible,
	VehicleTypePet,
}

// VehicleTypes 回傳白名單副本（呼叫端不得改動內部切片）。
func VehicleTypes() []string {
	out := make([]string, len(vehicleTypes))
	copy(out, vehicleTypes)
	return out
}

// IsValidVehicleType 是否為合法車種 code。空字串（未設定）**不算合法**——
// 呼叫端若要允許「未設定」需自行另判，避免 API 不小心接受空車種而繞過 O3 gate。
func IsValidVehicleType(s string) bool {
	for _, v := range vehicleTypes {
		if v == s {
			return true
		}
	}
	return false
}

// vehicleTypeNames 車種 code 的中文顯示名。
//
// 一般原則仍是「後端只認 code，顯示名由前端對應」（O1）——這份對照表**只給後端自產的文案用**，
// 也就是 LINE 推播（例如「附近暫無寵物用車」，P4）：那些字串是後端組的，沒有前端可以對應。
// API／WS payload 一律只送 code，不要送顯示名。
var vehicleTypeNames = map[string]string{
	VehicleTypeSedan:      "轎車",
	VehicleTypeSUV:        "休旅車",
	VehicleTypeVan7:       "七人座",
	VehicleTypeAccessible: "無障礙車",
	VehicleTypePet:        "寵物用車",
}

// VehicleTypeDisplayName 回傳車種的中文顯示名；未知 code（含空字串）回「可用車輛」，
// 讓文案在資料異常時仍讀得通，而不是出現空白或原始 code。
func VehicleTypeDisplayName(code string) string {
	if name, ok := vehicleTypeNames[code]; ok {
		return name
	}
	return "可用車輛"
}

// 車牌長度界線（正規化後）。台灣現行最長為 `ABC-1234`（8 碼），
// 放寬到 10 是為了容納舊式與特殊車牌。
const (
	MinPlateLen = 2
	MaxPlateLen = 10
)

// NormalizePlateNumber 去掉所有空白並轉大寫，作為寫入 DB 的正規形式。
// 車牌非空時有唯一索引（O1），不正規化會讓「abc-1234」與「ABC-1234」被當成兩台車。
func NormalizePlateNumber(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsSpace(r) {
			continue
		}
		b.WriteRune(unicode.ToUpper(r))
	}
	return b.String()
}

// IsValidPlateNumber 寬鬆驗證（O2 拍板 2026-07-17）：只檢查長度與字元集
// （半形 A-Z、0-9、`-`，且至少有一個英數字），不綁特定樣式。
// 台灣車牌樣式多代並存（ABC-1234／1234-AB／AB-1234／電動車／機車格式），
// 硬綁正則會誤擋真車牌，而擋不住的髒值本來就由 O3 gate 與人工審核（O5）兜底。
// 傳入值須為 NormalizePlateNumber 的輸出（全形字元不會被轉半形，會在此被判為非法）。
func IsValidPlateNumber(s string) bool {
	if len(s) < MinPlateLen || len(s) > MaxPlateLen {
		return false
	}
	hasAlnum := false
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			hasAlnum = true
		case r == '-':
		default:
			return false
		}
	}
	return hasAlnum
}
