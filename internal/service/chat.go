package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"line-fleet-dispatch/internal/events"
	"line-fleet-dispatch/internal/model"
	"line-fleet-dispatch/internal/notify"
	"line-fleet-dispatch/internal/repository"
)

var (
	ErrEmptyMessage   = errors.New("訊息內容不可為空")
	ErrMessageTooLong = errors.New("訊息長度超過上限")
)

// chatMaxRunes 單則訊息長度上限（rune 數；DB 另有 char_length ≤ 1000 的最後防線）。
const chatMaxRunes = 500

// ChatService 乘客↔司機行程內對話：訊息持久化 + WebSocket 即時遞送（chat.message）
// + App 推播給**收訊那一方**（對方 App 不在前景時 WS 早就斷了）。
type ChatService struct {
	rides     *repository.RideRepository
	messages  *repository.RideMessageRepository
	publisher events.Publisher
	appNotify *notify.Dispatcher
}

func NewChatService(rides *repository.RideRepository, messages *repository.RideMessageRepository, publisher events.Publisher) *ChatService {
	return &ChatService{rides: rides, messages: messages, publisher: publisher}
}

// SetAppNotifier 注入 App 推播；可選，未注入時只有 WS
// （＝對方 App 一離開前景，訊息就要等他自己再打開才看得到）。
func (s *ChatService) SetAppNotifier(d *notify.Dispatcher) {
	s.appNotify = d
}

// Send 驗證發話者是該趟行程的乘客／被指派司機後寫入訊息，並即時推播給行程雙方。
// 行程任何狀態皆可傳訊（完成後的遺失物協尋也走同一條對話）。
func (s *ChatService) Send(role string, senderID, rideID int64, body string) (*model.RideMessage, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, ErrEmptyMessage
	}
	if utf8.RuneCountInString(body) > chatMaxRunes {
		return nil, ErrMessageTooLong
	}
	ride, err := s.rides.GetByID(rideID)
	if err != nil {
		return nil, ErrNotFound
	}
	if err := authorizeRideParticipant(ride, role, senderID); err != nil {
		return nil, err
	}
	msg := &model.RideMessage{
		RideID:     rideID,
		SenderRole: role,
		SenderID:   senderID,
		Body:       body,
		CreatedAt:  time.Now(),
	}
	if err := s.messages.Create(msg); err != nil {
		return nil, err
	}
	s.publishToRideParties(ride, events.Event{
		Type:    events.TypeChatMessage,
		RideID:  rideID,
		Payload: rideMessagePayload(msg),
	})
	s.pushToRecipient(ride, msg)
	return msg, nil
}

// pushToRecipient 推播給**對方**（不推給發話者自己的其他裝置——他知道自己說了什麼，
// 而系統匣跳出自己剛送出的訊息只會讓人以為對方回話了）。
//
// 用 context.Background() 而非請求的 ctx：HTTP 回應一送出，請求 ctx 就被取消，
// 推播是回應之後才完成的背景動作，掛在請求 ctx 上會被腰斬。
func (s *ChatService) pushToRecipient(ride *model.Ride, msg *model.RideMessage) {
	if s.appNotify == nil {
		return
	}
	ctx := context.Background()
	data := map[string]string{
		"type":    events.TypeChatMessage,
		"ride_id": strconv.FormatInt(ride.ID, 10),
	}
	// 訊息內容放在通知本文（使用者看得到就是要看內容），**不放進 data**——
	// App 醒來後重讀對話 REST 拿完整歷史，避免推播裡的一則與伺服器狀態各說各話。
	body := previewText(msg.Body)
	switch msg.SenderRole {
	case events.RoleCustomer:
		if ride.DriverID != nil {
			s.appNotify.NotifyDriverRideUpdate(ctx, *ride.DriverID, ride.ID, "乘客傳來訊息", body, data)
		}
	case events.RoleDriver:
		s.appNotify.NotifyCustomerRideUpdate(ctx, ride.CustomerID, ride.ID, "司機傳來訊息", body, data)
	}
}

// previewText 通知本文的預覽長度上限（rune 計，避免切壞中文）。
func previewText(body string) string {
	const maxRunes = 60
	runes := []rune(body)
	if len(runes) <= maxRunes {
		return body
	}
	return string(runes[:maxRunes]) + "…"
}

// List 讀取歷史訊息（本趟乘客／司機；admin 唯讀可稽核），afterID 供增量補讀。
func (s *ChatService) List(role string, subjectID, rideID, afterID int64, limit int) ([]model.RideMessage, error) {
	ride, err := s.rides.GetByID(rideID)
	if err != nil {
		return nil, ErrNotFound
	}
	if role != events.RoleAdmin {
		if err := authorizeRideParticipant(ride, role, subjectID); err != nil {
			return nil, err
		}
	}
	return s.messages.ListByRide(rideID, afterID, limit)
}

// authorizeRideParticipant 僅允許本趟乘客或被指派司機。
func authorizeRideParticipant(ride *model.Ride, role string, subjectID int64) error {
	switch role {
	case events.RoleCustomer:
		if ride.CustomerID == subjectID {
			return nil
		}
	case events.RoleDriver:
		if ride.DriverID != nil && *ride.DriverID == subjectID {
			return nil
		}
	}
	return ErrForbidden
}

// publishToRideParties 推播給行程雙方（含發話者本人的其他裝置；App 端以訊息 id 去重）。
func (s *ChatService) publishToRideParties(ride *model.Ride, ev events.Event) {
	if s.publisher == nil {
		return
	}
	s.publisher.Publish(events.Recipient{Role: events.RoleCustomer, ID: ride.CustomerID}, ev)
	if ride.DriverID != nil {
		s.publisher.Publish(events.Recipient{Role: events.RoleDriver, ID: *ride.DriverID}, ev)
	}
}

// rideMessagePayload 序列化訊息為 WS 事件 payload（與 REST 回應同欄位）。
func rideMessagePayload(m *model.RideMessage) map[string]any {
	return map[string]any{
		"id":          m.ID,
		"ride_id":     m.RideID,
		"sender_role": m.SenderRole,
		"sender_id":   m.SenderID,
		"body":        m.Body,
		"created_at":  m.CreatedAt.Format(time.RFC3339),
	}
}
