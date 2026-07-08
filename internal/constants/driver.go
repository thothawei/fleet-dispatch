package constants

// 司機狀態
const (
	DriverStatusOffline  int16 = 0 // 離線
	DriverStatusIdle     int16 = 1 // 待命（在派單池）
	DriverStatusOnTrip   int16 = 2 // 載客中
	DriverStatusDisabled int16 = 3 // 後台停用（不可上線、不被派單）
)
