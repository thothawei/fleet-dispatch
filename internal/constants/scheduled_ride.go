package constants

// 預約行程狀態（scheduled_rides.status）
const (
	// ScheduledRideStatusPending 等待到點；唯一可被排程器認領、也是唯一可取消的狀態。
	ScheduledRideStatusPending = "pending"
	// ScheduledRideStatusDispatched 已轉成真訂單（ride_id 指向它），後續走既有派單鏈路。
	ScheduledRideStatusDispatched = "dispatched"
	// ScheduledRideStatusCancelled 乘客取消。
	ScheduledRideStatusCancelled = "cancelled"
	// ScheduledRideStatusFailed 到點後重試上限內仍建不出訂單，last_error 記原因。
	ScheduledRideStatusFailed = "failed"
)

const (
	// ScheduledRideLeadMinutes 提前發動派單的分鐘數。
	//
	// 預約 08:00 的車，08:00 才開始找司機就注定遲到——派單本身要時間（找車、司機接受、
	// 開過來）。提前這麼多分鐘把它丟進派單池，讓司機有機會在約定時間前抵達。
	ScheduledRideLeadMinutes = 15

	// ScheduledRideMaxAttempts 轉單重試上限。
	//
	// 到點時乘客可能還在另一趟行程上（後端擋「同時只能有一張進行中訂單」），
	// 那不是永久失敗，等下一輪再試即可。試滿這個次數仍不成，才標 failed 讓乘客看到原因。
	ScheduledRideMaxAttempts = 10

	// ScheduledRideMinLeadMinutes 建立預約時，距現在至少要有的分鐘數。
	// 比 lead time 更短的預約一建立就該立刻派單，那使用者要的其實是「現在叫車」。
	ScheduledRideMinLeadMinutes = 20

	// ScheduledRideMaxDaysAhead 最遠可預約的天數。
	ScheduledRideMaxDaysAhead = 30

	// ScheduledRideExpiryGraceMinutes 過了約定時間多久之後，這筆預約就不再轉單。
	//
	// **正常運作下永遠碰不到這個界線**——提前量是 15 分鐘，早在約定時間前就轉完了。
	// 它防的是排程器停過一段時間（部署、當機、DB 不可用）：重啟時如果照樣把積壓的
	// 過期預約全部轉成真訂單，乘客昨天早上預約的車今天下午會突然開到他家樓下。
	// 等了半小時還沒車的乘客，早就自己叫車或改搭別的了。
	ScheduledRideExpiryGraceMinutes = 30
)

// IsScheduledRideCancellable 只有還沒轉單的預約可以取消。
// 已轉單的要取消的是**那張真訂單**（走 cancel-by-customer），不是這筆預約紀錄。
func IsScheduledRideCancellable(status string) bool {
	return status == ScheduledRideStatusPending
}
