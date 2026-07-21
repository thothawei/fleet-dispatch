// Package events 定義即時事件的型別與發佈介面。
// Publisher 由 WebSocket Hub 實作；業務服務只依賴此介面，方便測試替身注入。
package events

import "encoding/json"

// 角色：對應 JWT Subject
const (
	RoleDriver   = "driver"
	RoleCustomer = "customer"
	RoleAdmin    = "admin"
)

// 事件型別
const (
	TypeRideRequested    = "ride.requested"    // 乘客叫車
	TypeRideAssigned     = "ride.assigned"     // 已派單給司機（待接）
	TypeRideAccepted     = "ride.accepted"     // 司機已接單
	TypeDriverLocation   = "driver.location"   // 司機位置更新（後台車隊 / 乘客追蹤）
	TypeDriverArrived    = "driver.arrived"    // 司機進入上車圍籬
	TypeRidePickedUp     = "ride.picked_up"    // 乘客已上車
	TypeRideCompleted    = "ride.completed"    // 行程完成
	TypeRideCancelled    = "ride.cancelled"    // 行程取消
	TypeRideRedispatched = "ride.redispatched" // 司機放棄後重回待派
	TypeChatMessage      = "chat.message"      // 行程內對話訊息（乘客↔司機即時遞送）
	TypeLostItemCreated  = "lost_item.created" // 乘客建立遺失物協尋單
	TypeLostItemUpdated  = "lost_item.updated" // 協尋單狀態變更（found/paid/returned/closed）
	// TypeRideStopUpdated 司機標記到達／跳過某一站（N7）。payload 帶**整趟 stops**，
	// 收到整批覆蓋即可，不必自己套用差異——乘客端據此顯示「走到第幾站」。
	TypeRideStopUpdated = "ride.stop_updated"
)

// 審計 actor_role
const (
	ActorCustomer = "customer"
	ActorDriver   = "driver"
	ActorAdmin    = "admin"
	ActorSystem   = "system"
)

// Recipient 事件收件人。ID=0 代表該角色的廣播（例如後台車隊看板）。
type Recipient struct {
	Role string
	ID   int64
}

// Event 送往前端的即時事件。
type Event struct {
	Type    string         `json:"type"`
	RideID  int64          `json:"ride_id,omitempty"`
	Payload map[string]any `json:"payload,omitempty"`
}

// JSON 序列化為前端可解析的位元組。
func (e Event) JSON() ([]byte, error) {
	return json.Marshal(e)
}

// Publisher 發佈事件到對應收件人。Hub 為其唯一實作。
type Publisher interface {
	Publish(rec Recipient, ev Event)
}
