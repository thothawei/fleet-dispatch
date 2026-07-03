package constants

// 訂單狀態
const (
	RideStatusRequested int16 = 0 // 已叫車、待派單
	RideStatusAssigned  int16 = 1 // 已派給司機、待司機接受
	RideStatusAccepted  int16 = 2 // 司機已接單、前往接客中
	RideStatusPickedUp  int16 = 3 // 客戶已上車、行程中
	RideStatusCompleted int16 = 4 // 已完成
	RideStatusCancelled int16 = 9 // 取消
)
