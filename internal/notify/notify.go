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
type AppPusher interface {
	SendRideOffer(ctx context.Context, devices []Device, rideID int64, title, body string) error
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

func (d *Dispatcher) NotifyDriverRideOffer(ctx context.Context, driverID, rideID int64, title, body string) {
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
	if err := d.push.SendRideOffer(ctx, devices, rideID, title, body); err != nil {
		log.Error().Err(err).Int64("driver_id", driverID).Int64("ride_id", rideID).Msg("App 推播派單失敗")
	}
}

// LogPusher 開發用 stub：只記錄將送出的 token，不打外部服務。
type LogPusher struct{}

func (LogPusher) SendRideOffer(ctx context.Context, devices []Device, rideID int64, title, body string) error {
	_ = ctx
	for _, d := range devices {
		log.Info().
			Str("platform", d.Platform).
			Str("token_prefix", tokenPrefix(d.Token)).
			Int64("ride_id", rideID).
			Str("title", title).
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
