package service

import (
	"errors"
	"testing"
	"time"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/model"
	"line-fleet-dispatch/internal/repository"
)

// stopsFixture 一趟已接單的兩人四停行程；**同一個 DB 裡**另有一位司機與他自己的一趟行程，
// 用來測授權邊界。
//
// 「同一個 DB」是重點：newServiceTestDB 每次呼叫都起一個新容器，兩個 fixture 的
// 自增 id 會各自從 1 開始而撞在一起——那樣「拿別趟的 stop_id」根本測不到跨行程，
// 反而會撞到自己那趟的同號 stop 而合法通過。
type stopsFixture struct {
	svc       *RideStopService
	stopsRepo *repository.RideStopRepository
	rides     *repository.RideRepository
	rideID    int64
	driverID  int64
	otherID   int64
	stopIDs   []int64
	// otherRideID／otherStopIDs 同一個 DB 裡、屬於 otherID 司機的另一趟行程。
	otherRideID  int64
	otherStopIDs []int64
}

func newStopsFixture(t *testing.T, prefix string) *stopsFixture {
	t.Helper()
	db := newServiceTestDB(t)
	drivers := repository.NewDriverRepository(db)
	rides := repository.NewRideRepository(db)
	stopsRepo := repository.NewRideStopRepository(db)
	customers := repository.NewCustomerRepository(db)

	cust, err := customers.FindOrCreateByLineUserID(prefix+"_cust", "乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	driver, err := drivers.FindOrCreate(prefix+"_drv", "司機")
	if err != nil {
		t.Fatalf("建立司機失敗：%v", err)
	}
	other, err := drivers.FindOrCreate(prefix+"_other", "別的司機")
	if err != nil {
		t.Fatalf("建立司機失敗：%v", err)
	}

	// newRide 在同一個 DB 建一趟兩人四停、已被指定司機接單的行程。
	newRide := func(driverID int64) (int64, []int64) {
		t.Helper()
		now := time.Now()
		ride := &model.Ride{
			CustomerID: cust.ID, Status: constants.RideStatusRequested,
			PickupPoint: model.GeoPoint{Lat: 25.03, Lng: 121.56}, PickupAddress: "台北車站",
			RequestedAt: now, CreatedAt: now, UpdatedAt: now,
		}
		if err := rides.Create(ride); err != nil {
			t.Fatalf("建立訂單失敗：%v", err)
		}
		rows := []repository.StopRow{
			{Seq: 1, Kind: constants.StopKindPickup, Lat: 25.03, Lng: 121.56, Address: "台北車站", PassengerLabel: "A"},
			{Seq: 2, Kind: constants.StopKindPickup, Lat: 25.04, Lng: 121.57, Address: "市政府", PassengerLabel: "B"},
			{Seq: 3, Kind: constants.StopKindDropoff, Lat: 25.05, Lng: 121.58, Address: "松山機場", PassengerLabel: "A"},
			{Seq: 4, Kind: constants.StopKindDropoff, Lat: 25.06, Lng: 121.59, Address: "台北101", PassengerLabel: "B"},
		}
		if err := stopsRepo.CreateForRide(ride.ID, rows); err != nil {
			t.Fatalf("建立停靠點失敗：%v", err)
		}
		if err := rides.AcceptRide(ride.ID, driverID, 300); err != nil {
			t.Fatalf("接單失敗：%v", err)
		}
		stops, err := stopsRepo.ListByRide(ride.ID)
		if err != nil {
			t.Fatalf("讀停靠點失敗：%v", err)
		}
		ids := make([]int64, 0, len(stops))
		for _, s := range stops {
			ids = append(ids, s.ID)
		}
		return ride.ID, ids
	}

	rideID, ids := newRide(driver.ID)
	otherRideID, otherIDs := newRide(other.ID) // 同一個 DB，id 不會與上一趟相撞

	return &stopsFixture{
		svc:          NewRideStopService(rides, stopsRepo),
		stopsRepo:    stopsRepo,
		rides:        rides,
		rideID:       rideID,
		driverID:     driver.ID,
		otherID:      other.ID,
		stopIDs:      ids,
		otherRideID:  otherRideID,
		otherStopIDs: otherIDs,
	}
}

// TestMarkArrived_到達與跳過 N7 的正常路徑。
func TestMarkArrived_到達與跳過(t *testing.T) {
	f := newStopsFixture(t, "U_mark_ok")

	if err := f.svc.MarkArrived(f.driverID, f.rideID, f.stopIDs[0]); err != nil {
		t.Fatalf("標記到達失敗：%v", err)
	}
	// B 沒出現 → 司機跳過 B 的上車點（2026-07-17 拍板）。
	if err := f.svc.MarkSkipped(f.driverID, f.rideID, f.stopIDs[1]); err != nil {
		t.Fatalf("標記跳過失敗：%v", err)
	}

	stops, err := f.stopsRepo.ListByRide(f.rideID)
	if err != nil {
		t.Fatalf("讀停靠點失敗：%v", err)
	}
	if stops[0].ArrivedAt == nil || stops[0].SkippedAt != nil {
		t.Fatalf("第 1 站應為已到達：%+v", stops[0])
	}
	if !stops[1].Skipped() || stops[1].ArrivedAt != nil {
		t.Fatalf("第 2 站應為已跳過：%+v", stops[1])
	}
	// 未動過的站維持待處理。
	if !stops[2].Pending() || !stops[3].Pending() {
		t.Fatal("未標記的站應維持待處理")
	}
}

// TestMarkStop_不覆寫既有事實 到達時間是計費與稽核的原始資料。
func TestMarkStop_不覆寫既有事實(t *testing.T) {
	f := newStopsFixture(t, "U_mark_dup")

	if err := f.svc.MarkArrived(f.driverID, f.rideID, f.stopIDs[0]); err != nil {
		t.Fatalf("首次標記應成功：%v", err)
	}
	if err := f.svc.MarkArrived(f.driverID, f.rideID, f.stopIDs[0]); !errors.Is(err, ErrStopAlreadyHandled) {
		t.Fatalf("重複標記到達預期 ErrStopAlreadyHandled，得到 %v", err)
	}
	// 已到達的站不得反悔改成跳過——那會讓計費路線少算已經開過的路。
	if err := f.svc.MarkSkipped(f.driverID, f.rideID, f.stopIDs[0]); !errors.Is(err, ErrStopAlreadyHandled) {
		t.Fatalf("已到達的站不該能改成跳過，得到 %v", err)
	}

	f.mustBeArrivedOnly(t, 0)
}

func (f *stopsFixture) mustBeArrivedOnly(t *testing.T, idx int) {
	t.Helper()
	stops, err := f.stopsRepo.ListByRide(f.rideID)
	if err != nil {
		t.Fatalf("讀停靠點失敗：%v", err)
	}
	if stops[idx].ArrivedAt == nil || stops[idx].SkippedAt != nil {
		t.Fatalf("狀態不該被覆寫：%+v", stops[idx])
	}
}

// TestMarkStop_授權邊界 N7 的重點：stop_id 全域遞增，只驗 ride 歸屬是不夠的。
func TestMarkStop_授權邊界(t *testing.T) {
	f := newStopsFixture(t, "U_mark_auth")

	t.Run("別的司機不得標記", func(t *testing.T) {
		if err := f.svc.MarkArrived(f.otherID, f.rideID, f.stopIDs[0]); !errors.Is(err, ErrForbidden) {
			t.Fatalf("非被指派司機預期 ErrForbidden，得到 %v", err)
		}
	})

	t.Run("不得拿自己的 ride_id 去標記別趟的停靠點", func(t *testing.T) {
		// 用「自己有權限的 rideID」搭配「別趟的 stopID」——
		// stop_id 是全域遞增的，只驗 ride 歸屬的話這裡會過，等於任何司機
		// 都能改別人行程的停靠點狀態。
		err := f.svc.MarkArrived(f.driverID, f.rideID, f.otherStopIDs[0])
		if !errors.Is(err, ErrForbidden) {
			t.Fatalf("stop 不屬於該 ride 時預期 ErrForbidden，得到 %v", err)
		}
		// 反面：別趟的停靠點狀態不得被動到。
		stops, err := f.stopsRepo.ListByRide(f.otherRideID)
		if err != nil {
			t.Fatalf("讀停靠點失敗：%v", err)
		}
		if !stops[0].Pending() {
			t.Fatalf("別趟的停靠點不該被標記：%+v", stops[0])
		}
	})

	t.Run("行程不存在", func(t *testing.T) {
		if err := f.svc.MarkArrived(f.driverID, 999999, f.stopIDs[0]); !errors.Is(err, ErrNotFound) {
			t.Fatalf("預期 ErrNotFound，得到 %v", err)
		}
	})
}

// TestMarkStop_行程已完成不得標記 計費已定格，改了也不會重算。
func TestMarkStop_行程已完成不得標記(t *testing.T) {
	f := newStopsFixture(t, "U_mark_done")

	if err := f.rides.UpdateStatus(f.rideID, constants.RideStatusCompleted); err != nil {
		t.Fatalf("更新狀態失敗：%v", err)
	}
	if err := f.svc.MarkArrived(f.driverID, f.rideID, f.stopIDs[0]); !errors.Is(err, ErrBadStopState) {
		t.Fatalf("已完成的行程預期 ErrBadStopState，得到 %v", err)
	}
}

// TestListForDriver 司機讀自己那趟的停靠點；別人不行。
func TestListForDriver(t *testing.T) {
	f := newStopsFixture(t, "U_list")

	stops, err := f.svc.ListForDriver(f.driverID, f.rideID)
	if err != nil {
		t.Fatalf("讀取失敗：%v", err)
	}
	if len(stops) != 4 {
		t.Fatalf("預期 4 站，得到 %d", len(stops))
	}
	for i, want := range []int{1, 2, 3, 4} {
		if stops[i].Seq != want {
			t.Fatalf("應依 seq 排序：%+v", stops)
		}
	}
	if _, err := f.svc.ListForDriver(f.otherID, f.rideID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("非被指派司機預期 ErrForbidden，得到 %v", err)
	}
}
