package model

import (
	"database/sql/driver"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"time"
)

// GeoPoint 對應 PostGIS geography(Point,4326)，儲存順序為 lng, lat
type GeoPoint struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

func (g GeoPoint) Value() (driver.Value, error) {
	if g.Lat == 0 && g.Lng == 0 {
		return nil, fmt.Errorf("無效的地理座標")
	}
	return fmt.Sprintf("SRID=4326;POINT(%f %f)", g.Lng, g.Lat), nil
}

// Scan 解析 GORM 從 PostGIS geography 欄位讀出的值。
// pgx 讀 geography 預設回傳 EWKB 的十六進位字串（如 "0101000020E6100000..."），
// 少數情況也可能是 EWKB 原始位元組；兩者皆支援。nil（欄位為 NULL）視為 no-op。
func (g *GeoPoint) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	var raw []byte
	switch v := value.(type) {
	case []byte:
		raw = v
	case string:
		raw = []byte(v)
	default:
		return fmt.Errorf("GeoPoint.Scan: 不支援的型別 %T", value)
	}
	if b, ok := decodeHexBytes(raw); ok {
		raw = b
	}
	lng, lat, err := parseEWKBPoint(raw)
	if err != nil {
		return err
	}
	g.Lng, g.Lat = lng, lat
	return nil
}

// decodeHexBytes 若 raw 整段是十六進位文字（偶數長度且全為 hex 字元）則解碼成位元組，
// 否則回傳 (nil, false) 表示 raw 本身已是原始位元組。
func decodeHexBytes(raw []byte) ([]byte, bool) {
	if len(raw) == 0 || len(raw)%2 != 0 {
		return nil, false
	}
	for _, c := range raw {
		isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
		if !isHex {
			return nil, false
		}
	}
	b, err := hex.DecodeString(string(raw))
	if err != nil {
		return nil, false
	}
	return b, true
}

// parseEWKBPoint 解析單一 EWKB Point（含可選 SRID 旗標），回傳 lng, lat。
func parseEWKBPoint(b []byte) (lng, lat float64, err error) {
	if len(b) < 5 {
		return 0, 0, fmt.Errorf("EWKB 過短：%d 位元組", len(b))
	}
	var bo binary.ByteOrder
	switch b[0] {
	case 0:
		bo = binary.BigEndian
	case 1:
		bo = binary.LittleEndian
	default:
		return 0, 0, fmt.Errorf("未知的 EWKB 位元組序：0x%02x", b[0])
	}
	typ := bo.Uint32(b[1:5])
	if typ&0xffff != 1 { // 低位為幾何型別，Point = 1
		return 0, 0, fmt.Errorf("非 Point 幾何型別：0x%08x", typ)
	}
	off := 5
	const sridFlag = 0x20000000
	if typ&sridFlag != 0 {
		off += 4 // 跳過 4 位元組 SRID
	}
	if len(b) < off+16 {
		return 0, 0, fmt.Errorf("EWKB Point 座標長度不足")
	}
	lng = math.Float64frombits(bo.Uint64(b[off : off+8]))
	lat = math.Float64frombits(bo.Uint64(b[off+8 : off+16]))
	return lng, lat, nil
}

type Customer struct {
	ID           int64     `gorm:"primaryKey"`
	LineUserID   string    `gorm:"column:line_user_id;uniqueIndex;not null"`
	Name         string    `gorm:"not null;default:''"`
	Phone        string    `gorm:"not null;default:''"`
	PasswordHash string    `gorm:"column:password_hash;not null;default:''" json:"-"`
	CreatedAt    time.Time `gorm:"not null"`
	UpdatedAt    time.Time `gorm:"not null"`
}

func (Customer) TableName() string {
	return "customers"
}

type Driver struct {
	ID         int64  `gorm:"primaryKey"`
	LineUserID string `gorm:"column:line_user_id;uniqueIndex;not null"`
	Name       string `gorm:"not null;default:''"`
	Phone      string `gorm:"not null;default:''"`
	Status     int16  `gorm:"not null;default:0"`
	// VehicleType 車種 code（O1）：constants.VehicleType* 之一；'' 為未設定。
	// 未設定的司機不得被派單／接單（O3），且它是清潔費（O6）與派單車種過濾（P3）的判斷依據。
	VehicleType string `gorm:"column:vehicle_type;not null;default:''" json:"vehicle_type"`
	// PlateNumber 車牌（O1）：非空時唯一（partial unique index），'' 為未設定。
	PlateNumber  string    `gorm:"column:plate_number;not null;default:''" json:"plate_number"`
	PasswordHash string    `gorm:"column:password_hash;not null;default:''" json:"-"`
	CreatedAt    time.Time `gorm:"not null"`
	UpdatedAt    time.Time `gorm:"not null"`
}

