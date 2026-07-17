package service

import (
	"context"
	"errors"
	"math"
	"testing"

	"line-fleet-dispatch/internal/constants"
	lineclient "line-fleet-dispatch/internal/line"
	"line-fleet-dispatch/internal/repository"
)

func newStopsRideService(t *testing.T) (*RideService, *repository.RideRepository, *repository.RideStopRepository, *repository.CustomerRepository) {
	t.Helper()
	db := newServiceTestDB(t)
	customers := repository.NewCustomerRepository(db)
	rides := repository.NewRideRepository(db)
	stops := repository.NewRideStopRepository(db)
	redis := newServiceTestRedis(t)
	dispatch := NewDispatchService(
		repository.NewDriverRepository(db), rides, customers, redis,
		lineclient.NewClient(""), nil, NewDispatchSettings(3000, 5, 20, 1, 5), nil,
	)
	svc := NewRideService(customers, rides, redis, dispatch)
	svc.SetStops(stops)
	return svc, rides, stops, customers
}

// 兩人四停：A 在台北車站上、B 在市政府上、A 在松山機場下、B 在台北 101 下。
func fourStopInputs() []StopInput {
	return []StopInput{
		{Seq: 1, Kind: constants.StopKindPickup, Lat: 25.0478, Lng: 121.5170, Address: "台北車站", PassengerLabel: "A"},
		{Seq: 2, Kind: constants.StopKindPickup, Lat: 25.0339, Lng: 121.5645, Address: "市政府", PassengerLabel: "B"},
		{Seq: 3, Kind: constants.StopKindDropoff, Lat: 25.0697, Lng: 121.5522, Address: "松山機場", PassengerLabel: "A"},
		{Seq: 4, Kind: constants.StopKindDropoff, Lat: 25.0339, Lng: 121.5645, Address: "台北101", PassengerLabel: "B"},
	}
}

