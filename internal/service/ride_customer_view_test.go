package service

import (
	"errors"
	"testing"
	"time"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/model"
	"line-fleet-dispatch/internal/repository"
)

// TestRideAcceptedCustomerPayload O4：乘客要能在路邊對車，並直接聯絡司機（O7 明碼）。
func TestRideAcceptedCustomerPayload(t *testing.T) {
	t.Run("帶車種車牌與電話", func(t *testing.T) {
		d := &model.Driver{
			Name: "王司機", Phone: "0912345678",
			VehicleType: constants.VehicleTypePet, PlateNumber: "ABC-1234",
		}
		p := rideAcceptedCustomerPayload(d, 300)
		if p["driver_name"] != "王司機" || p["eta_sec"] != 300 {
			t.Fatalf("既有欄位不該變：%v", p)
		}
		// 送 code 而非顯示名：前端有自己的車種對照（O1 原則）。
		if p["driver_vehicle_type"] != constants.VehicleTypePet {
			t.Fatalf("應帶車種 code：%v", p)
		}
		if p["driver_plate_number"] != "ABC-1234" {
			t.Fatalf("應帶車牌：%v", p)
		}
		if p["driver_phone"] != "0912345678" {
			t.Fatalf("應帶司機電話（明碼，僅該趟乘客收得到）：%v", p)
		}
	})

	t.Run("欄位為空時不帶該鍵", func(t *testing.T) {
		// 寧可少一個鍵，也不要讓 App 顯示空白車牌／空白電話。
		p := rideAcceptedCustomerPayload(&model.Driver{Name: "無車資料"}, 300)
		for _, k := range []string{"driver_vehicle_type", "driver_plate_number", "driver_phone"} {
			if _, ok := p[k]; ok {
				t.Fatalf("空值不該帶 %q：%v", k, p)
			}
		}
	})
}

// customerViewFixture 一趟已接單的行程，司機開寵物車、有電話。
func customerViewFixture(t *testing.T, prefix string) (
	*RideQueryService, *repository.DriverRepository, *repository.RideRepository, int64, int64, int64,
) {
	t.Helper()
	db := newServiceTestDB(t)
	drivers := repository.NewDriverRepository(db)
	rides := repository.NewRideRepository(db)
	tracks := repository.NewTrackRepository(db)
	customers := repository.NewCustomerRepository(db)

	cust, err := customers.FindOrCreateByLineUserID(prefix+"_cust", "乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	driver, err := drivers.FindOrCreate(prefix+"_drv", "王司機")
	if err != nil {
		t.Fatalf("建立司機失敗：%v", err)
	}
	if err := drivers.UpdateVehicle(driver.ID, constants.VehicleTypePet, prefix+"-PET"); err != nil {
		t.Fatalf("設定車輛失敗：%v", err)
	}
	if err := db.Model(&model.Driver{}).Where("id = ?", driver.ID).
		Update("phone", "0912345678").Error; err != nil {
		t.Fatalf("設定電話失敗：%v", err)
	}

	now := time.Now()
	ride := &model.Ride{
		CustomerID: cust.ID, Status: constants.RideStatusRequested,
		PickupPoint: model.GeoPoint{Lat: 25.03, Lng: 121.56}, PickupAddress: "台北車站",
		RequestedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := rides.Create(ride); err != nil {
		t.Fatalf("建立訂單失敗：%v", err)
	}
	if err := rides.AcceptRide(ride.ID, driver.ID, 300); err != nil {
		t.Fatalf("接單失敗：%v", err)
	}

	svc := NewRideQueryService(tracks, rides)
	svc.SetDrivers(drivers)
	return svc, drivers, rides, cust.ID, driver.ID, ride.ID
}

// TestAcceptRide車輛快照定格 O7：接單時定格；司機事後換車，歷史行程不得跟著變。
func TestAcceptRide車輛快照定格(t *testing.T) {
	_, drivers, rides, _, driverID, rideID := customerViewFixture(t, "U_snap")

	got, err := rides.GetByID(rideID)
	if err != nil {
		t.Fatalf("重讀訂單失敗：%v", err)
	}
	if got.DriverVehicleType != constants.VehicleTypePet || got.DriverPlateNumber != "U_snap-PET" {
		t.Fatalf("接單時應定格車輛快照，得到 %q / %q", got.DriverVehicleType, got.DriverPlateNumber)
	}

	// 司機換車 → 歷史行程的快照不得改變（這正是不用 JOIN drivers 的理由）。
	if err := drivers.UpdateVehicle(driverID, constants.VehicleTypeSedan, "NEW-9999"); err != nil {
		t.Fatalf("換車失敗：%v", err)
	}
	after, err := rides.GetByID(rideID)
	if err != nil {
		t.Fatalf("重讀訂單失敗：%v", err)
	}
	if after.DriverVehicleType != constants.VehicleTypePet || after.DriverPlateNumber != "U_snap-PET" {
		t.Fatalf("司機換車後歷史快照不該變，得到 %q / %q", after.DriverVehicleType, after.DriverPlateNumber)
	}
}

// TestGetActiveRideByCustomer帶司機資訊 O4：乘客在行程中看得到車種車牌與電話。
func TestGetActiveRideByCustomer帶司機資訊(t *testing.T) {
	svc, _, _, custID, _, _ := customerViewFixture(t, "U_active_view")

	view, err := svc.GetActiveRideByCustomer(custID)
	if err != nil {
		t.Fatalf("查詢失敗：%v", err)
	}
	if view == nil {
		t.Fatal("應有進行中訂單")
	}
	if view.DriverName != "王司機" || view.DriverPhone != "0912345678" {
		t.Fatalf("應帶司機姓名與電話，得到 %q / %q", view.DriverName, view.DriverPhone)
	}
	// 車種車牌來自 ride 快照（內嵌 *model.Ride 攤平）。
	if view.DriverVehicleType != constants.VehicleTypePet || view.DriverPlateNumber != "U_active_view-PET" {
		t.Fatalf("應帶車輛快照，得到 %q / %q", view.DriverVehicleType, view.DriverPlateNumber)
	}
	// 既有欄位不得因為換成 view 而消失（App 仍在讀）。
	if view.Ride == nil || view.PickupAddress != "台北車站" {
		t.Fatalf("既有 ride 欄位應原樣保留：%+v", view.Ride)
	}
}

// TestGetRideForCustomer電話僅本人可見 O7 的授權邊界：明碼電話絕不可外流給別人。
func TestGetRideForCustomer電話僅本人可見(t *testing.T) {
	svc, _, _, custID, _, rideID := customerViewFixture(t, "U_phone_auth")

	t.Run("本人查得到電話", func(t *testing.T) {
		view, err := svc.GetRideForCustomer(custID, rideID)
		if err != nil {
			t.Fatalf("本人查詢應成功：%v", err)
		}
		if view.DriverPhone != "0912345678" {
			t.Fatalf("本人應查得到司機電話，得到 %q", view.DriverPhone)
		}
	})

	t.Run("別人一律403且拿不到任何司機資訊", func(t *testing.T) {
		const otherCustomerID = int64(999999)
		view, err := svc.GetRideForCustomer(otherCustomerID, rideID)
		if !errors.Is(err, ErrForbidden) {
			t.Fatalf("非本趟乘客預期 ErrForbidden，得到 %v", err)
		}
		if view != nil {
			t.Fatalf("被拒時不該回傳任何資料（含司機電話）：%+v", view)
		}
	})
}
