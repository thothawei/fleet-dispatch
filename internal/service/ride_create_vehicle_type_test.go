package service

import (
	"context"
	"errors"
	"testing"

	"line-fleet-dispatch/internal/constants"
	lineclient "line-fleet-dispatch/internal/line"
	"line-fleet-dispatch/internal/repository"
)

func newVehicleTypeRideService(t *testing.T) (*RideService, *repository.RideRepository, *repository.CustomerRepository) {
	t.Helper()
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
	return NewRideService(customers, rides, redis, dispatch), rides, customers
}

// TestCreateByCustomer_指定車種持久化 P2：乘客指定的車種要真的落到 rides 上。
// 重讀 DB 而非只看回傳值——RideRepository.Create 走 raw INSERT，漏掉欄位時
// 回傳的 struct 仍然是對的，只有 DB 裡是空的。
func TestCreateByCustomer_指定車種持久化(t *testing.T) {
	svc, rides, customers := newVehicleTypeRideService(t)

	cust, err := customers.FindOrCreateByLineUserID("U_req_veh_pet", "寵物車乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}

	dropoffLat, dropoffLng := 25.08, 121.57
	created, err := svc.CreateByCustomer(context.Background(), cust.ID, CustomerCreateRequest{
		PickupLat:           25.03,
		PickupLng:           121.56,
		PickupAddress:       "台北車站",
		DropoffAddress:      "松山機場",
		DropoffLat:          &dropoffLat,
		DropoffLng:          &dropoffLng,
		RequiredVehicleType: constants.VehicleTypePet,
	})
	if err != nil {
		t.Fatalf("下單失敗：%v", err)
	}

	got, err := rides.GetByID(created.ID)
	if err != nil {
		t.Fatalf("讀回訂單失敗：%v", err)
	}
	if got.RequiredVehicleType != constants.VehicleTypePet {
		t.Fatalf("DB 裡的 required_vehicle_type 應為 pet，得到 %q", got.RequiredVehicleType)
	}
}

// TestCreateByCustomer_指定車種持久化_無目的地 Create 有「帶／不帶 dropoff」兩條 raw INSERT，
// 兩條都要帶車種——只補其中一條，無目的地的單（LINE 建單常見）就會靜靜地掉車種。
func TestCreateByCustomer_指定車種持久化_無目的地(t *testing.T) {
	svc, rides, customers := newVehicleTypeRideService(t)

	cust, err := customers.FindOrCreateByLineUserID("U_req_veh_nodrop", "無目的地乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}

	created, err := svc.CreateByCustomer(context.Background(), cust.ID, CustomerCreateRequest{
		PickupLat:           25.03,
		PickupLng:           121.56,
		PickupAddress:       "台北車站",
		RequiredVehicleType: constants.VehicleTypeAccessible,
	})
	if err != nil {
		t.Fatalf("下單失敗：%v", err)
	}

	got, err := rides.GetByID(created.ID)
	if err != nil {
		t.Fatalf("讀回訂單失敗：%v", err)
	}
	if got.RequiredVehicleType != constants.VehicleTypeAccessible {
		t.Fatalf("無目的地的單也該帶車種，得到 %q", got.RequiredVehicleType)
	}
}

// TestCreateByCustomer_未指定車種維持現行行為 P2 不可破壞既有 App／LINE 建單。
func TestCreateByCustomer_未指定車種維持現行行為(t *testing.T) {
	svc, rides, customers := newVehicleTypeRideService(t)

	cust, err := customers.FindOrCreateByLineUserID("U_req_veh_none", "一般乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}

	created, err := svc.CreateByCustomer(context.Background(), cust.ID, CustomerCreateRequest{
		PickupLat:     25.03,
		PickupLng:     121.56,
		PickupAddress: "台北車站",
	})
	if err != nil {
		t.Fatalf("未帶車種的下單不該失敗：%v", err)
	}
	got, err := rides.GetByID(created.ID)
	if err != nil {
		t.Fatalf("讀回訂單失敗：%v", err)
	}
	if got.RequiredVehicleType != "" {
		t.Fatalf("未指定車種應為空字串，得到 %q", got.RequiredVehicleType)
	}
}

// TestCreateByCustomer_非白名單車種回400 錯誤要在 service 就變成 ErrInvalidVehicleType，
// 而不是讓 INSERT 撞 DB CHECK——那會變成 500，乘客看到「伺服器錯誤」。
func TestCreateByCustomer_非白名單車種被拒(t *testing.T) {
	svc, _, customers := newVehicleTypeRideService(t)

	cust, err := customers.FindOrCreateByLineUserID("U_req_veh_bad", "亂填乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}

	_, err = svc.CreateByCustomer(context.Background(), cust.ID, CustomerCreateRequest{
		PickupLat:           25.03,
		PickupLng:           121.56,
		PickupAddress:       "台北車站",
		RequiredVehicleType: "spaceship",
	})
	if !errors.Is(err, ErrInvalidVehicleType) {
		t.Fatalf("預期 ErrInvalidVehicleType，得到 %v", err)
	}
}
