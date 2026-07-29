package notify

import (
	"context"

	"github.com/rs/zerolog/log"
)

const (
	RoleDriver   = "driver"
	RoleCustomer = "customer"
	PlatformFCM  = "fcm"
	PlatformAPNs = "apns"
)

// Device 對應一台已註冊的推播裝置。
type Device struct {
	Platform string
	Token    string
}

// AppPusher 依註冊的 FCM/APNs token 送 App 推播（與 LINE Messaging API 分開）。
// 無 Firebase／APNs 憑證時可用 LogPusher 當 stub，讓 D1 契約與派單路徑先就緒。
//
// data 是給 App「被殺後點推播喚醒」用的鍵值：type／ride_id／上車點與座標等，
// **值一律字串**（FCM data payload 限制）。App 端 fleetEventFromPushData 據此
// 直接開接單卡。stops 這種結構化陣列**不放進 data**——App 接單後重讀 rides/active
// 以後端補齊全程（見 fleet-app：acceptOffer 重讀 active），推播保持精簡。
//
// SendRideUpdate 是**乘客端**那條：行程狀態變了（司機已接單／已抵達／完成／取消／重新派車）。
// 與派單邀請分成兩個方法，是因為兩邊的語意與失敗處理不同——派單失敗要記在司機那筆，
// 行程更新失敗要記在乘客那筆；共用一個方法會讓 log 分不出是哪一條路徑。
type AppPusher interface {
	SendRideOffer(ctx context.Context, devices []Device, rideID int64, title, body string, data map[string]string) error
	SendRideUpdate(ctx context.Context, devices []Device, rideID int64, title, body string, data map[string]string) error
}

// DeviceLookup 查某角色主體的裝置 token。
type DeviceLookup interface {
	ListBySubject(role string, subjectID int64) ([]Device, error)
}

// Dispatcher 查裝置後走 AppPusher 送出；失敗只打 log，不讓派單中斷。
type Dispatcher struct {
	tokens DeviceLookup
	push   AppPusher
}

func NewDispatcher(tokens DeviceLookup, push AppPusher) *Dispatcher {
	if push == nil {
		push = LogPusher{}
	}
	return &Dispatcher{tokens: tokens, push: push}
}

func (d *Dispatcher) NotifyDriverRideOffer(ctx context.Context, driverID, rideID int64, title, body string, data map[string]string) {
	if d == nil || d.tokens == nil {
		return
	}
	devices, err := d.tokens.ListBySubject(RoleDriver, driverID)
	if err != nil {
		log.Error().Err(err).Int64("driver_id", driverID).Msg("查詢裝置 token 失敗")
		return
	}
	if len(devices) == 0 {
		return
	}
	if err := d.push.SendRideOffer(ctx, devices, rideID, title, body, data); err != nil {
		log.Error().Err(err).Int64("driver_id", driverID).Int64("ride_id", rideID).Msg("App 推播派單失敗")
	}
}

// NotifyCustomerRideUpdate 推播「這趟車的狀態變了」給乘客的所有裝置。
//
// App 端把這則推播**只當對帳訊號**：醒來後重讀 rides/active，不直接信 data 裡的內容
// （見 fleet-app：isCustomerRidePush → refreshActive）。所以 data 只需要 type 與 ride_id，
// 車資、司機姓名這類會變的欄位一律不放——推播可能延遲數十秒才到，內容早就過期了。
//
// 失敗只打 log：乘客收不到推播的後果是「App 在背景時晚一點才知道」，
// 不該讓接單／完成這些主鏈路動作跟著失敗。
func (d *Dispatcher) NotifyCustomerRideUpdate(ctx context.Context, customerID, rideID int64, title, body string, data map[string]string) {
	if d == nil || d.tokens == nil {
		return
	}
	devices, err := d.tokens.ListBySubject(RoleCustomer, customerID)
	if err != nil {
		log.Error().Err(err).Int64("customer_id", customerID).Msg("查詢乘客裝置 token 失敗")
		return
	}
	if len(devices) == 0 {
		return
	}
	if err := d.push.SendRideUpdate(ctx, devices, rideID, title, body, data); err != nil {
		log.Error().Err(err).Int64("customer_id", customerID).Int64("ride_id", rideID).
			Msg("App 推播行程狀態失敗")
	}
}

// LogPusher 開發用 stub：只記錄將送出的 token，不打外部服務。
type LogPusher struct{}

func (LogPusher) SendRideOffer(ctx context.Context, devices []Device, rideID int64, title, body string, data map[string]string) error {
	_ = ctx
	_ = body
	for _, d := range devices {
		log.Info().
			Str("platform", d.Platform).
			Str("token_prefix", tokenPrefix(d.Token)).
			Int64("ride_id", rideID).
			Str("title", title).
			Interface("data", data).
			Msg("App 推播（stub）：派單邀請")
	}
	return nil
}

func (LogPusher) SendRideUpdate(ctx context.Context, devices []Device, rideID int64, title, body string, data map[string]string) error {
	_ = ctx
	_ = body
	for _, d := range devices {
		log.Info().
			Str("platform", d.Platform).
			Str("token_prefix", tokenPrefix(d.Token)).
			Int64("ride_id", rideID).
			Str("title", title).
			Interface("data", data).
			Msg("App 推播（stub）：行程狀態更新")
	}
	return nil
}

func tokenPrefix(token string) string {
	if len(token) <= 8 {
		return token
	}
	return token[:8] + "…"
}
