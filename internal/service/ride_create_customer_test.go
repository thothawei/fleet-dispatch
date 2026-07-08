package service

import (
	"context"
	"math"
	"testing"

	"line-fleet-dispatch/internal/constants"
	lineclient "line-fleet-dispatch/internal/line"
	"line-fleet-dispatch/internal/repository"
)

// TestCreateByCustomer_寫入Dropoff 驗證 App 下單 body 的 dropoff_address/lat/lng 會持久化。
func TestCreateByCustomer_寫入Dropoff(t *testing.T) {
	db := newServiceTestDB(t)
	customers := repository.NewCustomerRepository(db)
	rides := repository.NewRideRepository(db)
	redis := newServiceTestRedis(t)
	dispatch := NewDispatchService(
		repository.NewDriverRepository(db),
		rides,
		customers,
		redis,
		lineclient.NewClient(""),
		nil, NewDispatchSettings(3000, 5, 20, 1, 5), nil,
	)
	svc := NewRideService(customers, rides, redis, dispatch)

	cust, err := customers.FindOrCreateByLineUserID("U_dropoff_create", "目的地測試")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}

	dropoffLat, dropoffLng := 25.08, 121.57
	created, err := svc.CreateByCustomer(context.Background(), cust.ID, CustomerCreateRequest{
		PickupLat:      25.03,
		PickupLng:      121.56,
		PickupAddress:  "台北車站",
		DropoffAddress: "松山機場",
		DropoffLat:     &dropoffLat,
		DropoffLng:     &dropoffLng,
	})
	if err != nil {
		t.Fatalf("下單失敗：%v", err)
	}

	got, err := rides.GetByID(created.ID)
	if err != nil {
		t.Fatalf("讀回訂單失敗：%v", err)
	}
	if got.DropoffAddress != "松山機場" {
		t.Fatalf("dropoff_address 錯誤：%q", got.DropoffAddress)
	}
	if got.DropoffPoint == nil {
		t.Fatal("dropoff_point 為 nil")
	}
	if math.Abs(got.DropoffPoint.Lat-dropoffLat) > 1e-6 || math.Abs(got.DropoffPoint.Lng-dropoffLng) > 1e-6 {
		t.Fatalf("dropoff_point 讀回錯誤：lat=%f lng=%f", got.DropoffPoint.Lat, got.DropoffPoint.Lng)
	}
	if got.Status != constants.RideStatusRequested {
		t.Fatalf("status 預期 %d，得到 %d", constants.RideStatusRequested, got.Status)
	}
}
