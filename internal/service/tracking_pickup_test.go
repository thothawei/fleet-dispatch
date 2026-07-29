package service

import (
	"context"
	"math"
	"testing"
	"time"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/events"
	lineclient "line-fleet-dispatch/internal/line"
	"line-fleet-dispatch/internal/model"
	"line-fleet-dispatch/internal/repository"
)

// pickUpFixture 建好「已接單」的訂單，回傳 tracking 服務、訂單與司機 id。
func pickUpFixture(t *testing.T, dropoff *model.GeoPoint, dropoffAddress string) (*TrackingService, int64, int64) {
	t.Helper()
	svc, rideID, driverID, _ := pickUpFixtureWithPublisher(t, dropoff, dropoffAddress)
	return svc, rideID, driverID
}

// pickUpFixtureWithPublisher 同上，但額外回傳注入的 fakePublisher 供事件斷言。
func pickUpFixtureWithPublisher(
	t *testing.T, dropoff *model.GeoPoint, dropoffAddress string,
) (*TrackingService, int64, int64, *fakePublisher) {
	t.Helper()
	db := newServiceTestDB(t)
	redis := newServiceTestRedis(t)

	customers := repository.NewCustomerRepository(db)
	rides := repository.NewRideRepository(db)
	drivers := repository.NewDriverRepository(db)
	tracks := repository.NewTrackRepository(db)

	cust, err := customers.FindOrCreateByLineUserID("U_pickup_customer", "乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}

	now := time.Now()
	driver := &model.Driver{
		LineUserID: "U_pickup_driver",
		Name:       "司機",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := db.Create(driver).Error; err != nil {
		t.Fatalf("建立司機失敗：%v", err)
	}

	ride := &model.Ride{
		CustomerID:     cust.ID,
		Status:         constants.RideStatusRequested,
		PickupPoint:    model.GeoPoint{Lat: 25.03, Lng: 121.56},
		PickupAddress:  "台北車站",
		DropoffAddress: dropoffAddress,
		DropoffPoint:   dropoff,
		RequestedAt:    now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := rides.Create(ride); err != nil {
		t.Fatalf("建立訂單失敗：%v", err)
	}
	if err := rides.AcceptRide(ride.ID, driver.ID, 300); err != nil {
		t.Fatalf("接單失敗：%v", err)
	}

	fp := &fakePublisher{}
	svc := NewTrackingService(
		drivers, rides, tracks, redis, lineclient.NewClient(""),
		nil, 0, 0, fp,
	)
	return svc, ride.ID, driver.ID, fp
}

func TestPickUp_回傳目的地座標(t *testing.T) {
	lat, lng := 25.08, 121.57
	svc, rideID, driverID := pickUpFixture(t,
		&model.GeoPoint{Lat: lat, Lng: lng}, "松山機場")

	res, err := svc.PickUp(context.Background(), rideID, driverID)
	if err != nil {
		t.Fatalf("PickUp 失敗：%v", err)
	}
	if res.DropoffAddress != "松山機場" {
		t.Fatalf("dropoff_address 錯誤：%q", res.DropoffAddress)
	}
	if !res.HasDropoffPoint {
		t.Fatal("HasDropoffPoint 應為 true")
	}
	if math.Abs(res.DropoffLat-lat) > 1e-6 || math.Abs(res.DropoffLng-lng) > 1e-6 {
		t.Fatalf("座標錯誤：lat=%f lng=%f", res.DropoffLat, res.DropoffLng)
	}
}

// LINE 叫車路徑沒有目的地，司機端須走「無座標」分支而非拿到 (0,0)。
func TestPickUp_無目的地時不回座標(t *testing.T) {
	svc, rideID, driverID := pickUpFixture(t, nil, "")

	res, err := svc.PickUp(context.Background(), rideID, driverID)
	if err != nil {
		t.Fatalf("PickUp 失敗：%v", err)
	}
	if res.HasDropoffPoint {
		t.Fatal("無目的地時 HasDropoffPoint 應為 false")
	}
	if res.DropoffAddress != "" {
		t.Fatalf("dropoff_address 應為空：%q", res.DropoffAddress)
	}
}

func TestPickUp_非該訂單司機被拒(t *testing.T) {
	svc, rideID, driverID := pickUpFixture(t, nil, "")

	if _, err := svc.PickUp(context.Background(), rideID, driverID+999); err != ErrForbidden {
		t.Fatalf("預期 ErrForbidden，得到 %v", err)
	}
}

// TestPickUp_也推給司機 「乘客已上車」的事件先前只推乘客。
//
// 多裝置／跨管道下這會讓司機的另一台裝置停在「前往上車點」階段——按鈕還是
// 「乘客已上車」，按下去只會被後端拒絕（狀態已是 PickedUp），「放棄此單」這時也已不可用。
// App 端本來就有 ride.picked_up 的處理（切到 onTrip 階段），缺的一直是這則事件。
// 與 ride.stop_updated（#53）同一原則。
func TestPickUp_也推給司機(t *testing.T) {
	svc, rideID, driverID, fp := pickUpFixtureWithPublisher(t, nil, "")

	if _, err := svc.PickUp(context.Background(), rideID, driverID); err != nil {
		t.Fatalf("上車標記應成功：%v", err)
	}

	var toCustomer, toDriver *events.Event
	for i := range fp.recv {
		r := fp.recv[i]
		if r.Ev.Type != events.TypeRidePickedUp {
			continue
		}
		switch r.Rec.Role {
		case events.RoleCustomer:
			toCustomer = &fp.recv[i].Ev
		case events.RoleDriver:
			if r.Rec.ID == driverID {
				toDriver = &fp.recv[i].Ev
			}
		}
	}
	if toCustomer == nil {
		t.Fatalf("乘客沒收到 %s；實際發佈：%+v", events.TypeRidePickedUp, fp.recv)
	}
	if toDriver == nil {
		t.Fatalf("**司機**沒收到 %s——他的另一台裝置會停在前往上車點階段；實際發佈：%+v",
			events.TypeRidePickedUp, fp.recv)
	}
	if toDriver.RideID != rideID {
		t.Fatalf("司機收到的事件 ride_id 應為 %d，得到 %d", rideID, toDriver.RideID)
	}
}
