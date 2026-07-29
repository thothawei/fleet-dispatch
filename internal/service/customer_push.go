package service

import (
	"context"
	"strconv"

	"line-fleet-dispatch/internal/events"
	"line-fleet-dispatch/internal/notify"
)

// 乘客端 App 推播（第九輪候選 1 的後半）。
//
// 先前後端只有 `NotifyDriverRideOffer` 一條送出路徑：乘客即使註冊了 device token
// （`POST /api/customer/device-token` 早就存在），也**永遠不會收到任何推播**——
// App 一離開前景，WS 就斷了，行程狀態要等下次回前景輪詢才知道。
//
// 這裡補的是「送出」那一半。憑證那一半（Firebase 專案）仍待外部資源；
// 沒有憑證時走 LogPusher stub，這條路徑照樣可驗（log 看得到要送給誰）。

// customerPushCopy 依事件型別給推播的標題與內文。
//
// 白名單刻意與 App 端 `_customerPushTypes` 對齊（fleet-app: lib/core/push/push_payload.dart）——
// App 收到不在白名單裡的 type 會直接丟掉，後端多送只是白白燒推播額度。
// 特別**不送 driver.location**：司機每 8 秒回報一次，拿它推播會把電池與額度燒光。
func customerPushCopy(eventType string) (title, body string, ok bool) {
	switch eventType {
	case events.TypeRideAccepted:
		return "司機已接單", "司機正在前往上車點", true
	case events.TypeDriverArrived:
		return "司機已抵達", "司機已在上車點等候，請準備上車", true
	case events.TypeRideCompleted:
		return "行程已完成", "感謝搭乘，可在 App 查看車資明細", true
	case events.TypeRideCancelled:
		return "行程已取消", "您的行程已取消，請重新叫車", true
	case events.TypeRideRedispatched:
		return "正在重新為您派車", "原司機取消了行程，系統正在尋找新的司機", true
	default:
		return "", "", false
	}
}

// customerRidePushData 組裝乘客端推播的 data。
//
// **只帶 type 與 ride_id**：App 端把這則推播只當對帳訊號，醒來後重讀 rides/active
// 取當下狀態（fleet-app: isCustomerRidePush → refreshActive）。
// 車資、司機姓名這類會變的欄位一律不放——推播可能延遲數十秒才到，內容早就過期，
// 直接顯示反而會讓 App 說謊。值一律字串（FCM data payload 限制）。
func customerRidePushData(eventType string, rideID int64) map[string]string {
	return map[string]string{
		"type":    eventType,
		"ride_id": strconv.FormatInt(rideID, 10),
	}
}

// notifyCustomerRide 送一則乘客端行程狀態推播；未注入 notifier 或型別不在白名單時靜默略過。
//
// 兩個 service（DispatchService／TrackingService）共用，所以寫成自由函式而非方法。
func notifyCustomerRide(ctx context.Context, n *notify.Dispatcher, customerID, rideID int64, eventType string) {
	if n == nil {
		return
	}
	title, body, ok := customerPushCopy(eventType)
	if !ok {
		return
	}
	n.NotifyCustomerRideUpdate(ctx, customerID, rideID, title, body,
		customerRidePushData(eventType, rideID))
}
