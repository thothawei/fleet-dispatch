package service

import (
	"context"
	"errors"
	"testing"

	"line-fleet-dispatch/internal/constants"
	lineclient "line-fleet-dispatch/internal/line"
	"line-fleet-dispatch/internal/repository"
)

// T1（2026-07-30）：接單／取消「當下做不到」時，先前回的是 (訊息, nil)，
// HTTP 層照樣送 200。**任何只看有沒有丟例外的客戶端都會把失敗當成功**——
// 沒搶到的司機會拿到一張完整但假的行程卡，開去接一個不存在的乘客。
//
// 這組測試釘住：這些情況一律回 sentinel error（handler 對應 409），文案不變。

func TestAcceptRide_已被接走回ErrRideTaken(t *testing.T) {
	db := newServiceTestDB(t)
	redisStore := newServiceTestRedis(t)
	customers := repository.NewCustomerRepository(db)
	drivers := repository.NewDriverRepository(db)
	rides := repository.NewRideRepository(db)
	dispatch := NewDispatchService(drivers, rides, customers, redisStore,
		lineclient.NewClient(""), NewETAService(nil), NewDispatchSettings(3000, 5, 20, 1, 5), &fakePublisher{})

	cust, err := customers.FindOrCreateByLineUserID("U_conflict_cust", "乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	winner := newApprovedDriver(t, drivers, "U_conflict_win", "搶到的", constants.DriverStatusIdle)
	loser := newApprovedDriver(t, drivers, "U_conflict_lose", "沒搶到的", constants.DriverStatusIdle)
	ride := newTestRide(t, rides, cust.ID, constants.RideStatusAssigned)

	ctx := context.Background()
	if _, err := dispatch.AcceptRide(ctx, ride.ID, winner.ID, ""); err != nil {
		t.Fatalf("第一位接單應成功：%v", err)
	}

	_, err = dispatch.AcceptRide(ctx, ride.ID, loser.ID, "")
	if !errors.Is(err, ErrRideTaken) {
		t.Fatalf("沒搶到的應得到 ErrRideTaken，得到 %v", err)
	}
	// 文案不變——LINE 與 App 都直接顯示它。
	if err.Error() != "手慢了，這單已被其他司機接走" {
		t.Errorf("文案被改動了：%q", err.Error())
	}
}

func TestAcceptRide_非待命狀態回ErrDriverNotIdle(t *testing.T) {
	db := newServiceTestDB(t)
	redisStore := newServiceTestRedis(t)
	customers := repository.NewCustomerRepository(db)
	drivers := repository.NewDriverRepository(db)
	rides := repository.NewRideRepository(db)
	dispatch := NewDispatchService(drivers, rides, customers, redisStore,
		lineclient.NewClient(""), NewETAService(nil), NewDispatchSettings(3000, 5, 20, 1, 5), &fakePublisher{})

	cust, err := customers.FindOrCreateByLineUserID("U_conflict_idle_cust", "乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	// 車輛已核准但狀態是 OnTrip（例如手上還有一趟）。
	driver := newApprovedDriver(t, drivers, "U_conflict_ontrip", "忙碌司機", constants.DriverStatusOnTrip)
	ride := newTestRide(t, rides, cust.ID, constants.RideStatusAssigned)

	_, err = dispatch.AcceptRide(context.Background(), ride.ID, driver.ID, "")
	if !errors.Is(err, ErrDriverNotIdle) {
		t.Fatalf("預期 ErrDriverNotIdle，得到 %v", err)
	}
}

// 取消一張「狀態已經變過」的訂單（這裡用已完成）→ ErrRideStateChanged。
func TestCancel_狀態已變更回ErrRideStateChanged(t *testing.T) {
	db := newServiceTestDB(t)
	redisStore := newServiceTestRedis(t)
	customers := repository.NewCustomerRepository(db)
	drivers := repository.NewDriverRepository(db)
	rides := repository.NewRideRepository(db)
	dispatch := NewDispatchService(drivers, rides, customers, redisStore,
		lineclient.NewClient(""), NewETAService(nil), NewDispatchSettings(3000, 5, 20, 1, 5), &fakePublisher{})

	cust, err := customers.FindOrCreateByLineUserID("U_conflict_done", "乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	ride := newTestRide(t, rides, cust.ID, constants.RideStatusCompleted)

	_, err = dispatch.CancelByCustomerID(context.Background(), cust.ID, ride.ID)
	if !errors.Is(err, ErrRideStateChanged) {
		t.Fatalf("預期 ErrRideStateChanged，得到 %v", err)
	}
}
