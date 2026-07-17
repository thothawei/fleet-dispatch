package service

import (
	"context"
	"testing"
	"time"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/events"
	lineclient "line-fleet-dispatch/internal/line"
	"line-fleet-dispatch/internal/model"
	"line-fleet-dispatch/internal/repository"
)

// TestGiveUpCancelInfo P4 的文案與 payload。純單元——不必起容器。
func TestGiveUpCancelInfo(t *testing.T) {
	t.Run("未指定車種維持泛用文案", func(t *testing.T) {
		reason, text, payload := giveUpCancelInfo(&model.Ride{})
		if reason != constants.CancelReasonNoDriver {
			t.Fatalf("預期 %s，得到 %s", constants.CancelReasonNoDriver, reason)
		}
		if text != "抱歉，附近暫無可用司機，請稍後再試" {
			t.Fatalf("文案錯誤：%q", text)
		}
		if payload["cancel_reason"] != constants.CancelReasonNoDriver {
			t.Fatalf("payload cancel_reason 錯誤：%v", payload)
		}
		// 沒指定車種就不該出現這個鍵，否則 App 會以為乘客指定過。
		if _, ok := payload["required_vehicle_type"]; ok {
			t.Fatalf("未指定車種時不該帶 required_vehicle_type：%v", payload)
		}
	})

	t.Run("指定車種時原因與文案都要具體", func(t *testing.T) {
		reason, text, payload := giveUpCancelInfo(&model.Ride{RequiredVehicleType: constants.VehicleTypePet})
		if reason != constants.CancelReasonNoVehicleOfType {
			t.Fatalf("預期 %s，得到 %s", constants.CancelReasonNoVehicleOfType, reason)
		}
		if text != "抱歉，附近暫無寵物用車，請稍後再試" {
			t.Fatalf("文案應指出是車種問題，得到 %q", text)
		}
		if payload["cancel_reason"] != constants.CancelReasonNoVehicleOfType {
			t.Fatalf("payload cancel_reason 錯誤：%v", payload)
		}
		// App 端靠這兩個機器可讀欄位判斷，不 parse 文案。
		if payload["required_vehicle_type"] != constants.VehicleTypePet {
			t.Fatalf("payload 應帶 required_vehicle_type=pet：%v", payload)
		}
	})
}

// newRequiredVehicleFixture 一台寵物車、一台轎車，都在上車點附近待命且已填車輛資訊。
func newRequiredVehicleFixture(t *testing.T, prefix string, offerTimeoutSec int) (
	*DispatchService, *fakePublisher, *repository.RideRepository, *repository.CustomerRepository, map[string]int64,
) {
	t.Helper()
	db := newServiceTestDB(t)
	redisStore := newServiceTestRedis(t)
	drivers := repository.NewDriverRepository(db)
	rides := repository.NewRideRepository(db)
	customers := repository.NewCustomerRepository(db)
	fp := &fakePublisher{}
	dispatch := NewDispatchService(drivers, rides, customers, redisStore,
		lineclient.NewClient(""), NewETAService(nil),
		NewDispatchSettings(3000, 5, offerTimeoutSec, 1, 5), fp)

	ids := map[string]int64{}
	ctx := context.Background()
	for _, spec := range []struct{ key, vType, plate string }{
		{constants.VehicleTypePet, constants.VehicleTypePet, prefix + "-PET"},
		{constants.VehicleTypeSedan, constants.VehicleTypeSedan, prefix + "-SED"},
	} {
		d, err := drivers.FindOrCreate(prefix+"_"+spec.key, spec.key+"司機")
		if err != nil {
			t.Fatalf("建立司機失敗：%v", err)
		}
		if err := drivers.UpdateVehicle(d.ID, spec.vType, spec.plate); err != nil {
			t.Fatalf("設定車輛失敗：%v", err)
		}
		if err := redisStore.UpdateDriverLocation(ctx, d.ID, 25.031, 121.561); err != nil {
			t.Fatalf("寫入司機位置失敗：%v", err)
		}
		ids[spec.key] = d.ID
	}
	return dispatch, fp, rides, customers, ids
}

