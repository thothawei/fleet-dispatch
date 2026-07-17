package service

import (
	"context"
	"strconv"
	"testing"
	"time"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/events"
	lineclient "line-fleet-dispatch/internal/line"
	"line-fleet-dispatch/internal/model"
	"line-fleet-dispatch/internal/repository"
)

// cents 供失敗訊息顯示金額；直接 %v 印 *int64 會印出指標位址，對除錯毫無幫助。
func cents(p *int64) string {
	if p == nil {
		return "nil"
	}
	return strconv.FormatInt(*p, 10)
}

// completeFixture 建一趟「已上車」的行程，司機一律開寵物車——
// 用來分辨清潔費是看乘客指定的車種，還是看司機的車。
func completeFixture(t *testing.T, prefix, requiredVehicleType string, cleaningBps int) (
	*TrackingService, *repository.RideRepository, *fakePublisher, int64, int64,
) {
	t.Helper()
	db := newServiceTestDB(t)
	redis := newServiceTestRedis(t)
	drivers := repository.NewDriverRepository(db)
	rides := repository.NewRideRepository(db)
	tracks := repository.NewTrackRepository(db)
	customers := repository.NewCustomerRepository(db)
	fp := &fakePublisher{}

	cust, err := customers.FindOrCreateByLineUserID(prefix+"_cust", "乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	driver, err := drivers.FindOrCreate(prefix+"_drv", "寵物車司機")
	if err != nil {
		t.Fatalf("建立司機失敗：%v", err)
	}
	// 司機開的是寵物車——但這**不該**影響計費。
	if err := drivers.UpdateVehicle(driver.ID, constants.VehicleTypePet, prefix+"-PET"); err != nil {
		t.Fatalf("設定車輛失敗：%v", err)
	}

	now := time.Now()
	ride := &model.Ride{
		CustomerID:          cust.ID,
		Status:              constants.RideStatusRequested,
		PickupPoint:         model.GeoPoint{Lat: 25.03, Lng: 121.56},
		PickupAddress:       "台北車站",
		RequiredVehicleType: requiredVehicleType,
		RequestedAt:         now,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	if err := rides.Create(ride); err != nil {
		t.Fatalf("建立訂單失敗：%v", err)
	}
	if err := rides.AcceptRide(ride.ID, driver.ID, 300); err != nil {
		t.Fatalf("接單失敗：%v", err)
	}
	if err := rides.UpdateStatus(ride.ID, constants.RideStatusPickedUp); err != nil {
		t.Fatalf("上車失敗：%v", err)
	}

	svc := NewTrackingService(drivers, rides, tracks, redis, lineclient.NewClient(""), nil, 0, 0, fp)
	// 起步 85 元、每公里 20 元、最低 85 元、手續費 15%。
	svc.SetFeeSettings(newFeeSettingsWithCleaning(8500, 2000, 8500, 1500, cleaningBps))
	return svc, rides, fp, ride.ID, driver.ID
}

// TestComplete_指定寵物車落帳清潔費 O6 端到端：完成時定格寫入 cleaning_fee_cents，
// 且 driver_net 含清潔費、commission 不含。
func TestComplete_指定寵物車落帳清潔費(t *testing.T) {
	svc, rides, fp, rideID, driverID := completeFixture(t, "U_clean_pet", constants.VehicleTypePet, 2000)

	if err := svc.Complete(context.Background(), rideID, driverID); err != nil {
		t.Fatalf("完成行程失敗：%v", err)
	}

	got, err := rides.GetByID(rideID)
	if err != nil {
		t.Fatalf("重讀訂單失敗：%v", err)
	}
	// 無軌跡、無 OSRM → 距離 0 → fare 落到 min_fare 8500。
	if got.FareAmountCents == nil || *got.FareAmountCents != 8500 {
		t.Fatalf("車資應為 8500，得到 %s", cents(got.FareAmountCents))
	}
	// 清潔費 = floorNtd(8500 * 20%) = 1700
	if got.CleaningFeeCents == nil || *got.CleaningFeeCents != 1700 {
		t.Fatalf("清潔費應定格為 1700，得到 %s", cents(got.CleaningFeeCents))
	}
	// 手續費基準只有車資：floorNtd(8500*15%=1275) = 1200
	if got.CommissionAmountCents == nil || *got.CommissionAmountCents != 1200 {
		t.Fatalf("手續費應為 1200（基準不含清潔費），得到 %s", cents(got.CommissionAmountCents))
	}
	// 實得 = 8500 − 1200 + 1700 = 9000
	if got.DriverNetAmountCents == nil || *got.DriverNetAmountCents != 9000 {
		t.Fatalf("司機實得應為 9000（含清潔費），得到 %s", cents(got.DriverNetAmountCents))
	}

	// 乘客端完成卡要能分項顯示「車資 + 清潔費」，不能只給總額。
	var completed *events.Event
	for i := range fp.recv {
		if fp.recv[i].Ev.Type == events.TypeRideCompleted && fp.recv[i].Rec.Role == events.RoleCustomer {
			ev := fp.recv[i].Ev
			completed = &ev
			break
		}
	}
	if completed == nil {
		t.Fatal("應發出 ride.completed 給乘客")
	}
	if completed.Payload["fare_amount_cents"] != int64(8500) {
		t.Fatalf("payload 車資錯誤：%v", completed.Payload)
	}
	if completed.Payload["cleaning_fee_cents"] != int64(1700) {
		t.Fatalf("payload 應帶清潔費分項：%v", completed.Payload)
	}
}

// TestComplete_寵物車司機載到未指定的乘客不加收 O6 最關鍵的語意：
// 加收依 rides.required_vehicle_type，**不是** drivers.vehicle_type。
// 這位乘客沒指定寵物車（未指定＝不過濾車種），只是剛好被派到寵物車司機——不得加收。
func TestComplete_寵物車司機載到未指定的乘客不加收(t *testing.T) {
	svc, rides, fp, rideID, driverID := completeFixture(t, "U_clean_nonpet", "", 2000)

	if err := svc.Complete(context.Background(), rideID, driverID); err != nil {
		t.Fatalf("完成行程失敗：%v", err)
	}

	got, err := rides.GetByID(rideID)
	if err != nil {
		t.Fatalf("重讀訂單失敗：%v", err)
	}
	if got.CleaningFeeCents == nil || *got.CleaningFeeCents != 0 {
		t.Fatalf("乘客沒指定寵物車就不該加收清潔費（司機開什麼車無關），得到 %s", cents(got.CleaningFeeCents))
	}
	// 實得回到 fare − commission = 8500 − 1200
	if got.DriverNetAmountCents == nil || *got.DriverNetAmountCents != 7300 {
		t.Fatalf("無清潔費時實得應為 7300，得到 %s", cents(got.DriverNetAmountCents))
	}

	// 沒加收就不該帶這個鍵——帶 0 會讓 App 顯示一列「清潔費 NT$0」。
	for i := range fp.recv {
		if fp.recv[i].Ev.Type == events.TypeRideCompleted && fp.recv[i].Rec.Role == events.RoleCustomer {
			if _, ok := fp.recv[i].Ev.Payload["cleaning_fee_cents"]; ok {
				t.Fatalf("未加收時不該帶 cleaning_fee_cents：%v", fp.recv[i].Ev.Payload)
			}
		}
	}
}
