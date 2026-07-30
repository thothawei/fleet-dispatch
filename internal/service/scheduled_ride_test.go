package service

import (
	"errors"
	"testing"
	"time"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/repository"
)

// TestScheduledRideCreateValidation 建立預約的邊界：太近、太遠、座標、車種、備註長度。
func TestScheduledRideCreateValidation(t *testing.T) {
	db := newServiceTestDB(t)
	customers := repository.NewCustomerRepository(db)
	svc := NewScheduledRideService(repository.NewScheduledRideRepository(db))

	me, err := customers.FindOrCreateByLineUserID("U_sched_valid", "預約乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}

	now := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	base := ScheduleRideInput{
		PickupLat: 25.0330, PickupLng: 121.5654, PickupAddress: "台北101",
	}

	withTime := func(at time.Time) ScheduleRideInput {
		in := base
		in.ScheduledAt = at
		return in
	}

	// 太近：比最小前置時間還早。
	tooSoon := withTime(now.Add((constants.ScheduledRideMinLeadMinutes - 1) * time.Minute))
	if _, err := svc.createAt(me.ID, tooSoon, now); !errors.Is(err, ErrScheduleTooSoon) {
		t.Fatalf("太近的預約應回 ErrScheduleTooSoon，得到 %v", err)
	}

	// 沒給時間。
	if _, err := svc.createAt(me.ID, base, now); !errors.Is(err, ErrScheduleTooSoon) {
		t.Fatalf("零值時間應回 ErrScheduleTooSoon，得到 %v", err)
	}

	// 太遠：超過可預約天數。
	tooFar := withTime(now.AddDate(0, 0, constants.ScheduledRideMaxDaysAhead+1))
	if _, err := svc.createAt(me.ID, tooFar, now); !errors.Is(err, ErrScheduleTooFar) {
		t.Fatalf("太遠的預約應回 ErrScheduleTooFar，得到 %v", err)
	}

	// 剛好在最小前置時間上：可以建。
	ok := withTime(now.Add(constants.ScheduledRideMinLeadMinutes * time.Minute))
	if _, err := svc.createAt(me.ID, ok, now); err != nil {
		t.Fatalf("剛好達到最小前置時間的預約應可建立，得到 %v", err)
	}

	// 座標無效。
	badCoords := withTime(now.Add(2 * time.Hour))
	badCoords.PickupLat = 999
	if _, err := svc.createAt(me.ID, badCoords, now); !errors.Is(err, ErrInvalidCoords) {
		t.Fatalf("無效座標應回 ErrInvalidCoords，得到 %v", err)
	}

	// 車種不在白名單。
	badVehicle := withTime(now.Add(2 * time.Hour))
	badVehicle.RequiredVehicleType = "submarine"
	if _, err := svc.createAt(me.ID, badVehicle, now); !errors.Is(err, ErrInvalidVehicleType) {
		t.Fatalf("無效車種應回 ErrInvalidVehicleType，得到 %v", err)
	}

	// 備註過長。
	longNote := withTime(now.Add(2 * time.Hour))
	longNote.Note = string(make([]rune, maxScheduleNoteRunes+1))
	if _, err := svc.createAt(me.ID, longNote, now); !errors.Is(err, ErrScheduleNoteTooLong) {
		t.Fatalf("過長備註應回 ErrScheduleNoteTooLong，得到 %v", err)
	}
}

// TestScheduledRideCreateAllowsActiveRide 預約**不該**被「已有進行中訂單」擋住——
// 現在正在搭車，跟三小時後要用車沒有衝突。這道檢查只該在到點轉單那一刻生效。
func TestScheduledRideCreateAllowsActiveRide(t *testing.T) {
	db := newServiceTestDB(t)
	customers := repository.NewCustomerRepository(db)
	rides := repository.NewRideRepository(db)
	svc := NewScheduledRideService(repository.NewScheduledRideRepository(db))

	me, err := customers.FindOrCreateByLineUserID("U_sched_active", "行程中乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	mustCreateRide(t, rides, me.ID, constants.RideStatusPickedUp)

	now := time.Now()
	if _, err := svc.createAt(me.ID, ScheduleRideInput{
		ScheduledAt: now.Add(3 * time.Hour),
		PickupLat:   25.0330, PickupLng: 121.5654, PickupAddress: "台北101",
	}, now); err != nil {
		t.Fatalf("行程進行中仍應能建立未來的預約，得到 %v", err)
	}
}

// TestScheduledRideCancel 取消：pending 可取消；已轉單的回 ErrScheduleNotCancellable
// （不能宣稱取消成功——那張真訂單還在派單池裡）。
func TestScheduledRideCancel(t *testing.T) {
	db := newServiceTestDB(t)
	customers := repository.NewCustomerRepository(db)
	rides := repository.NewRideRepository(db)
	repo := repository.NewScheduledRideRepository(db)
	svc := NewScheduledRideService(repo)

	me, err := customers.FindOrCreateByLineUserID("U_sched_cancel", "取消乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	other, err := customers.FindOrCreateByLineUserID("U_sched_cancel_other", "別人")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}

	now := time.Now()
	row, err := svc.createAt(me.ID, ScheduleRideInput{
		ScheduledAt: now.Add(3 * time.Hour),
		PickupLat:   25.0330, PickupLng: 121.5654, PickupAddress: "台北101",
	}, now)
	if err != nil {
		t.Fatalf("建立預約失敗：%v", err)
	}

	// 別人的預約：讀不到也取消不了。
	if _, err := svc.Cancel(other.ID, row.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("取消別人的預約應回 ErrNotFound，得到 %v", err)
	}

	cancelled, err := svc.Cancel(me.ID, row.ID)
	if err != nil {
		t.Fatalf("取消預約失敗：%v", err)
	}
	if cancelled.Status != constants.ScheduledRideStatusCancelled {
		t.Fatalf("取消後狀態應為 cancelled，得到 %s", cancelled.Status)
	}

	// 重複取消：已不是 pending。
	if _, err := svc.Cancel(me.ID, row.ID); !errors.Is(err, ErrScheduleNotCancellable) {
		t.Fatalf("重複取消應回 ErrScheduleNotCancellable，得到 %v", err)
	}

	// 已轉單的預約同樣不可取消。
	dispatchedRow, err := svc.createAt(me.ID, ScheduleRideInput{
		ScheduledAt: now.Add(4 * time.Hour),
		PickupLat:   25.0330, PickupLng: 121.5654, PickupAddress: "台北101",
	}, now)
	if err != nil {
		t.Fatalf("建立第二筆預約失敗：%v", err)
	}
	ride := mustCreateRide(t, rides, me.ID, constants.RideStatusRequested)
	if ok, err := repo.MarkDispatched(dispatchedRow.ID, ride.ID); err != nil || !ok {
		t.Fatalf("標記已轉單失敗：ok=%v err=%v", ok, err)
	}
	if _, err := svc.Cancel(me.ID, dispatchedRow.ID); !errors.Is(err, ErrScheduleNotCancellable) {
		t.Fatalf("已轉單的預約應回 ErrScheduleNotCancellable，得到 %v", err)
	}
}

// TestScheduledRideList upcoming 只回 pending，全部則含已取消／已轉單；近的在前。
func TestScheduledRideList(t *testing.T) {
	db := newServiceTestDB(t)
	customers := repository.NewCustomerRepository(db)
	svc := NewScheduledRideService(repository.NewScheduledRideRepository(db))

	me, err := customers.FindOrCreateByLineUserID("U_sched_list", "清單乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}

	now := time.Now()
	mk := func(offset time.Duration) int64 {
		row, err := svc.createAt(me.ID, ScheduleRideInput{
			ScheduledAt: now.Add(offset),
			PickupLat:   25.0330, PickupLng: 121.5654, PickupAddress: "台北101",
		}, now)
		if err != nil {
			t.Fatalf("建立預約失敗：%v", err)
		}
		return row.ID
	}
	later := mk(8 * time.Hour)
	sooner := mk(3 * time.Hour)
	toCancel := mk(5 * time.Hour)

	if _, err := svc.Cancel(me.ID, toCancel); err != nil {
		t.Fatalf("取消失敗：%v", err)
	}

	upcoming, err := svc.List(me.ID, true)
	if err != nil {
		t.Fatalf("讀 upcoming 清單失敗：%v", err)
	}
	if len(upcoming) != 2 {
		t.Fatalf("upcoming 應只有 2 筆 pending，得到 %d", len(upcoming))
	}
	if upcoming[0].ID != sooner || upcoming[1].ID != later {
		t.Fatalf("upcoming 應近的在前，得到 %d, %d", upcoming[0].ID, upcoming[1].ID)
	}

	all, err := svc.List(me.ID, false)
	if err != nil {
		t.Fatalf("讀全部清單失敗：%v", err)
	}
	if len(all) != 3 {
		t.Fatalf("全部應有 3 筆，得到 %d", len(all))
	}
}
