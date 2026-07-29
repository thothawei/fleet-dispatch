package service

import (
	"testing"

	"gorm.io/gorm"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/events"
	"line-fleet-dispatch/internal/repository"
)

// 協尋的節奏是小時級（司機要回車上翻、乘客要等），雙方幾乎都不在 App 前景——
// 先前每一步都只走 WS，等於整條流程要靠當事人自己想起來去開 App 看。
//
// 這組測試釘住「每一步推給對方、不推給動作發起者」。

func TestLostItem_每一步推給對方(t *testing.T) {
	db := newServiceTestDB(t)
	customers := repository.NewCustomerRepository(db)
	drivers := repository.NewDriverRepository(db)
	rides := repository.NewRideRepository(db)
	items := repository.NewLostItemRepository(db)

	cust, err := customers.FindOrCreateByLineUserID("U_lostpush_cust", "乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	driver, err := drivers.FindOrCreate("U_lostpush_driver", "司機")
	if err != nil {
		t.Fatalf("建立司機失敗：%v", err)
	}
	ride := newTestRide(t, rides, cust.ID, constants.RideStatusRequested)
	if err := rides.AcceptRide(ride.ID, driver.ID, 300); err != nil {
		t.Fatalf("指派司機失敗：%v", err)
	}
	if err := rides.MarkPickedUp(ride.ID); err != nil {
		t.Fatalf("上車失敗：%v", err)
	}
	fare, comm, net := int64(10000), int64(1500), int64(8500)
	if err := rides.CompleteRide(ride.ID, 3000, &fare, &comm, &net, nil); err != nil {
		t.Fatalf("完成行程失敗：%v", err)
	}

	svc := NewLostItemService(rides, items, newTestFees(t, db), &fakePublisher{})
	n, pusher := newFakeNotifier()
	svc.SetAppNotifier(n)

	// 1) 乘客建單 → 推司機
	item, err := svc.CreateByCustomer(cust.ID, ride.ID, "黑色錢包")
	if err != nil {
		t.Fatalf("建立協尋單失敗：%v", err)
	}
	pushes := pusher.customerPushes()
	if len(pushes) != 1 {
		t.Fatalf("建單應推一則給司機，得到 %d：%v", len(pushes), pushes)
	}
	if pushes[0].data["type"] != events.TypeLostItemCreated {
		t.Errorf("data.type=%q，預期 lost_item.created", pushes[0].data["type"])
	}
	if pushes[0].title != "乘客回報遺失物" {
		t.Errorf("title=%q", pushes[0].title)
	}

	// 2) 司機標記尋獲 → 推乘客，標題要講「乘客接下來要做什麼」
	if _, err := svc.MarkFound(driver.ID, item.ID); err != nil {
		t.Fatalf("標記尋獲失敗：%v", err)
	}
	pushes = pusher.customerPushes()
	if len(pushes) != 2 {
		t.Fatalf("尋獲後應累計 2 則，得到 %d", len(pushes))
	}
	if pushes[1].title != "司機找到你的遺失物了" {
		t.Errorf("title=%q", pushes[1].title)
	}
	if pushes[1].data["type"] != events.TypeLostItemUpdated {
		t.Errorf("data.type=%q，預期 lost_item.updated", pushes[1].data["type"])
	}

	// 3) 乘客付款 → 推司機
	if _, err := svc.Pay(cust.ID, item.ID); err != nil {
		t.Fatalf("支付處理費失敗：%v", err)
	}
	pushes = pusher.customerPushes()
	if len(pushes) != 3 || pushes[2].title != "乘客已支付處理費" {
		t.Fatalf("付款後的推播不對：%v", pushes)
	}

	// 4) 司機歸還 → 推乘客
	if _, err := svc.MarkReturned(driver.ID, item.ID); err != nil {
		t.Fatalf("標記歸還失敗：%v", err)
	}
	pushes = pusher.customerPushes()
	if len(pushes) != 4 || pushes[3].title != "遺失物已歸還" {
		t.Fatalf("歸還後的推播不對：%v", pushes)
	}
}

// 標題要講「收訊者接下來要做什麼」——同一個 closed 對兩邊的意思不一樣。
func TestLostItemPushTitle(t *testing.T) {
	cases := []struct {
		recipient string
		status    string
		want      string
	}{
		{events.RoleCustomer, constants.LostItemStatusFound, "司機找到你的遺失物了"},
		{events.RoleDriver, constants.LostItemStatusPaid, "乘客已支付處理費"},
		{events.RoleCustomer, constants.LostItemStatusReturned, "遺失物已歸還"},
		{events.RoleCustomer, constants.LostItemStatusClosed, "協尋已結案"},
		{events.RoleDriver, constants.LostItemStatusClosed, "乘客取消了協尋"},
	}
	for _, c := range cases {
		if got := lostItemPushTitle(c.recipient, c.status); got != c.want {
			t.Errorf("lostItemPushTitle(%q, %q)=%q，預期 %q", c.recipient, c.status, got, c.want)
		}
	}
}

// 未注入 notifier（既有測試與舊部署）照常運作。
func TestLostItem_未注入notifier仍可建單(t *testing.T) {
	db := newServiceTestDB(t)
	customers := repository.NewCustomerRepository(db)
	drivers := repository.NewDriverRepository(db)
	rides := repository.NewRideRepository(db)
	items := repository.NewLostItemRepository(db)

	cust, err := customers.FindOrCreateByLineUserID("U_lostpush_nonotify", "乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	driver, err := drivers.FindOrCreate("U_lostpush_nonotify_d", "司機")
	if err != nil {
		t.Fatalf("建立司機失敗：%v", err)
	}
	ride := newTestRide(t, rides, cust.ID, constants.RideStatusRequested)
	if err := rides.AcceptRide(ride.ID, driver.ID, 300); err != nil {
		t.Fatalf("指派司機失敗：%v", err)
	}
	if err := rides.MarkPickedUp(ride.ID); err != nil {
		t.Fatalf("上車失敗：%v", err)
	}
	fare, comm, net := int64(10000), int64(1500), int64(8500)
	if err := rides.CompleteRide(ride.ID, 3000, &fare, &comm, &net, nil); err != nil {
		t.Fatalf("完成行程失敗：%v", err)
	}

	svc := NewLostItemService(rides, items, newTestFees(t, db), &fakePublisher{})
	if _, err := svc.CreateByCustomer(cust.ID, ride.ID, "雨傘"); err != nil {
		t.Fatalf("沒有推播也要能建單：%v", err)
	}
}

// newTestFees 載入費率設定（migration 已填預設值）。
func newTestFees(t *testing.T, db *gorm.DB) *FeeSettings {
	t.Helper()
	fees, err := NewFeeSettings(repository.NewFeeSettingsRepository(db))
	if err != nil {
		t.Fatalf("載入費率設定失敗：%v", err)
	}
	return fees
}
