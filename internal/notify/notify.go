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
type AppPusher interface {
	SendRideOffer(ctx context.Context, devices []Device, rideID int64, title, body string, data map[string]string) error
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

func tokenPrefix(token string) string {
	if len(token) <= 8 {
		return token
	}
	return token[:8] + "…"
}
