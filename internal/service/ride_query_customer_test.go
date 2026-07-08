package service

import (
	"errors"
	"testing"
	"time"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/model"
	"line-fleet-dispatch/internal/repository"
)

// TestGetActiveRideByCustomer_無進行中訂單回nil 驗收條件 #2：乘客無進行中訂單時回 (nil, nil)，非錯誤
func TestGetActiveRideByCustomer_無進行中訂單回nil(t *testing.T) {
	db := newServiceTestDB(t)
	customers := repository.NewCustomerRepository(db)
	tracks := repository.NewTrackRepository(db)
	rides := repository.NewRideRepository(db)

	cust, err := customers.FindOrCreateByLineUserID("U_active_empty", "測試乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}

	q := NewRideQueryService(tracks, rides)
	got, err := q.GetActiveRideByCustomer(cust.ID)
	if err != nil {
		t.Fatalf("預期無錯誤，得到 %v", err)
	}
	if got != nil {
		t.Fatalf("預期無進行中訂單回 nil，卻得到 ride id=%d", got.ID)
	}
}

// TestGetActiveRideByCustomer_有進行中訂單回該筆 有訂單時應正確回傳
func TestGetActiveRideByCustomer_有進行中訂單回該筆(t *testing.T) {
	db := newServiceTestDB(t)
	customers := repository.NewCustomerRepository(db)
	tracks := repository.NewTrackRepository(db)
	rides := repository.NewRideRepository(db)

	cust, err := customers.FindOrCreateByLineUserID("U_active_hit", "測試乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	ride := newTestRide(t, rides, cust.ID, constants.RideStatusRequested)

	q := NewRideQueryService(tracks, rides)
	got, err := q.GetActiveRideByCustomer(cust.ID)
	if err != nil {
		t.Fatalf("查詢失敗：%v", err)
	}
	if got == nil || got.ID != ride.ID {
		t.Fatalf("預期回進行中訂單 id=%d，得到 %v", ride.ID, got)
	}
}

// TestGetRideForCustomer_本人訂單可查 驗收條件：本人訂單可正常查到
func TestGetRideForCustomer_本人訂單可查(t *testing.T) {
	db := newServiceTestDB(t)
	customers := repository.NewCustomerRepository(db)
	tracks := repository.NewTrackRepository(db)
	rides := repository.NewRideRepository(db)

	cust, err := customers.FindOrCreateByLineUserID("U_owner_ok", "測試乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	ride := newTestRide(t, rides, cust.ID, constants.RideStatusRequested)

	q := NewRideQueryService(tracks, rides)
	got, err := q.GetRideForCustomer(cust.ID, ride.ID)
	if err != nil {
		t.Fatalf("預期可查到本人訂單，得到錯誤 %v", err)
	}
	if got.ID != ride.ID {
		t.Fatalf("預期 ride id=%d，得到 %d", ride.ID, got.ID)
	}
}

// TestGetRideForCustomer_他人訂單回Forbidden 驗收條件 #3：非本人訂單須被拒
func TestGetRideForCustomer_他人訂單回Forbidden(t *testing.T) {
	db := newServiceTestDB(t)
	customers := repository.NewCustomerRepository(db)
	tracks := repository.NewTrackRepository(db)
	rides := repository.NewRideRepository(db)

	owner, err := customers.FindOrCreateByLineUserID("U_owner_a", "乘客A")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	other, err := customers.FindOrCreateByLineUserID("U_owner_b", "乘客B")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	ride := newTestRide(t, rides, owner.ID, constants.RideStatusRequested)

	q := NewRideQueryService(tracks, rides)
	_, err = q.GetRideForCustomer(other.ID, ride.ID)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("非本人訂單預期 ErrForbidden，得到 %v", err)
	}
}

// TestGetRideForCustomer_不存在訂單回NotFound 訂單 id 不存在時回 ErrNotFound
func TestGetRideForCustomer_不存在訂單回NotFound(t *testing.T) {
	db := newServiceTestDB(t)
	tracks := repository.NewTrackRepository(db)
	rides := repository.NewRideRepository(db)

	q := NewRideQueryService(tracks, rides)
	_, err := q.GetRideForCustomer(1, 999999)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在的訂單預期 ErrNotFound，得到 %v", err)
	}
}

// newTestRide 建立一筆測試用訂單，供各整合測試共用
func newTestRide(t *testing.T, rides *repository.RideRepository, customerID int64, status int16) *model.Ride {
	t.Helper()
	now := time.Now()
	ride := &model.Ride{
		CustomerID:    customerID,
		Status:        status,
		PickupPoint:   model.GeoPoint{Lat: 25.03, Lng: 121.56},
		PickupAddress: "台北車站",
		RequestedAt:   now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := rides.Create(ride); err != nil {
		t.Fatalf("建立訂單失敗：%v", err)
	}
	if status != constants.RideStatusRequested {
		if err := rides.UpdateStatus(ride.ID, status); err != nil {
			t.Fatalf("更新訂單狀態失敗：%v", err)
		}
		ride.Status = status
	}
	return ride
}