func newRideWithVehicleType(t *testing.T, rides *repository.RideRepository, customerID int64, vType string) *model.Ride {
	t.Helper()
	now := time.Now()
	ride := &model.Ride{
		CustomerID:          customerID,
		Status:              constants.RideStatusRequested,
		PickupPoint:         model.GeoPoint{Lat: 25.03, Lng: 121.56},
		PickupAddress:       "台北車站",
		RequiredVehicleType: vType,
		RequestedAt:         now,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	if err := rides.Create(ride); err != nil {
		t.Fatalf("建立訂單失敗：%v", err)
	}
	return ride
}

// TestDispatch_指定車種只派同車種 P3：指定寵物車時，轎車司機不得收到派單。
func TestDispatch_指定車種只派同車種(t *testing.T) {
	dispatch, fp, rides, customers, ids := newRequiredVehicleFixture(t, "U_p3_match", 20)

	cust, err := customers.FindOrCreateByLineUserID("U_p3_match_cust", "寵物乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	ride := newRideWithVehicleType(t, rides, cust.ID, constants.VehicleTypePet)

	if err := dispatch.Dispatch(context.Background(), ride.ID); err != nil {
		t.Fatalf("派單失敗：%v", err)
	}

	var offeredTo []int64
	for _, r := range fp.recv {
		if r.Ev.Type == events.TypeRideAssigned && r.Rec.Role == events.RoleDriver {
			offeredTo = append(offeredTo, r.Rec.ID)
		}
	}
	if len(offeredTo) != 1 || offeredTo[0] != ids[constants.VehicleTypePet] {
		t.Fatalf("指定寵物車時只該派給寵物車司機 %d，實際派給 %v", ids[constants.VehicleTypePet], offeredTo)
	}
}

// TestDispatch_未指定車種時所有車種都可派 P3 不可讓既有（不指定）的單變窄。
func TestDispatch_未指定車種時所有車種都可派(t *testing.T) {
	dispatch, fp, rides, customers, _ := newRequiredVehicleFixture(t, "U_p3_any", 20)

	cust, err := customers.FindOrCreateByLineUserID("U_p3_any_cust", "一般乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	ride := newRideWithVehicleType(t, rides, cust.ID, "")

	if err := dispatch.Dispatch(context.Background(), ride.ID); err != nil {
		t.Fatalf("派單失敗：%v", err)
	}

	count := 0
	for _, r := range fp.recv {
		if r.Ev.Type == events.TypeRideAssigned && r.Rec.Role == events.RoleDriver {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("未指定車種時兩台都該收到派單，實際 %d 台", count)
	}
}

// TestDispatch_找不到指定車種時取消並帶原因 P3+P4 端到端：
// 沒有無障礙車 → 不降級（轎車／寵物車都不得收到派單）→ 逾時自動取消 →
// ride.cancelled 帶 cancel_reason=no_vehicle_of_type。
func TestDispatch_找不到指定車種時取消並帶原因(t *testing.T) {
	// offerTimeout 設 1 秒，讓 giveUpIfUnaccepted 在測試時間內觸發。
	dispatch, fp, rides, customers, _ := newRequiredVehicleFixture(t, "U_p3_none", 1)

	cust, err := customers.FindOrCreateByLineUserID("U_p3_none_cust", "無障礙乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	ride := newRideWithVehicleType(t, rides, cust.ID, constants.VehicleTypeAccessible)

	if err := dispatch.Dispatch(context.Background(), ride.ID); err != nil {
		t.Fatalf("派單失敗：%v", err)
	}

	// 不降級：一台都不該收到派單。
	for _, r := range fp.recv {
		if r.Ev.Type == events.TypeRideAssigned {
			t.Fatalf("沒有無障礙車時不得改派其他車種，卻派給了 %+v", r.Rec)
		}
	}

	// 等 giveUpIfUnaccepted（AfterFunc offerTimeout=1s）觸發。
	deadline := time.Now().Add(10 * time.Second)
	var cancelled *events.Event
	for time.Now().Before(deadline) {
		fp.mu.Lock()
		for i := range fp.recv {
			if fp.recv[i].Ev.Type == events.TypeRideCancelled {
				ev := fp.recv[i].Ev
				cancelled = &ev
				break
			}
		}
		fp.mu.Unlock()
		if cancelled != nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if cancelled == nil {
		t.Fatal("逾時無人接單應發出 ride.cancelled")
	}
	if cancelled.Payload["cancel_reason"] != constants.CancelReasonNoVehicleOfType {
		t.Fatalf("cancel_reason 應為 no_vehicle_of_type，得到 %v", cancelled.Payload)
	}
	if cancelled.Payload["required_vehicle_type"] != constants.VehicleTypeAccessible {
		t.Fatalf("payload 應帶 required_vehicle_type=accessible，得到 %v", cancelled.Payload)
	}

	got, err := rides.GetByID(ride.ID)
	if err != nil {
		t.Fatalf("重讀訂單失敗：%v", err)
	}
	if got.Status != constants.RideStatusCancelled {
		t.Fatalf("訂單應被自動取消，狀態為 %d", got.Status)
	}
}
