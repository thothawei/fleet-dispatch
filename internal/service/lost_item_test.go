package service

import (
	"errors"
	"testing"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/events"
	"line-fleet-dispatch/internal/repository"
)

// TestLostItemFlow 驗證遺失物協尋全流程：
// 已完成行程建單（處理費＝車資×%快照）→ 改費率不影響既有單 → 司機尋獲 → 乘客付費 → 歸還；
// 以及擁有權/狀態守門與重複建單防呆。
func TestLostItemFlow(t *testing.T) {
	db := newServiceTestDB(t)
	customers := repository.NewCustomerRepository(db)
	drivers := repository.NewDriverRepository(db)
	rides := repository.NewRideRepository(db)
	items := repository.NewLostItemRepository(db)
	feeRepo := repository.NewFeeSettingsRepository(db)

	fees, err := NewFeeSettings(feeRepo)
	if err != nil {
		t.Fatalf("載入費率設定失敗：%v", err)
	}
	if fees.LostItemFeeBps() != 1000 { // migration 預設 10%
		t.Fatalf("遺失物處理費預設應為 1000 bps，得到 %d", fees.LostItemFeeBps())
	}

	owner, err := customers.FindOrCreateByLineUserID("U_lost_owner", "遺失物乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	driver, err := drivers.FindOrCreate("U_lost_driver", "遺失物司機")
	if err != nil {
		t.Fatalf("建立司機失敗：%v", err)
	}

	// 已完成行程，車資 10000 分（100 元）
	ride := newTestRide(t, rides, owner.ID, constants.RideStatusRequested)
	if err := rides.AcceptRide(ride.ID, driver.ID, 300); err != nil {
		t.Fatalf("指派司機失敗：%v", err)
	}
	if err := rides.MarkPickedUp(ride.ID); err != nil {
		t.Fatalf("標記上車失敗：%v", err)
	}
	fare, comm, net := int64(10000), int64(1500), int64(8500)
	if err := rides.CompleteRide(ride.ID, 3000, &fare, &comm, &net, nil); err != nil {
		t.Fatalf("完成行程失敗：%v", err)
	}

	fp := &fakePublisher{}
	svc := NewLostItemService(rides, items, fees, fp)

	// 進行中行程不可建單
	activeRide := newTestRide(t, rides, owner.ID, constants.RideStatusPickedUp)
	if _, err := svc.CreateByCustomer(owner.ID, activeRide.ID, "雨傘"); !errors.Is(err, ErrRideNotCompleted) {
		t.Fatalf("未完成行程建單應被拒，got %v", err)
	}

	// 建單：處理費 = round(10000 × 1000 / 10000) = 1000 分
	item, err := svc.CreateByCustomer(owner.ID, ride.ID, "黑色錢包掉在後座")
	if err != nil {
		t.Fatalf("建立協尋單失敗：%v", err)
	}
	if item.FeeCents != 1000 || item.FeeBps != 1000 {
		t.Fatalf("處理費快照錯誤：fee=%d bps=%d，預期 1000/1000", item.FeeCents, item.FeeBps)
	}
	if item.Status != constants.LostItemStatusOpen || item.DriverID != driver.ID {
		t.Fatalf("協尋單初始狀態錯誤：%+v", item)
	}
	if fp.count() != 2 { // 建單推播給乘客與司機
		t.Fatalf("建單應推播 2 則，得到 %d", fp.count())
	}

	// 重複建單被拒
	if _, err := svc.CreateByCustomer(owner.ID, ride.ID, "又掉了一支手機"); !errors.Is(err, ErrLostItemExists) {
		t.Fatalf("同行程重複建單應被拒，got %v", err)
	}

	// 快照制：調高處理費% 後，既有協尋單金額不變
	newBps := int64(2000)
	if err := fees.Update(nil, nil, nil, nil, nil, &newBps, nil, nil); err != nil {
		t.Fatalf("更新處理費%%失敗：%v", err)
	}
	reloaded, err := svc.GetForRide(events.RoleCustomer, owner.ID, ride.ID)
	if err != nil || reloaded == nil {
		t.Fatalf("查協尋單失敗：%v", err)
	}
	if reloaded.FeeCents != 1000 {
		t.Fatalf("快照制失效：改費率後既有單 fee=%d，預期仍為 1000", reloaded.FeeCents)
	}

	// 狀態機：未尋獲前乘客不可付款
	if _, err := svc.Pay(owner.ID, item.ID); !errors.Is(err, ErrBadLostItemState) {
		t.Fatalf("open 狀態付款應被拒，got %v", err)
	}
	// 他人司機不可標記尋獲
	if _, err := svc.MarkFound(driver.ID+999, item.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("他人司機標記尋獲應被拒，got %v", err)
	}
	// 司機尋獲 → 乘客付款 → 司機歸還
	if _, err := svc.MarkFound(driver.ID, item.ID); err != nil {
		t.Fatalf("標記尋獲失敗：%v", err)
	}
	paid, err := svc.Pay(owner.ID, item.ID)
	if err != nil {
		t.Fatalf("付款失敗：%v", err)
	}
	if paid.Status != constants.LostItemStatusPaid || paid.PaidAt == nil {
		t.Fatalf("付款後狀態錯誤：%+v", paid)
	}
	// 已付款不可 close（只能歸還）
	if _, err := svc.Close(events.RoleCustomer, owner.ID, item.ID); !errors.Is(err, ErrBadLostItemState) {
		t.Fatalf("paid 狀態 close 應被拒，got %v", err)
	}
	done, err := svc.MarkReturned(driver.ID, item.ID)
	if err != nil || done.Status != constants.LostItemStatusReturned {
		t.Fatalf("歸還失敗：%v %+v", err, done)
	}

	// 結案後同行程可再開新單（部分唯一索引只擋未結案）
	again, err := svc.CreateByCustomer(owner.ID, ride.ID, "還有一副眼鏡")
	if err != nil {
		t.Fatalf("結案後再建單失敗：%v", err)
	}
	// 新單吃到新費率 20%：round(10000 × 2000 / 10000) = 2000
	if again.FeeCents != 2000 || again.FeeBps != 2000 {
		t.Fatalf("新單應以新費率快照：fee=%d bps=%d，預期 2000/2000", again.FeeCents, again.FeeBps)
	}
	// 未尋獲結案（open → closed）
	if _, err := svc.Close(events.RoleDriver, driver.ID, again.ID); err != nil {
		t.Fatalf("未尋獲結案失敗：%v", err)
	}

	// 司機工作清單：兩張都已結案，應為空
	list, err := svc.ListByDriver(driver.ID)
	if err != nil || len(list) != 0 {
		t.Fatalf("結案後司機清單應為空，got %d err=%v", len(list), err)
	}
}
