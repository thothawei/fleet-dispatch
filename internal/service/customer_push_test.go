package service

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/events"
	lineclient "line-fleet-dispatch/internal/line"
	"line-fleet-dispatch/internal/model"
	"line-fleet-dispatch/internal/notify"
	"line-fleet-dispatch/internal/repository"
)

// ---- 測試替身 ----

type recordedPush struct {
	rideID int64
	title  string
	data   map[string]string
}

// fakePusher 記下所有送出的推播；同時實作 notify.AppPusher 與 notify.DeviceLookup，
// 讓測試可以「所有人都有一台裝置」而不必真的寫 device_tokens 表。
type fakePusher struct {
	mu      sync.Mutex
	offers  []recordedPush
	updates []recordedPush
}

func (f *fakePusher) SendRideOffer(ctx context.Context, devices []notify.Device, rideID int64, title, body string, data map[string]string) error {
	_, _, _ = ctx, devices, body
	f.mu.Lock()
	defer f.mu.Unlock()
	f.offers = append(f.offers, recordedPush{rideID, title, data})
	return nil
}

func (f *fakePusher) SendRideUpdate(ctx context.Context, devices []notify.Device, rideID int64, title, body string, data map[string]string) error {
	_, _, _ = ctx, devices, body
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = append(f.updates, recordedPush{rideID, title, data})
	return nil
}

func (f *fakePusher) customerPushes() []recordedPush {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recordedPush(nil), f.updates...)
}

// everyoneHasDevice：任何角色任何 ID 都回一台 FCM 裝置。
type everyoneHasDevice struct{}

func (everyoneHasDevice) ListBySubject(role string, subjectID int64) ([]notify.Device, error) {
	_, _ = role, subjectID
	return []notify.Device{{Platform: notify.PlatformFCM, Token: "tok"}}, nil
}

func newFakeNotifier() (*notify.Dispatcher, *fakePusher) {
	p := &fakePusher{}
	return notify.NewDispatcher(everyoneHasDevice{}, p), p
}

// hasCustomerPush 找出指定型別的推播。
func hasCustomerPush(pushes []recordedPush, eventType string, rideID int64) *recordedPush {
	want := strconv.FormatInt(rideID, 10)
	for i := range pushes {
		if pushes[i].data["type"] == eventType && pushes[i].data["ride_id"] == want {
			return &pushes[i]
		}
	}
	return nil
}

// ---- 純邏輯：白名單與 data 形狀 ----

// 白名單刻意與 App 端 _customerPushTypes 對齊；多送 App 也會丟掉，只是白燒推播額度。
func TestCustomerPushCopy_白名單(t *testing.T) {
	shouldPush := []string{
		events.TypeRideAccepted,
		events.TypeDriverArrived,
		events.TypeRideCompleted,
		events.TypeRideCancelled,
		events.TypeRideRedispatched,
	}
	for _, ty := range shouldPush {
		title, body, ok := customerPushCopy(ty)
		if !ok {
			t.Errorf("%s 應該要推播", ty)
			continue
		}
		if title == "" || body == "" {
			t.Errorf("%s 的文案不可為空：title=%q body=%q", ty, title, body)
		}
	}

	// driver.location 每 8 秒一則，拿它推播會把電池與推播額度燒光；
	// ride.picked_up 時乘客人在車上；chat.message 另有管道（未實作）。
	for _, ty := range []string{
		events.TypeDriverLocation,
		events.TypeRidePickedUp,
		events.TypeChatMessage,
		events.TypeRideAssigned,
		events.TypeRideTaken,
		"",
	} {
		if _, _, ok := customerPushCopy(ty); ok {
			t.Errorf("%q 不該推播給乘客", ty)
		}
	}
}

// FCM data 的值一律字串；App 端 fleetEventFromPushData 據此還原成事件。
func TestCustomerRidePushData_只帶對帳需要的兩個鍵(t *testing.T) {
	data := customerRidePushData(events.TypeRideCompleted, 42)
	if data["type"] != "ride.completed" || data["ride_id"] != "42" {
		t.Fatalf("data=%v", data)
	}
	// 車資、司機姓名這類會變的欄位不放——推播延遲數十秒才到時內容早就過期。
	if len(data) != 2 {
		t.Fatalf("推播 data 應只有 type／ride_id 兩個鍵，得到 %v", data)
	}
}

func TestNotifyCustomerRide_未注入notifier不panic(t *testing.T) {
	notifyCustomerRide(context.Background(), nil, 1, 2, events.TypeRideAccepted)
}

