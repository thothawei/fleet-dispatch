package notify

import (
	"context"
	"fmt"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	"github.com/rs/zerolog/log"
	"google.golang.org/api/option"
)

// FCMPusher 用 Firebase Admin SDK 送真 App 推播（A2）。
//
// 只處理 FCM 平台的 token；APNs 走另一條路（尚未實作）。憑證是 Firebase
// 服務帳戶 JSON（Console → 專案設定 → 服務帳戶 → 產生新的私密金鑰）。
type FCMPusher struct {
	client *messaging.Client
}

// NewFCMPusher 以服務帳戶憑證檔初始化。credentialsFile 為空或初始化失敗時回錯，
// 由呼叫端決定要不要降級成 LogPusher——**憑證問題不該讓整個服務起不來**。
func NewFCMPusher(ctx context.Context, credentialsFile string) (*FCMPusher, error) {
	if credentialsFile == "" {
		return nil, fmt.Errorf("未提供 FCM 憑證檔")
	}
	app, err := firebase.NewApp(ctx, nil, option.WithCredentialsFile(credentialsFile))
	if err != nil {
		return nil, fmt.Errorf("初始化 Firebase App 失敗: %w", err)
	}
	client, err := app.Messaging(ctx)
	if err != nil {
		return nil, fmt.Errorf("取得 Messaging client 失敗: %w", err)
	}
	return &FCMPusher{client: client}, nil
}

// SendRideOffer 送派單邀請推播。
//
// **同時帶 Notification 與 Data**：Notification 讓系統匣在 App 被殺／背景時自動顯示，
// 點擊喚醒 App；Data 是 App 端 fleetEventFromPushData 用來直接開接單卡的鍵值。
// Android Priority=high，讓被殺的 App 及時醒來。
// 逐台送、逐台判斷失敗——一台 token 失效（換機、重裝）不該讓其他裝置收不到；
// 全部失敗才回錯，讓 Dispatcher 打一條 log（但仍不中斷派單）。
func (p *FCMPusher) SendRideOffer(ctx context.Context, devices []Device, rideID int64, title, body string, data map[string]string) error {
	tokens := make([]string, 0, len(devices))
	for _, d := range devices {
		if d.Platform == PlatformFCM && d.Token != "" {
			tokens = append(tokens, d.Token)
		}
	}
	if len(tokens) == 0 {
		return nil // 沒有 FCM 裝置（可能全是 APNs）——不算錯
	}

	msg := &messaging.MulticastMessage{
		Tokens: tokens,
		Data:   data,
		Notification: &messaging.Notification{
			Title: title,
			Body:  body,
		},
		Android: &messaging.AndroidConfig{
			Priority: "high",
		},
	}

	resp, err := p.client.SendEachForMulticast(ctx, msg)
	if err != nil {
		// 整批失敗（例如憑證失效、網路不通）。
		return fmt.Errorf("FCM 送出失敗: %w", err)
	}

	if resp.FailureCount > 0 {
		for i, r := range resp.Responses {
			if !r.Success {
				log.Warn().
					Err(r.Error).
					Str("token_prefix", tokenPrefix(tokens[i])).
					Int64("ride_id", rideID).
					Msg("FCM 單台推播失敗（token 可能失效）")
			}
		}
		if resp.SuccessCount == 0 {
			return fmt.Errorf("FCM 全數失敗（%d 台）", resp.FailureCount)
		}
	}
	log.Info().
		Int("success", resp.SuccessCount).
		Int("failure", resp.FailureCount).
		Int64("ride_id", rideID).
		Msg("FCM 派單推播已送出")
	return nil
}