// HasVehicle 是否已填妥車輛資訊；未填者不得被派單／接單（O3 gate）。
func (d Driver) HasVehicle() bool {
	return d.VehicleType != "" && d.PlateNumber != ""
}

func (Driver) TableName() string {
	return "drivers"
}

type Ride struct {
	ID             int64      `gorm:"primaryKey" json:"id"`
	CustomerID     int64      `gorm:"not null" json:"customer_id"`
	DriverID       *int64     `gorm:"" json:"driver_id"`
	Status         int16      `gorm:"not null;default:0" json:"status"`
	PickupPoint    GeoPoint   `gorm:"column:pickup_point;type:geography(Point,4326);not null" json:"pickup_point"`
	PickupAddress  string     `gorm:"not null;default:''" json:"pickup_address"`
	DropoffPoint   *GeoPoint  `gorm:"column:dropoff_point;type:geography(Point,4326)" json:"dropoff_point"`
	DropoffAddress string     `gorm:"not null;default:''" json:"dropoff_address"`
	RequestedAt    time.Time  `gorm:"not null" json:"requested_at"`
	AcceptedAt     *time.Time `gorm:"" json:"accepted_at"`
	PickedUpAt     *time.Time `gorm:"" json:"picked_up_at"`
	CompletedAt    *time.Time `gorm:"" json:"completed_at"`
	DistanceM      *int       `gorm:"column:distance_m" json:"distance_m"`
	EtaPickupSec   *int       `gorm:"column:eta_pickup_sec" json:"eta_pickup_sec"`
	// RequiredVehicleType 乘客指定的車種（P1）：constants.VehicleType* 之一；'' ＝不指定（任何車種都可派）。
	// 這是清潔費（O6）加收與否的判斷依據——依**乘客要的車種**，不是司機開的車種。
	RequiredVehicleType string `gorm:"column:required_vehicle_type;not null;default:''" json:"required_vehicle_type"`
	// 計費欄位：完成時定格寫入（費率快照制），一律以「分」儲存；未完成/取消為 nil。
	FareAmountCents       *int64    `gorm:"column:fare_amount_cents" json:"fare_amount_cents"`
	CommissionAmountCents *int64    `gorm:"column:commission_amount_cents" json:"commission_amount_cents"`
	DriverNetAmountCents  *int64    `gorm:"column:driver_net_amount_cents" json:"driver_net_amount_cents"`
	CreatedAt             time.Time `gorm:"not null" json:"created_at"`
	UpdatedAt             time.Time `gorm:"not null" json:"updated_at"`
}

func (Ride) TableName() string {
	return "rides"
}

// FleetSettings 費率設定（單列，id 固定為 1）。金額以「分」儲存，手續費以 bps 儲存。
type FleetSettings struct {
	ID                        int16     `gorm:"primaryKey" json:"-"`
	BaseFareCents             int64     `gorm:"column:base_fare_cents;not null" json:"base_fare_cents"`
	PerKmFareCents            int64     `gorm:"column:per_km_fare_cents;not null" json:"per_km_fare_cents"`
	MinFareCents              int64     `gorm:"column:min_fare_cents;not null" json:"min_fare_cents"`
	CommissionBps             int       `gorm:"column:commission_bps;not null" json:"commission_bps"`
	MonthlyMembershipFeeCents int64     `gorm:"column:monthly_membership_fee_cents;not null" json:"monthly_membership_fee_cents"`
	LostItemFeeBps            int       `gorm:"column:lost_item_fee_bps;not null" json:"lost_item_fee_bps"`
	UpdatedBy                 *int64    `gorm:"column:updated_by" json:"updated_by"`
	UpdatedAt                 time.Time `gorm:"column:updated_at;not null" json:"updated_at"`
}

func (FleetSettings) TableName() string {
	return "fleet_settings"
}

// MembershipInvoice 會費帳單（F8）。每月一列/司機，amount_cents 為產生時定格的月會費。
type MembershipInvoice struct {
	ID          int64      `gorm:"primaryKey" json:"id"`
	DriverID    int64      `gorm:"column:driver_id;not null" json:"driver_id"`
	Period      string     `gorm:"column:period;not null" json:"period"` // YYYY-MM
	AmountCents int64      `gorm:"column:amount_cents;not null" json:"amount_cents"`
	Status      string     `gorm:"column:status;not null;default:'unpaid'" json:"status"`
	PaidAt      *time.Time `gorm:"column:paid_at" json:"paid_at"`
	CreatedAt   time.Time  `gorm:"not null" json:"created_at"`
	UpdatedAt   time.Time  `gorm:"not null" json:"updated_at"`
}

