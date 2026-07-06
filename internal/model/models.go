package model

import (
	"database/sql/driver"
	"fmt"
	"time"
)

// GeoPoint 對應 PostGIS geography(Point,4326)，儲存順序為 lng, lat
type GeoPoint struct {
	Lat float64
	Lng float64
}

func (g GeoPoint) Value() (driver.Value, error) {
	if g.Lat == 0 && g.Lng == 0 {
		return nil, fmt.Errorf("無效的地理座標")
	}
	return fmt.Sprintf("SRID=4326;POINT(%f %f)", g.Lng, g.Lat), nil
}

func (g *GeoPoint) Scan(value interface{}) error {
	// M1 僅寫入，讀取留待 M4
	return nil
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
	ID             int64      `gorm:"primaryKey"`
	CustomerID     int64      `gorm:"not null"`
	DriverID       *int64     `gorm:""`
	Status         int16      `gorm:"not null;default:0"`
	PickupPoint    GeoPoint   `gorm:"column:pickup_point;type:geography(Point,4326);not null"`
	PickupAddress  string     `gorm:"not null;default:''"`
	DropoffPoint   *GeoPoint  `gorm:"column:dropoff_point;type:geography(Point,4326)"`
	DropoffAddress string     `gorm:"not null;default:''"`
	RequestedAt    time.Time  `gorm:"not null"`
	AcceptedAt     *time.Time `gorm:""`
	PickedUpAt     *time.Time `gorm:""`
	CompletedAt    *time.Time `gorm:""`
	DistanceM      *int       `gorm:"column:distance_m"`
	EtaPickupSec   *int       `gorm:"column:eta_pickup_sec"`
	CreatedAt      time.Time  `gorm:"not null"`
	UpdatedAt      time.Time  `gorm:"not null"`
}

func (Ride) TableName() string {
	return "rides"
}

type Admin struct {
	ID           int64     `gorm:"primaryKey"`
	Email        string    `gorm:"uniqueIndex;not null"`
	PasswordHash string    `gorm:"column:password_hash;not null;default:''"`
	Name         string    `gorm:"not null;default:''"`
	CreatedAt    time.Time `gorm:"not null"`
	UpdatedAt    time.Time `gorm:"not null"`
}

func (Admin) TableName() string {
	return "admins"
}
