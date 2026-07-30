package service

import (
	"context"

	"sync"
	"testing"
	"time"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/model"
	"line-fleet-dispatch/internal/repository"
)

// mustCreateRide 建一張指定狀態的訂單（供預約測試佈景）。
func mustCreateRide(t *testing.T, rides *repository.RideRepository, customerID int64, status int16) *model.Ride {
	t.Helper()
	now := time.Now()
	ride := &model.Ride{
		CustomerID:    customerID,
		Status:        status,
		PickupPoint:   model.GeoPoint{Lat: 25.03, Lng: 121.56},
		PickupAddress: "台北車站",
		RequestedAt:   now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := rides.Create(ride); err != nil {
		t.Fatalf("建立訂單失敗：%v", err)
	}
	return ride
}

// fakeRideCreator 記錄每次建單請求，並可指定要回傳的錯誤。
type fakeRideCreator struct {
	mu    sync.Mutex
	calls []CustomerCreateRequest
	rides *repository.RideRepository
	err   error
	// 建出來的訂單真的寫進 DB，才能被 scheduled_rides.ride_id 的外鍵接受。

	t *testing.T
}

func (f *fakeRideCreator) CreateByCustomer(_ context.Context, customerID int64, req CustomerCreateRequest) (*model.Ride, error) {
	f.mu.Lock()
	f.calls = append(f.calls, req)
	err := f.err
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	// 這裡刻意回 error 而不是 t.Fatalf：併發測試會從非測試 goroutine 呼叫進來，
	// 在那裡呼叫 Fatalf 只會讓那個 goroutine 靜默結束，測試反而變成綠的。
	now := time.Now()
	ride := &model.Ride{
		CustomerID:    customerID,
		Status:        constants.RideStatusRequested,
		PickupPoint:   model.GeoPoint{Lat: 25.03, Lng: 121.56},
		PickupAddress: "台北車站",
		RequestedAt:   now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := f.rides.Create(ride); err != nil {
		return nil, err
	}
	return ride, nil
}

func (f *fakeRideCreator) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

type dispatcherFixture struct {
	db       *repository.ScheduledRideRepository
	svc      *ScheduledRideService
	creator  *fakeRideCreator
	disp     *ScheduledRideDispatcher
	customer int64
}

func newDispatcherFixture(t *testing.T, lineUserID string) *dispatcherFixture {
	t.Helper()
	db := newServiceTestDB(t)
	customers := repository.NewCustomerRepository(db)
	rideRepo := repository.NewRideRepository(db)
	repo := repository.NewScheduledRideRepository(db)

	cust, err := customers.FindOrCreateByLineUserID(lineUserID, "排程乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	creator := &fakeRideCreator{rides: rideRepo, t: t}
	disp := NewScheduledRideDispatcher(repo, creator)
	return &dispatcherFixture{
		db: repo, svc: NewScheduledRideService(repo),
		creator: creator, disp: disp, customer: cust.ID,
	}
}

// scheduleAsOfEarlier 以「一小時前的現在」建立預約。
//
// 為什麼要這樣繞：建立預約時擋的最小前置時間（20 分）**大於**排程器的提前發動量（15 分），
// 所以一筆剛建好的預約永遠不可能立刻到點——那正是設計要的（否則使用者要的其實是現在叫車）。
// 但排程器要處理的偏偏是「時間已經逼近」的那些，測試得把建立時的時間軸往回推才造得出來。
func (f *dispatcherFixture) scheduleAsOfEarlier(t *testing.T, in ScheduleRideInput) *model.ScheduledRide {
	t.Helper()
	row, err := f.svc.createAt(f.customer, in, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("建立預約失敗：%v", err)
	}
	return row
}

func (f *dispatcherFixture) schedule(t *testing.T, at time.Time) *model.ScheduledRide {
	t.Helper()
	return f.scheduleAsOfEarlier(t, ScheduleRideInput{
		ScheduledAt: at,
		PickupLat:   25.0330, PickupLng: 121.5654, PickupAddress: "台北101",
	})
}

// TestDispatcherOnlyPicksDueSchedules 未進入提前發動界線的預約不該被轉單。
//
// 這是整支排程器最基本、也最該有負向對照的一條：如果 FindDue 的條件寫錯方向，
// 所有測試仍會「有東西被轉單」而看起來是綠的——差別只在「三天後的預約現在就派車了」。
func TestDispatcherOnlyPicksDueSchedules(t *testing.T) {
	f := newDispatcherFixture(t, "U_disp_due")
	now := time.Now()

	// 遠在提前量之外（3 小時後出發，提前量 15 分鐘）。
	future := f.schedule(t, now.Add(3*time.Hour))
	// 已進入提前量內（10 分鐘後出發）。
	due := f.schedule(t, now.Add(10*time.Minute))

	n, err := f.disp.Tick(context.Background())
	if err != nil {
		t.Fatalf("Tick 失敗：%v", err)
	}
	if n != 1 {
		t.Fatalf("這一輪應只轉 1 筆，得到 %d", n)
	}
	if f.creator.callCount() != 1 {
		t.Fatalf("應只呼叫一次建單，得到 %d", f.creator.callCount())
	}

	futureAfter, _ := f.db.GetByID(future.ID)
	if futureAfter.Status != constants.ScheduledRideStatusPending {
		t.Fatalf("未到點的預約應維持 pending，得到 %s", futureAfter.Status)
	}
	if futureAfter.AttemptCount != 0 {
		t.Fatalf("未到點的預約不該被認領，attempt=%d", futureAfter.AttemptCount)
	}

	dueAfter, _ := f.db.GetByID(due.ID)
	if dueAfter.Status != constants.ScheduledRideStatusDispatched {
		t.Fatalf("到點的預約應為 dispatched，得到 %s", dueAfter.Status)
	}
	if dueAfter.RideID == nil {
		t.Fatalf("已轉單的預約必須帶 ride_id")
	}
	if dueAfter.DispatchedAt == nil {
		t.Fatalf("已轉單的預約應記錄 dispatched_at")
	}
}

// TestDispatcherIsIdempotent 同一筆預約連跑兩輪只能建出一張訂單。
//
// **這支守的是「循序重跑」，不是併發**——它會綠是因為第一輪把狀態改成 dispatched，
// 第二輪的 FindDue 就撈不到了。實測確認過：把 ClaimForDispatch 的樂觀鎖整個拔掉，
// 這支仍然是綠的。真正守著併發的是 TestDispatcherClaimRejectsStaleAttempt
// 與 TestDispatcherConcurrentTicksCreateOneRide，改動認領邏輯時要看的是那兩支。
func TestDispatcherIsIdempotent(t *testing.T) {
	f := newDispatcherFixture(t, "U_disp_idem")
	row := f.schedule(t, time.Now().Add(5*time.Minute))

	if n, err := f.disp.Tick(context.Background()); err != nil || n != 1 {
		t.Fatalf("第一輪應轉 1 筆：n=%d err=%v", n, err)
	}
	if n, err := f.disp.Tick(context.Background()); err != nil || n != 0 {
		t.Fatalf("第二輪不該再轉任何一筆：n=%d err=%v", n, err)
	}
	if got := f.creator.callCount(); got != 1 {
		t.Fatalf("建單只該被呼叫一次，得到 %d 次", got)
	}

	after, _ := f.db.GetByID(row.ID)
	if after.AttemptCount != 1 {
		t.Fatalf("attempt_count 應為 1，得到 %d", after.AttemptCount)
	}
}

// TestDispatcherClaimRejectsStaleAttempt 認領帶 attempt_count 樂觀鎖：
// 拿著過期的 attempt 值認領同一筆會失敗（模擬兩個排程器同時掃到）。
func TestDispatcherClaimRejectsStaleAttempt(t *testing.T) {
	f := newDispatcherFixture(t, "U_disp_claim")
	row := f.schedule(t, time.Now().Add(5*time.Minute))

	ok, err := f.db.ClaimForDispatch(row.ID, 0)
	if err != nil || !ok {
		t.Fatalf("第一次認領應成功：ok=%v err=%v", ok, err)
	}
	// 另一個排程器手上還是舊的 attempt_count=0。
	ok, err = f.db.ClaimForDispatch(row.ID, 0)
	if err != nil {
		t.Fatalf("第二次認領出錯：%v", err)
	}
	if ok {
		t.Fatalf("拿過期 attempt_count 的認領必須失敗，否則同一筆預約會建出兩張訂單")
	}
}

// TestDispatcherConcurrentTicksCreateOneRide 兩個排程器**同時**掃到同一筆預約，
// 仍然只能建出一張訂單。
//
// 這是多副本部署（或重啟時新舊實例短暫重疊）的真實情境，也是這個功能最貴的失敗：
// 訂單一旦派出去就收不回來，重複轉單＝乘客莫名其妙多了一張要付錢的車。
// 循序版的 TestDispatcherIsIdempotent 驗不到這條——那支靠的是狀態已經改掉，
// 而併發的兩個 Tick 是在狀態改掉**之前**就都撈到了同一筆。
func TestDispatcherConcurrentTicksCreateOneRide(t *testing.T) {
	f := newDispatcherFixture(t, "U_disp_race")
	row := f.schedule(t, time.Now().Add(5*time.Minute))

	// 第二個排程器共用同一個 repo 與建單器，等同另一個 pod 上的同一支程式。
	second := NewScheduledRideDispatcher(f.db, f.creator)

	// **重疊必須是造出來的，不能靠運氣**：如果只是兩個 goroutine 各跑一次 Tick，
	// 先跑完的那個已經把狀態改成 dispatched，後跑的 FindDue 根本撈不到——
	// 那樣即使把認領的樂觀鎖整個拔掉，測試照樣是綠的（實測連跑三次都綠）。
	// 真正的併發是「兩邊都已經 FindDue 到同一筆、都還沒認領」，
	// 所以這裡直接餵兩份 attempt_count=0 的相同快照進 dispatchOne。
	snapA, snapB := *row, *row

	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make([]bool, 2)
	errs := make([]error, 2)
	for i, pair := range []struct {
		d    *ScheduledRideDispatcher
		snap *model.ScheduledRide
	}{{f.disp, &snapA}, {second, &snapB}} {
		wg.Add(1)
		go func(idx int, d *ScheduledRideDispatcher, s *model.ScheduledRide) {
			defer wg.Done()
			<-start // 起跑線：兩邊盡量同時進入認領
			results[idx], errs[idx] = d.dispatchOne(context.Background(), s)
		}(i, pair.d, pair.snap)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("排程器 %d 轉單出錯：%v", i, err)
		}
	}
	if got := f.creator.callCount(); got != 1 {
		t.Fatalf("兩個排程器同時處理同一筆，建單仍只能發生一次，得到 %d 次", got)
	}
	if results[0] == results[1] {
		t.Fatalf("必須剛好一邊認領成功、一邊落空，得到 %v / %v", results[0], results[1])
	}

	after, _ := f.db.GetByID(row.ID)
	if after.Status != constants.ScheduledRideStatusDispatched {
		t.Fatalf("應為 dispatched，得到 %s", after.Status)
	}
	if after.AttemptCount != 1 {
		t.Fatalf("只該有一次認領成功，attempt_count 應為 1，得到 %d", after.AttemptCount)
	}
}

// TestDispatcherRetriesTransientFailure 乘客到點時還在別的行程上：
// 不是永久失敗，要維持 pending 等下一輪，並把原因記在 last_error。
func TestDispatcherRetriesTransientFailure(t *testing.T) {
	f := newDispatcherFixture(t, "U_disp_retry")
	row := f.schedule(t, time.Now().Add(5*time.Minute))
	f.creator.err = ErrActiveRideExists

	if n, err := f.disp.Tick(context.Background()); err != nil || n != 0 {
		t.Fatalf("建單失敗時不該算成功：n=%d err=%v", n, err)
	}
	after, _ := f.db.GetByID(row.ID)
	if after.Status != constants.ScheduledRideStatusPending {
		t.Fatalf("暫時性失敗應維持 pending 等下一輪，得到 %s", after.Status)
	}
	if after.AttemptCount != 1 {
		t.Fatalf("attempt_count 應為 1，得到 %d", after.AttemptCount)
	}
	if after.LastError == "" {
		t.Fatalf("應記下失敗原因")
	}

	// 下一輪乘客的行程結束了 → 轉單成功，last_error 應被清掉。
	f.creator.err = nil
	if n, err := f.disp.Tick(context.Background()); err != nil || n != 1 {
		t.Fatalf("下一輪應轉單成功：n=%d err=%v", n, err)
	}
	final, _ := f.db.GetByID(row.ID)
	if final.Status != constants.ScheduledRideStatusDispatched {
		t.Fatalf("應為 dispatched，得到 %s", final.Status)
	}
	if final.LastError != "" {
		t.Fatalf("轉單成功後應清掉 last_error，得到 %q", final.LastError)
	}
}

// TestDispatcherFailsFastOnPermanentError 永久性錯誤（座標無效）要**立刻**判死，
// 不能佔著重試額度一路撐到約定時間才讓乘客發現沒車。
func TestDispatcherFailsFastOnPermanentError(t *testing.T) {
	f := newDispatcherFixture(t, "U_disp_perm")
	row := f.schedule(t, time.Now().Add(5*time.Minute))
	f.creator.err = ErrInvalidCoords

	if n, err := f.disp.Tick(context.Background()); err != nil || n != 0 {
		t.Fatalf("Tick：n=%d err=%v", n, err)
	}
	after, _ := f.db.GetByID(row.ID)
	if after.Status != constants.ScheduledRideStatusFailed {
		t.Fatalf("永久性錯誤應直接 failed，得到 %s（attempt=%d）", after.Status, after.AttemptCount)
	}
	if after.AttemptCount != 1 {
		t.Fatalf("應只嘗試一次就判死，得到 %d", after.AttemptCount)
	}
	if after.LastError == "" {
		t.Fatalf("應留下失敗原因")
	}
}

// TestDispatcherGivesUpAfterMaxAttempts 暫時性錯誤重試到上限後標 failed。
func TestDispatcherGivesUpAfterMaxAttempts(t *testing.T) {
	f := newDispatcherFixture(t, "U_disp_giveup")
	row := f.schedule(t, time.Now().Add(5*time.Minute))
	f.creator.err = ErrActiveRideExists

	for i := 0; i < constants.ScheduledRideMaxAttempts; i++ {
		if _, err := f.disp.Tick(context.Background()); err != nil {
			t.Fatalf("第 %d 輪 Tick 失敗：%v", i+1, err)
		}
	}
	after, _ := f.db.GetByID(row.ID)
	if after.Status != constants.ScheduledRideStatusFailed {
		t.Fatalf("重試用盡後應為 failed，得到 %s（attempt=%d）", after.Status, after.AttemptCount)
	}
	if after.AttemptCount != constants.ScheduledRideMaxAttempts {
		t.Fatalf("attempt_count 應為 %d，得到 %d",
			constants.ScheduledRideMaxAttempts, after.AttemptCount)
	}

	// failed 之後就不該再被撿起來重試。
	before := f.creator.callCount()
	if n, err := f.disp.Tick(context.Background()); err != nil || n != 0 {
		t.Fatalf("failed 後不該再轉單：n=%d err=%v", n, err)
	}
	if f.creator.callCount() != before {
		t.Fatalf("failed 後不該再呼叫建單")
	}
}

// TestDispatcherSkipsCancelled 已取消的預約到點時不該被轉單。
func TestDispatcherSkipsCancelled(t *testing.T) {
	f := newDispatcherFixture(t, "U_disp_cancelled")
	row := f.schedule(t, time.Now().Add(5*time.Minute))
	if _, err := f.svc.Cancel(f.customer, row.ID); err != nil {
		t.Fatalf("取消失敗：%v", err)
	}

	if n, err := f.disp.Tick(context.Background()); err != nil || n != 0 {
		t.Fatalf("已取消的預約不該被轉單：n=%d err=%v", n, err)
	}
	if f.creator.callCount() != 0 {
		t.Fatalf("已取消的預約不該呼叫建單，得到 %d 次", f.creator.callCount())
	}
}

// TestDispatcherCarriesRideFields 轉單時要把預約上的目的地與車種原封不動帶進訂單——
// 漏掉的話，乘客預約時填的目的地會在到點那一刻人間蒸發。
func TestDispatcherCarriesRideFields(t *testing.T) {
	f := newDispatcherFixture(t, "U_disp_fields")
	now := time.Now()
	dropLat, dropLng := 25.0478, 121.5170
	row := f.scheduleAsOfEarlier(t, ScheduleRideInput{
		ScheduledAt: now.Add(5 * time.Minute),
		PickupLat:   25.0330, PickupLng: 121.5654, PickupAddress: "台北101",
		DropoffLat: &dropLat, DropoffLng: &dropLng, DropoffAddress: "台北車站",
		RequiredVehicleType: constants.VehicleTypePet,
		Note:                "有一隻柴犬",
	})

	if n, err := f.disp.Tick(context.Background()); err != nil || n != 1 {
		t.Fatalf("Tick：n=%d err=%v", n, err)
	}
	if len(f.creator.calls) != 1 {
		t.Fatalf("應有一次建單呼叫，得到 %d", len(f.creator.calls))
	}
	got := f.creator.calls[0]
	if got.PickupAddress != "台北101" || got.PickupLat != 25.0330 {
		t.Fatalf("上車點沒帶過去：%+v", got)
	}
	if got.DropoffAddress != "台北車站" {
		t.Fatalf("目的地地址沒帶過去：%+v", got)
	}
	if got.DropoffLat == nil || *got.DropoffLat != dropLat {
		t.Fatalf("目的地座標沒帶過去：%+v", got.DropoffLat)
	}
	if got.RequiredVehicleType != constants.VehicleTypePet {
		t.Fatalf("指定車種沒帶過去：%q", got.RequiredVehicleType)
	}
	_ = row
}

// TestDispatcherSkipsLongExpiredSchedules 早就過了約定時間的預約不該再轉單。
//
// 觸發情境是**排程器停過一段時間**（部署、當機、DB 不可用）：重新啟動時，
// FindDue 的條件若只有上界（scheduled_at <= now+lead），那些早就過期的 pending
// 會一次全部被撿起來轉成真訂單——乘客三天前預約的車，今天突然有司機開到他家樓下。
func TestDispatcherSkipsLongExpiredSchedules(t *testing.T) {
	f := newDispatcherFixture(t, "U_disp_expired")

	// 兩小時前就該出發的預約（模擬排程器停機期間錯過的那些）。
	// 建立時的「現在」要再往前推，否則會被最小前置時間擋下來——
	// 一筆過期的預約在真實系統裡也是這樣來的：建立時完全合法，是後來才過期的。
	expired, err := f.svc.createAt(f.customer, ScheduleRideInput{
		ScheduledAt: time.Now().Add(-2 * time.Hour),
		PickupLat:   25.0330, PickupLng: 121.5654, PickupAddress: "台北101",
	}, time.Now().Add(-4*time.Hour))
	if err != nil {
		t.Fatalf("建立過期預約失敗：%v", err)
	}
	// 對照組：正常到點的那一筆，必須照常轉單。
	due := f.schedule(t, time.Now().Add(5*time.Minute))

	n, tickErr := f.disp.Tick(context.Background())
	if tickErr != nil {
		t.Fatalf("Tick 失敗：%v", tickErr)
	}

	expiredAfter, _ := f.db.GetByID(expired.ID)
	if expiredAfter.Status == constants.ScheduledRideStatusDispatched {
		t.Fatalf("兩小時前就該出發的預約被轉成訂單了——乘客會莫名其妙收到一台車")
	}
	if expiredAfter.Status != constants.ScheduledRideStatusFailed {
		t.Fatalf("過期太久的預約應標為 failed 讓乘客看得到，得到 %s", expiredAfter.Status)
	}
	if expiredAfter.LastError == "" {
		t.Fatalf("過期失敗要留下原因")
	}

	dueAfter, _ := f.db.GetByID(due.ID)
	if dueAfter.Status != constants.ScheduledRideStatusDispatched {
		t.Fatalf("正常到點的預約仍必須轉單，得到 %s", dueAfter.Status)
	}
	if n != 1 {
		t.Fatalf("這一輪應只轉 1 筆（過期那筆不算），得到 %d", n)
	}
	if f.creator.callCount() != 1 {
		t.Fatalf("過期那筆不該呼叫建單，總呼叫數應為 1，得到 %d", f.creator.callCount())
	}
}