// TestCreateByCustomer_多停靠點落庫 N1+N3：stops 寫入，且 rides 的 pickup/dropoff
// 由 stops 推導——這是相容性的核心，派單／導航／報表都還讀 rides。
func TestCreateByCustomer_多停靠點落庫(t *testing.T) {
	svc, rides, stopsRepo, customers := newStopsRideService(t)

	cust, err := customers.FindOrCreateByLineUserID("U_stops_ok", "包車乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}

	created, err := svc.CreateByCustomer(context.Background(), cust.ID, CustomerCreateRequest{
		Stops: fourStopInputs(),
	})
	if err != nil {
		t.Fatalf("多停靠點建單失敗：%v", err)
	}

	t.Run("rides 仍寫第一個上車點與最終目的地", func(t *testing.T) {
		got, err := rides.GetByID(created.ID)
		if err != nil {
			t.Fatalf("重讀訂單失敗：%v", err)
		}
		// 第一個 pickup（seq=1，台北車站）→ 派單據此找最近司機。
		if math.Abs(got.PickupPoint.Lat-25.0478) > 1e-6 || got.PickupAddress != "台北車站" {
			t.Fatalf("pickup 應為第一個上車點：%+v / %q", got.PickupPoint, got.PickupAddress)
		}
		// 最終 dropoff（seq 最大＝4，台北 101）→ 導航、地圖、F3 路線退路讀這裡。
		if got.DropoffPoint == nil {
			t.Fatal("dropoff_point 不該為 nil")
		}
		if math.Abs(got.DropoffPoint.Lat-25.0339) > 1e-6 || got.DropoffAddress != "台北101" {
			t.Fatalf("dropoff 應為最終目的地：%+v / %q", got.DropoffPoint, got.DropoffAddress)
		}
	})

	t.Run("stops 依序落庫且座標正確", func(t *testing.T) {
		list, err := stopsRepo.ListByRide(created.ID)
		if err != nil {
			t.Fatalf("讀停靠點失敗：%v", err)
		}
		if len(list) != 4 {
			t.Fatalf("預期 4 個停靠點，得到 %d", len(list))
		}
		for i, want := range fourStopInputs() {
			got := list[i]
			if got.Seq != want.Seq || got.Kind != want.Kind || got.PassengerLabel != want.PassengerLabel {
				t.Fatalf("第 %d 站不符：%+v", i, got)
			}
			// 座標經 ST_MakePoint(lng, lat) 落庫再讀回，經緯度不可對調。
			if math.Abs(got.Point.Lat-want.Lat) > 1e-6 || math.Abs(got.Point.Lng-want.Lng) > 1e-6 {
				t.Fatalf("第 %d 站座標錯誤（經緯度可能對調）：%+v，預期 lat=%f lng=%f",
					i, got.Point, want.Lat, want.Lng)
			}
			if got.Address != want.Address {
				t.Fatalf("第 %d 站地址錯誤：%q", i, got.Address)
			}
			// 剛建單，尚未到達也未跳過。
			if !got.Pending() {
				t.Fatalf("新建的停靠點應為未處理：arrived=%v skipped=%v", got.ArrivedAt, got.SkippedAt)
			}
		}
	})
}

// TestCreateByCustomer_客戶端未依序送 落庫一律以 seq 為準。
func TestCreateByCustomer_停靠點未依序送(t *testing.T) {
	svc, _, stopsRepo, customers := newStopsRideService(t)

	cust, err := customers.FindOrCreateByLineUserID("U_stops_unsorted", "乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	s := fourStopInputs()
	s[0], s[3] = s[3], s[0] // 打亂

	created, err := svc.CreateByCustomer(context.Background(), cust.ID, CustomerCreateRequest{Stops: s})
	if err != nil {
		t.Fatalf("建單失敗：%v", err)
	}
	list, err := stopsRepo.ListByRide(created.ID)
	if err != nil {
		t.Fatalf("讀停靠點失敗：%v", err)
	}
	for i, want := range []int{1, 2, 3, 4} {
		if list[i].Seq != want {
			t.Fatalf("落庫後應依 seq 排序，得到 %+v", list)
		}
	}
}

// TestCreateByCustomer_單點訂單不建stops N1／N3 的相容性保證：
// 既有 App／LINE 建單一律不產生任何 ride_stops 列。
func TestCreateByCustomer_單點訂單不建stops(t *testing.T) {
	svc, _, stopsRepo, customers := newStopsRideService(t)

	cust, err := customers.FindOrCreateByLineUserID("U_stops_single", "一般乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	dropLat, dropLng := 25.08, 121.57
	created, err := svc.CreateByCustomer(context.Background(), cust.ID, CustomerCreateRequest{
		PickupLat: 25.03, PickupLng: 121.56, PickupAddress: "台北車站",
		DropoffAddress: "松山機場", DropoffLat: &dropLat, DropoffLng: &dropLng,
	})
	if err != nil {
		t.Fatalf("單點建單不該受 N3 影響：%v", err)
	}
	list, err := stopsRepo.ListByRide(created.ID)
	if err != nil {
		t.Fatalf("讀停靠點失敗：%v", err)
	}
	if len(list) != 0 {
		t.Fatalf("單點訂單不該有 ride_stops 列，得到 %d 筆", len(list))
	}
}

// TestCreateByCustomer_停靠點驗證失敗不建單 驗證失敗必須在建 ride 之前擋下，
// 否則會留下一筆「只有第一個上車點、少載其餘乘客」的孤兒訂單。
func TestCreateByCustomer_停靠點驗證失敗不建單(t *testing.T) {
	svc, rides, _, customers := newStopsRideService(t)

	cust, err := customers.FindOrCreateByLineUserID("U_stops_bad", "亂填乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	_, err = svc.CreateByCustomer(context.Background(), cust.ID, CustomerCreateRequest{
		Stops: []StopInput{
			{Seq: 1, Kind: constants.StopKindDropoff, Lat: 25.03, Lng: 121.56, PassengerLabel: "A"},
			{Seq: 2, Kind: constants.StopKindPickup, Lat: 25.04, Lng: 121.57, PassengerLabel: "A"},
		},
	})
	if !errors.Is(err, ErrDropoffBeforePickup) {
		t.Fatalf("預期 ErrDropoffBeforePickup，得到 %v", err)
	}
	// 不該留下任何訂單。
	active, err := rides.FindActiveByCustomer(cust.ID)
	if err != nil {
		t.Fatalf("查詢失敗：%v", err)
	}
	if active != nil {
		t.Fatalf("驗證失敗不該建出訂單，卻有 ride #%d", active.ID)
	}
}

// TestCreateByCustomer_未注入stops時明確失敗 設定沒接好時不可默默當單點訂單建出去
// （那會變成「少載四個人」的行程）。
func TestCreateByCustomer_未注入stops時明確失敗(t *testing.T) {
	db := newServiceTestDB(t)
	customers := repository.NewCustomerRepository(db)
	rides := repository.NewRideRepository(db)
	redis := newServiceTestRedis(t)
	dispatch := NewDispatchService(
		repository.NewDriverRepository(db), rides, customers, redis,
		lineclient.NewClient(""), nil, NewDispatchSettings(3000, 5, 20, 1, 5), nil,
	)
	svc := NewRideService(customers, rides, redis, dispatch) // 刻意不 SetStops

	cust, err := customers.FindOrCreateByLineUserID("U_stops_noinject", "乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	if _, err := svc.CreateByCustomer(context.Background(), cust.ID, CustomerCreateRequest{
		Stops: fourStopInputs(),
	}); !errors.Is(err, ErrStopsUnavailable) {
		t.Fatalf("預期 ErrStopsUnavailable，得到 %v", err)
	}
}
