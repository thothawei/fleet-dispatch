package constants

// 常用地點種類（customer_saved_places.kind）。
//
// home／work 是「語意化插槽」：每位乘客各只能有一筆（DB 部分唯一索引把關），
// App 端據此給固定圖示與置頂排序。custom 則是其他自訂地點，數量不限。
// **App 不該去比對 label 文字**來判斷是不是住家——label 是使用者可改的顯示名稱。
const (
	SavedPlaceKindHome   = "home"
	SavedPlaceKindWork   = "work"
	SavedPlaceKindCustom = "custom"
)

// IsValidSavedPlaceKind 白名單檢查；DB CHECK 是最後防線，但錯誤要在服務層變成 400。
func IsValidSavedPlaceKind(kind string) bool {
	switch kind {
	case SavedPlaceKindHome, SavedPlaceKindWork, SavedPlaceKindCustom:
		return true
	}
	return false
}

// IsSlotSavedPlaceKind 是否為「每人限一筆」的插槽種類。
func IsSlotSavedPlaceKind(kind string) bool {
	return kind == SavedPlaceKindHome || kind == SavedPlaceKindWork
}
