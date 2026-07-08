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
	PasswordHash string    `gorm:"column:password_hash;not null;default:''"`
	CreatedAt    time.Time `gorm:"not null"`
	UpdatedAt    time.Time `gorm:"not null"`
}

func (Customer) TableName() string {
	return "customers"
}

type Driver struct {
	ID           int64     `gorm:"primaryKey"`
	LineUserID   string    `gorm:"column:line_user_id;uniqueIndex;not null"`
	Name         string    `gorm:"not null;default:''"`
	Phone        string    `gorm:"not null;default:''"`
	Status       int16     `gorm:"not null;default:0"`
	PasswordHash string    `gorm:"column:password_hash;not null;default:''"`
	CreatedAt    time.Time `gorm:"not null"`
	UpdatedAt    time.Time `gorm:"not null"`
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
	CreatedAt      time.Time  `gorm:"not null" json:"created_at"`
	UpdatedAt      time.Time  `gorm:"not null" json:"updated_at"`
}

func (Ride) TableName() string {
	return "rides"
}

type Admin struct {
	ID           int64     `gorm:"primaryKey"`
	Username     string    `gorm:"column:username;uniqueIndex;not null"`
	PasswordHash string    `gorm:"column:password_hash;not null;default:''"`
	Name         string    `gorm:"not null;default:''"`
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
