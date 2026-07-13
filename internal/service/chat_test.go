package service

import (
	"errors"
	"strings"
	"testing"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/events"
	"line-fleet-dispatch/internal/repository"
)

// TestChatSendAndList 驗證行程內對話：參與者可發話並即時推播給雙方、
// 非參與者被拒、空訊息被拒、afterID 增量補讀。
func TestChatSendAndList(t *testing.T) {
	db := newServiceTestDB(t)
	customers := repository.NewCustomerRepository(db)
	drivers := repository.NewDriverRepository(db)
	rides := repository.NewRideRepository(db)
	messages := repository.NewRideMessageRepository(db)

	owner, err := customers.FindOrCreateByLineUserID("U_chat_owner", "聊天乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	stranger, err := customers.FindOrCreateByLineUserID("U_chat_stranger", "他人乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	driver, err := drivers.FindOrCreate("U_chat_driver", "聊天司機")
	if err != nil {
		t.Fatalf("建立司機失敗：%v", err)
	}

	ride := newTestRide(t, rides, owner.ID, constants.RideStatusRequested)
	if err := rides.AcceptRide(ride.ID, driver.ID, 300); err != nil {
		t.Fatalf("指派司機失敗：%v", err)
	}

	fp := &fakePublisher{}
	chat := NewChatService(rides, messages, fp)

	// 乘客發話：成功寫入且推播給乘客與司機兩方
	msg, err := chat.Send(events.RoleCustomer, owner.ID, ride.ID, "  司機你好，我在 7-11 門口  ")
	if err != nil {
		t.Fatalf("乘客發話失敗：%v", err)
	}
	if msg.Body != "司機你好，我在 7-11 門口" {
		t.Fatalf("訊息應去除前後空白，得到 %q", msg.Body)
	}
	if fp.count() != 2 {
		t.Fatalf("應推播給行程雙方（2 則），得到 %d", fp.count())
	}
	for _, rec := range fp.recv {
		if rec.Ev.Type != events.TypeChatMessage || rec.Ev.RideID != ride.ID {
			t.Fatalf("推播事件錯誤：%+v", rec.Ev)
		}
	}

	// 司機回話
	if _, err := chat.Send(events.RoleDriver, driver.ID, ride.ID, "好的，馬上到"); err != nil {
		t.Fatalf("司機發話失敗：%v", err)
	}

	// 非本趟乘客/司機發話被拒
	if _, err := chat.Send(events.RoleCustomer, stranger.ID, ride.ID, "亂入"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("他人乘客發話應被拒，got %v", err)
	}

	// 空訊息與超長訊息被拒
	if _, err := chat.Send(events.RoleCustomer, owner.ID, ride.ID, "   "); !errors.Is(err, ErrEmptyMessage) {
		t.Fatalf("空訊息應被拒，got %v", err)
	}
	if _, err := chat.Send(events.RoleCustomer, owner.ID, ride.ID, strings.Repeat("長", chatMaxRunes+1)); !errors.Is(err, ErrMessageTooLong) {
		t.Fatalf("超長訊息應被拒，got %v", err)
	}

	// 歷史查詢：參與者拿到 2 則、admin 可稽核、他人被拒
	list, err := chat.List(events.RoleCustomer, owner.ID, ride.ID, 0, 0)
	if err != nil || len(list) != 2 {
		t.Fatalf("乘客查歷史應得 2 則，got %d, err=%v", len(list), err)
	}
	if _, err := chat.List(events.RoleAdmin, 999, ride.ID, 0, 0); err != nil {
		t.Fatalf("admin 查歷史應放行，got %v", err)
	}
	if _, err := chat.List(events.RoleDriver, driver.ID+999, ride.ID, 0, 0); !errors.Is(err, ErrForbidden) {
		t.Fatalf("他人司機查歷史應被拒，got %v", err)
	}

	// afterID 增量補讀：只拿到第一則之後的訊息
	tail, err := chat.List(events.RoleDriver, driver.ID, ride.ID, list[0].ID, 0)
	if err != nil || len(tail) != 1 || tail[0].Body != "好的，馬上到" {
		t.Fatalf("afterID 增量補讀錯誤：len=%d err=%v", len(tail), err)
	}
}
