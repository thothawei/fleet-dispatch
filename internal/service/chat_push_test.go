package service

import (
	"strings"
	"testing"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/events"
	"line-fleet-dispatch/internal/repository"
)

// 對話訊息先前**只走 WS**：對方 App 一離開前景 WS 就斷了，訊息只會躺在伺服器上，
// 要等他自己再打開 App 才看得到。這組測試釘住「推給對方、不推給自己」。

func TestChatSend_推播給對方而不是發話者(t *testing.T) {
	db := newServiceTestDB(t)
	customers := repository.NewCustomerRepository(db)
	drivers := repository.NewDriverRepository(db)
	rides := repository.NewRideRepository(db)
	messages := repository.NewRideMessageRepository(db)

	cust, err := customers.FindOrCreateByLineUserID("U_chatpush_cust", "乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	driver, err := drivers.FindOrCreate("U_chatpush_driver", "司機")
	if err != nil {
		t.Fatalf("建立司機失敗：%v", err)
	}
	ride := newTestRide(t, rides, cust.ID, constants.RideStatusRequested)
	if err := rides.AcceptRide(ride.ID, driver.ID, 300); err != nil {
		t.Fatalf("指派司機失敗：%v", err)
	}

	chat := NewChatService(rides, messages, &fakePublisher{})
	n, pusher := newFakeNotifier()
	chat.SetAppNotifier(n)

	if _, err := chat.Send(events.RoleCustomer, cust.ID, ride.ID, "我在 7-11 門口"); err != nil {
		t.Fatalf("乘客發話失敗：%v", err)
	}
	pushes := pusher.customerPushes()
	if len(pushes) != 1 {
		t.Fatalf("乘客發話應只推一則（給司機），得到 %d 則：%v", len(pushes), pushes)
	}
	if pushes[0].data["type"] != events.TypeChatMessage {
		t.Errorf("data.type=%q，預期 chat.message", pushes[0].data["type"])
	}
	if pushes[0].title != "乘客傳來訊息" {
		t.Errorf("title=%q", pushes[0].title)
	}

	if _, err := chat.Send(events.RoleDriver, driver.ID, ride.ID, "我五分鐘後到"); err != nil {
		t.Fatalf("司機發話失敗：%v", err)
	}
	pushes = pusher.customerPushes()
	if len(pushes) != 2 {
		t.Fatalf("司機回話後應累計 2 則推播，得到 %d", len(pushes))
	}
	if pushes[1].title != "司機傳來訊息" {
		t.Errorf("title=%q", pushes[1].title)
	}
	// 每則都只送一次：推給發話者自己的裝置只會讓他以為對方回話了。
	// （fakePusher 對任何角色都回一台裝置，所以「推給雙方」在這裡會是 2 則。）
}

// 還沒派到司機的訂單，乘客發話不該 panic，也不該推給不存在的司機。
func TestChatSend_尚未指派司機時不推播(t *testing.T) {
	db := newServiceTestDB(t)
	customers := repository.NewCustomerRepository(db)
	rides := repository.NewRideRepository(db)
	messages := repository.NewRideMessageRepository(db)

	cust, err := customers.FindOrCreateByLineUserID("U_chatpush_nodriver", "乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	ride := newTestRide(t, rides, cust.ID, constants.RideStatusRequested)

	chat := NewChatService(rides, messages, &fakePublisher{})
	n, pusher := newFakeNotifier()
	chat.SetAppNotifier(n)

	if _, err := chat.Send(events.RoleCustomer, cust.ID, ride.ID, "有人嗎"); err != nil {
		t.Fatalf("發話失敗：%v", err)
	}
	if got := len(pusher.customerPushes()); got != 0 {
		t.Fatalf("沒有司機時不該推播，得到 %d 則", got)
	}
}

// 未注入 notifier（既有測試與舊部署）照常運作，只是沒有推播。
func TestChatSend_未注入notifier不影響發話(t *testing.T) {
	db := newServiceTestDB(t)
	customers := repository.NewCustomerRepository(db)
	rides := repository.NewRideRepository(db)
	messages := repository.NewRideMessageRepository(db)

	cust, err := customers.FindOrCreateByLineUserID("U_chatpush_nonotify", "乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	ride := newTestRide(t, rides, cust.ID, constants.RideStatusRequested)

	chat := NewChatService(rides, messages, &fakePublisher{})
	if _, err := chat.Send(events.RoleCustomer, cust.ID, ride.ID, "沒有推播也要能發話"); err != nil {
		t.Fatalf("發話失敗：%v", err)
	}
}

// 通知本文是預覽，太長要截斷——系統匣本來就顯示不完，
// 而且切點必須以 rune 計，否則中文會被切成亂碼。
func TestPreviewText(t *testing.T) {
	short := "我在門口"
	if got := previewText(short); got != short {
		t.Errorf("短訊息不該改動：%q", got)
	}

	long := strings.Repeat("測", 100)
	got := previewText(long)
	runes := []rune(got)
	if len(runes) != 61 { // 60 + 省略號
		t.Fatalf("截斷後 rune 數=%d，預期 61", len(runes))
	}
	if runes[len(runes)-1] != '…' {
		t.Errorf("截斷後應以省略號結尾：%q", got)
	}
	for _, r := range runes[:60] {
		if r != '測' {
			t.Fatalf("切點切壞了中文：%q", got)
		}
	}
}
