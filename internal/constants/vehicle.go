package constants

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