func TestNotifyCustomerRide_白名單外不送(t *testing.T) {
	n, p := newFakeNotifier()
	notifyCustomerRide(context.Background(), n, 1, 2, events.TypeDriverLocation)
	if len(p.customerPushes()) != 0 {
		t.Fatalf("不該送出，得到 %v", p.customerPushes())
	}
}

// ---- 整合：真的走到派單／取消／抵達／完成 ----

// 接單後乘客要收到推播——先前只有 WS，App 一離開前景就收不到。
func TestAcceptRide_推播給乘客(t *testing.T) {
	db := newServiceTestDB(t)
	redisStore := newServiceTestRedis(t)
	customers := repository.NewCustomerRepository(db)
	drivers := repository.NewDriverRepository(db)
	rides := repository.NewRideRepository(db)
	dispatch := NewDispatchService(drivers, rides, customers, redisStore,
		lineclient.NewClient(""), NewETAService(nil), NewDispatchSettings(3000, 5, 20, 1, 5), &fakePublisher{})
	n, pusher := newFakeNotifier()
	dispatch.SetAppNotifier(n)

	cust, err := customers.FindOrCreateByLineUserID("U_push_cust", "乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	driver := newApprovedDriver(t, drivers, "U_push_driver", "司機", constants.DriverStatusIdle)
	ride := newTestRide(t, rides, cust.ID, constants.RideStatusAssigned)

	if _, err := dispatch.AcceptRide(context.Background(), ride.ID, driver.ID, ""); err != nil {
		t.Fatalf("接單失敗：%v", err)
	}

	got := hasCustomerPush(pusher.customerPushes(), events.TypeRideAccepted, ride.ID)
	if got == nil {
		t.Fatalf("乘客沒收到 ride.accepted 推播：%v", pusher.customerPushes())
	}
	if got.title == "" {
		t.Error("推播標題不可為空（系統匣會顯示它）")
	}
}

// 乘客自己按的取消不推播（他人就在前景），admin／系統取消才推。
func TestCancel_乘客自己取消不推播_他人取消要推(t *testing.T) {
	db := newServiceTestDB(t)
	redisStore := newServiceTestRedis(t)
	customers := repository.NewCustomerRepository(db)
	drivers := repository.NewDriverRepository(db)
	rides := repository.NewRideRepository(db)
	dispatch := NewDispatchService(drivers, rides, customers, redisStore,
		lineclient.NewClient(""), NewETAService(nil), NewDispatchSettings(3000, 5, 20, 1, 5), &fakePublisher{})
	n, pusher := newFakeNotifier()
	dispatch.SetAppNotifier(n)

	cust, err := customers.FindOrCreateByLineUserID("U_cancel_cust", "乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	ctx := context.Background()

	selfCancelled := newTestRide(t, rides, cust.ID, constants.RideStatusRequested)
	if _, err := dispatch.CancelByCustomerID(ctx, cust.ID, selfCancelled.ID); err != nil {
		t.Fatalf("乘客取消失敗：%v", err)
	}
	if got := hasCustomerPush(pusher.customerPushes(), events.TypeRideCancelled, selfCancelled.ID); got != nil {
		t.Errorf("乘客自己按的取消不該推播：%+v", *got)
	}

	adminCancelled := newTestRide(t, rides, cust.ID, constants.RideStatusRequested)
	if _, err := dispatch.cancelActiveRide(ctx, adminCancelled, events.ActorAdmin, nil, "admin_cancel"); err != nil {
		t.Fatalf("admin 取消失敗：%v", err)
	}
	if got := hasCustomerPush(pusher.customerPushes(), events.TypeRideCancelled, adminCancelled.ID); got == nil {
		t.Errorf("admin 取消應推播給乘客：%v", pusher.customerPushes())
	}
}

// 司機進上車圍籬 → 乘客收到「司機已抵達」推播。這則最需要推播：
// 乘客多半把 App 收在背景等通知，WS 這時早就斷了。
func TestGeofence_抵達推播給乘客(t *testing.T) {
	svc, rideID, driverID, pusher := trackingPushFixture(t)

	// 上車點（25.03, 121.56）本身一定在圍籬內。
	if err := svc.ReportDriverLocation(context.Background(), driverID, 25.03, 121.56); err != nil {
		t.Fatalf("回報位置失敗：%v", err)
	}

	if got := hasCustomerPush(pusher.customerPushes(), events.TypeDriverArrived, rideID); got == nil {
		t.Fatalf("乘客沒收到 driver.arrived 推播：%v", pusher.customerPushes())
	}
}

// 行程完成 → 乘客收到推播（App 據此重讀後端拿完成卡與車資）。
func TestComplete_推播給乘客(t *testing.T) {
	svc, rideID, driverID, _ := pickUpFixtureWithPublisher(t, nil, "")
	n, pusher := newFakeNotifier()
	svc.SetAppNotifier(n)

	ctx := context.Background()
	if _, err := svc.PickUp(ctx, rideID, driverID); err != nil {
		t.Fatalf("上車標記失敗：%v", err)
	}
	if err := svc.Complete(ctx, rideID, driverID); err != nil {
		t.Fatalf("完成行程失敗：%v", err)
	}

	got := hasCustomerPush(pusher.customerPushes(), events.TypeRideCompleted, rideID)
	if got == nil {
		t.Fatalf("乘客沒收到 ride.completed 推播：%v", pusher.customerPushes())
	}
	// 車資不放進推播：金額由 App 重讀後端取得（推播到達時可能已不是同一個數字）。
	if _, ok := got.data["fare_amount_cents"]; ok {
		t.Errorf("推播不該帶車資：%v", got.data)
	}
}

// trackingPushFixture 建好「已接單」的訂單與**接上真 DispatchService** 的 TrackingService。
//
// 不共用 pickUpFixtureWithPublisher：那支的 dispatch 是 nil，一走 ReportDriverLocation
// 就會在 ETA 節流後呼叫 nil 服務。要驗圍籬得走真的位置回報路徑。
func trackingPushFixture(t *testing.T) (*TrackingService, int64, int64, *fakePusher) {
	t.Helper()
	db := newServiceTestDB(t)
	redisStore := newServiceTestRedis(t)
	customers := repository.NewCustomerRepository(db)
	drivers := repository.NewDriverRepository(db)
	rides := repository.NewRideRepository(db)
	tracks := repository.NewTrackRepository(db)

	cust, err := customers.FindOrCreateByLineUserID("U_geofence_cust", "乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	driver := newApprovedDriver(t, drivers, "U_geofence_driver", "司機", constants.DriverStatusOnTrip)

	now := time.Now()
	ride := &model.Ride{
		CustomerID:    cust.ID,
		Status:        constants.RideStatusRequested,
		PickupPoint:   model.GeoPoint{Lat: 25.03, Lng: 121.56},
		PickupAddress: "台北車站",
		RequestedAt:   now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := rides.Create(ride); err != nil {
		t.Fatalf("建立訂單失敗：%v", err)
	}
	if err := rides.AcceptRide(ride.ID, driver.ID, 300); err != nil {
		t.Fatalf("接單失敗：%v", err)
	}

	fp := &fakePublisher{}
	dispatch := NewDispatchService(drivers, rides, customers, redisStore,
		lineclient.NewClient(""), NewETAService(nil), NewDispatchSettings(3000, 5, 20, 1, 5), fp)
	svc := NewTrackingService(drivers, rides, tracks, redisStore, lineclient.NewClient(""),
		dispatch, 0, 0, fp)
	n, pusher := newFakeNotifier()
	svc.SetAppNotifier(n)
	return svc, ride.ID, driver.ID, pusher
}

// newApprovedDriver 建一位「車輛已核准」的司機（O5 gate：沒核准車輛接不了單）。
func newApprovedDriver(
	t *testing.T, drivers *repository.DriverRepository, lineUserID, name string, status int16,
) *model.Driver {
	t.Helper()
	driver, err := drivers.FindOrCreate(lineUserID, name)
	if err != nil {
		t.Fatalf("建立司機失敗：%v", err)
	}
	if err := drivers.UpdateStatus(driver.ID, status); err != nil {
		t.Fatalf("設定司機狀態失敗：%v", err)
	}
	// 車牌**唯一**：同一個測試裡建兩位司機時，寫死的車牌會被「此車牌已被其他司機使用」擋下。
	plate := fmt.Sprintf("T-%d", driver.ID)
	if err := drivers.UpdateVehicle(driver.ID, constants.VehicleTypeSedan, plate); err != nil {
		t.Fatalf("設定車輛失敗：%v", err)
	}
	if err := drivers.UpdateVehicleReview(driver.ID, constants.VehicleReviewApproved, ""); err != nil {
		t.Fatalf("核准車輛失敗：%v", err)
	}
	return driver
}
