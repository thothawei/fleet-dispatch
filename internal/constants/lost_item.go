package constants

// 遺失物協尋單狀態（lost_item_requests.status）
const (
	LostItemStatusOpen     = "open"     // 乘客已建立，待司機確認尋獲
	LostItemStatusFound    = "found"    // 司機已尋獲，待乘客支付處理費
	LostItemStatusPaid     = "paid"     // 乘客已支付處理費，待歸還
	LostItemStatusReturned = "returned" // 已歸還，結案
	LostItemStatusClosed   = "closed"   // 未尋獲／取消，結案
)