func (MembershipInvoice) TableName() string {
	return "membership_invoices"
}

// RideMessage 行程內對話訊息（乘客↔司機）。即時遞送走 WebSocket（chat.message），本表為歷史真源。
type RideMessage struct {
	ID         int64     `gorm:"primaryKey" json:"id"`
	RideID     int64     `gorm:"column:ride_id;not null" json:"ride_id"`
	SenderRole string    `gorm:"column:sender_role;not null" json:"sender_role"`
	SenderID   int64     `gorm:"column:sender_id;not null" json:"sender_id"`
	Body       string    `gorm:"not null" json:"body"`
	CreatedAt  time.Time `gorm:"not null" json:"created_at"`
}

func (RideMessage) TableName() string {
	return "ride_messages"
}

// LostItemRequest 遺失物協尋單。fee_cents／fee_bps 於建立當下依「該趟車資 × 處理費%」定格快照，
// 日後調整處理費%不影響既有協尋單（與車資/手續費同一套快照制）。
type LostItemRequest struct {
	ID          int64      `gorm:"primaryKey" json:"id"`
	RideID      int64      `gorm:"column:ride_id;not null" json:"ride_id"`
	CustomerID  int64      `gorm:"column:customer_id;not null" json:"customer_id"`
	DriverID    int64      `gorm:"column:driver_id;not null" json:"driver_id"`
	Description string     `gorm:"not null" json:"description"`
	FeeCents    int64      `gorm:"column:fee_cents;not null" json:"fee_cents"`
	FeeBps      int        `gorm:"column:fee_bps;not null" json:"fee_bps"`
	Status      string     `gorm:"not null;default:'open'" json:"status"`
	PaidAt      *time.Time `gorm:"column:paid_at" json:"paid_at"`
	CreatedAt   time.Time  `gorm:"not null" json:"created_at"`
	UpdatedAt   time.Time  `gorm:"not null" json:"updated_at"`
}

func (LostItemRequest) TableName() string {
	return "lost_item_requests"
}

type Admin struct {
	ID           int64     `gorm:"primaryKey"`
	Username     string    `gorm:"column:username;uniqueIndex;not null"`
	PasswordHash string    `gorm:"column:password_hash;not null;default:''" json:"-"`
	Name         string    `gorm:"not null;default:''"`
	Role         string    `gorm:"column:role;not null;default:'superadmin'"`
	IsActive     bool      `gorm:"column:is_active;not null;default:true"`
	CreatedAt    time.Time `gorm:"not null"`
	UpdatedAt    time.Time `gorm:"not null"`
}

func (Admin) TableName() string {
	return "admins"
}

// DeviceToken 存 FCM/APNs 裝置推播 token（App 被殺仍可收派單）。
type DeviceToken struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	Role      string    `gorm:"not null" json:"role"`
	SubjectID int64     `gorm:"column:subject_id;not null" json:"subject_id"`
	Platform  string    `gorm:"not null" json:"platform"`
	Token     string    `gorm:"not null" json:"token"`
	CreatedAt time.Time `gorm:"not null" json:"created_at"`
	UpdatedAt time.Time `gorm:"not null" json:"updated_at"`
}

func (DeviceToken) TableName() string {
	return "device_tokens"
}

// RideEvent 訂單狀態轉換審計（D4）。
type RideEvent struct {
	ID         int64     `gorm:"primaryKey" json:"id"`
	RideID     int64     `gorm:"column:ride_id;not null" json:"ride_id"`
	FromStatus *int16    `gorm:"column:from_status" json:"from_status"`
	ToStatus   int16     `gorm:"column:to_status;not null" json:"to_status"`
	EventType  string    `gorm:"column:event_type;not null" json:"event_type"`
	ActorRole  string    `gorm:"column:actor_role;not null;default:''" json:"actor_role"`
	ActorID    *int64    `gorm:"column:actor_id" json:"actor_id"`
	Note       string    `gorm:"not null;default:''" json:"note"`
	CreatedAt  time.Time `gorm:"not null" json:"created_at"`
}

func (RideEvent) TableName() string {
	return "ride_events"
}
