package service

import (
	"context"
	"errors"
	"testing"

	"line-fleet-dispatch/internal/constants"
	lineclient "line-fleet-dispatch/internal/line"
	"line-fleet-dispatch/internal/repository"
)

func TestSetDriverEnabled_停用與啟用(t *testing.T) {
	db := newServiceTestDB(t)
	drivers := repository.NewDriverRepository(db)
	rides := repository.NewRideRepository(db)
	customers := repository.NewCustomerRepository(db)
	redisStore := newServiceTestRedis(t)
	settings := NewDispatchSettings(3000, 5, 20, 1, 5)
	dispatch := NewDispatchService(drivers, rides, customers, redisStore, lineclient.NewClient(""), nil, settings, nil)
	ops := NewAdminOperations(drivers, dispatch, redisStore, settings)

	d, err := drivers.FindOrCreate("U_admin_disable", "待停用司機")
	if err != nil {
		t.Fatalf("建立司機失敗：%v", err)
	}
	ctx := context.Background()
	if err := redisStore.UpdateDriverLocation(ctx, d.ID, 25.03, 121.56); err != nil {
		t.Fatalf("寫入 GEO 失敗：%v", err)
	}

	got, err := ops.SetDriverEnabled(ctx, d.ID, false)
	if err != nil {
		t.Fatalf("停用失敗：%v", err)
	}
	if got.Status != constants.DriverStatusDisabled {
		t.Fatalf("預期 Disabled，得到 %d", got.Status)
	}

	reg := NewDriverRegistry(drivers)
	if _, err := reg.GoOnline(d.ID); !errors.Is(err, ErrDriverDisabled) {
		t.Fatalf("停用後上線預期 ErrDriverDisabled，得到 %v", err)
	}

	got, err = ops.SetDriverEnabled(ctx, d.ID, true)
	if err != nil {
		t.Fatalf("啟用失敗：%v", err)
	}
	if got.Status != constants.DriverStatusOffline {
		t.Fatalf("啟用後預期 Offline，得到 %d", got.Status)
	}

	got, err = reg.GoOnline(d.ID)
	if err != nil {
		t.Fatalf("啟用後上線失敗：%v", err)
	}
	if got.Status != constants.DriverStatusIdle {
		t.Fatalf("上線後預期 Idle，得到 %d", got.Status)
	}
}

func TestSetDriverEnabled_載客中不可停用(t *testing.T) {
	db := newServiceTestDB(t)
	drivers := repository.NewDriverRepository(db)
	ops := NewAdminOperations(drivers, nil, nil, nil)

	d, err := drivers.FindOrCreate("U_admin_ontrip", "載客司機")
	if err != nil {
		t.Fatalf("建立司機失敗：%v", err)
	}
	if err := drivers.UpdateStatus(d.ID, constants.DriverStatusOnTrip); err != nil {
		t.Fatalf("設狀態失敗：%v", err)
	}

	_, err = ops.SetDriverEnabled(context.Background(), d.ID, false)
	if !errors.Is(err, ErrDriverOnTrip) {
		t.Fatalf("載客中停用預期 ErrDriverOnTrip，得到 %v", err)
	}
}

// TestReviewDriverVehicle O5：admin 車輛審核 approve/reject/退回需原因/僅待審核可審。
func TestReviewDriverVehicle(t *testing.T) {
	db := newServiceTestDB(t)
	drivers := repository.NewDriverRepository(db)
	rides := repository.NewRideRepository(db)
	customers := repository.NewCustomerRepository(db)
	redisStore := newServiceTestRedis(t)
	settings := NewDispatchSettings(3000, 5, 20, 1, 5)
	dispatch := NewDispatchService(drivers, rides, customers, redisStore, lineclient.NewClient(""), nil, settings, nil)
	ops := NewAdminOperations(drivers, dispatch, redisStore, settings)
	reg := NewDriverRegistry(drivers)

	newPending := func(t *testing.T, lineID, plate string) int64 {
		t.Helper()
		d, err := drivers.FindOrCreate(lineID, "審核測試司機")
		if err != nil {
			t.Fatalf("建立司機失敗：%v", err)
		}
		if _, err := reg.SetVehicle(d.ID, constants.VehicleTypeSedan, plate); err != nil {
			t.Fatalf("填車輛失敗：%v", err)
		}
		return d.ID
	}

	t.Run("核准後可接單", func(t *testing.T) {
		id := newPending(t, "U_rev_ok", "REV-0001")
		got, err := ops.ReviewDriverVehicle(id, true, "")
		if err != nil {
			t.Fatalf("核准失敗：%v", err)
		}
		if got.VehicleReviewStatus != constants.VehicleReviewApproved || !got.VehicleApproved() {
			t.Fatalf("核准後應 approved，得到 %q", got.VehicleReviewStatus)
		}
	})

	t.Run("退回附原因，狀態為 rejected", func(t *testing.T) {
		id := newPending(t, "U_rev_rej", "REV-0002")
		got, err := ops.ReviewDriverVehicle(id, false, "車牌照片模糊")
		if err != nil {
			t.Fatalf("退回失敗：%v", err)
		}
		if got.VehicleReviewStatus != constants.VehicleReviewRejected {
			t.Fatalf("退回後應 rejected，得到 %q", got.VehicleReviewStatus)
		}
		if got.VehicleReviewNote != "車牌照片模糊" {
			t.Fatalf("退回原因應保留，得到 %q", got.VehicleReviewNote)
		}
		if got.VehicleApproved() {
			t.Fatal("退回的司機不該能接單")
		}
	})

	t.Run("退回未附原因回 ErrRejectNoteRequired", func(t *testing.T) {
		id := newPending(t, "U_rev_noreason", "REV-0003")
		if _, err := ops.ReviewDriverVehicle(id, false, ""); !errors.Is(err, ErrRejectNoteRequired) {
			t.Fatalf("預期 ErrRejectNoteRequired，得到 %v", err)
		}
	})

	t.Run("非待審核狀態不可審（已核准者重審回 ErrVehicleNotPending）", func(t *testing.T) {
		id := newPending(t, "U_rev_twice", "REV-0004")
		if _, err := ops.ReviewDriverVehicle(id, true, ""); err != nil {
			t.Fatalf("首次核准失敗：%v", err)
		}
		if _, err := ops.ReviewDriverVehicle(id, true, ""); !errors.Is(err, ErrVehicleNotPending) {
			t.Fatalf("已核准者重審預期 ErrVehicleNotPending，得到 %v", err)
		}
	})

	t.Run("改車輛回 pending 後可再審", func(t *testing.T) {
		id := newPending(t, "U_rev_rechange", "REV-0005")
		if _, err := ops.ReviewDriverVehicle(id, true, ""); err != nil {
			t.Fatalf("核准失敗：%v", err)
		}
		// 改車輛 → 回 pending（O5：任何改動都要重審）。
		if _, err := reg.SetVehicle(id, constants.VehicleTypeSUV, "REV-0006"); err != nil {
			t.Fatalf("改車輛失敗：%v", err)
		}
		d, err := drivers.FindByID(id)
		if err != nil {
			t.Fatalf("重讀失敗：%v", err)
		}
		if d.VehicleReviewStatus != constants.VehicleReviewPending {
			t.Fatalf("改車輛後應回 pending，得到 %q", d.VehicleReviewStatus)
		}
		if _, err := ops.ReviewDriverVehicle(id, true, ""); err != nil {
			t.Fatalf("重審核准失敗：%v", err)
		}
	})
}
