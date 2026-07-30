package service

import (
	"errors"
	"strings"
	"testing"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/events"
	"line-fleet-dispatch/internal/repository"
)

// 訊息送出的冪等鍵（client_msg_id）。
//
// 為什麼要有它：App 送出逾時後，「後端其實收到了、只是回應遺失」與「後端沒收到」
// 在客戶端看起來一樣，而訊息在後端**沒有唯一狀態**可查——「同內容再送一次」本來就合法，
// 所以無法像接單／取消／協尋／評分那樣靠一支查詢對帳。
// 鍵由客戶端給、後端據此去重，是唯一能同時支援「安全重試」與「真的想再說一次」的做法。

// chatIdemFixture 一趟已指派司機的行程 ＋ 一位乘客一位司機。
type chatIdemFixture struct {
	chat      *ChatService
	fp        *fakePublisher
	messages  *repository.RideMessageRepository
	customers *repository.CustomerRepository
	rideID    int64
	custID    int64
	driverID  int64
}

func newChatIdemFixture(t *testing.T, tag string) chatIdemFixture {
	t.Helper()
	db := newServiceTestDB(t)
	customers := repository.NewCustomerRepository(db)
	drivers := repository.NewDriverRepository(db)
	rides := repository.NewRideRepository(db)
	messages := repository.NewRideMessageRepository(db)

	cust, err := customers.FindOrCreateByLineUserID("U_idem_cust_"+tag, "冪等乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	driver, err := drivers.FindOrCreate("U_idem_drv_"+tag, "冪等司機")
	if err != nil {
		t.Fatalf("建立司機失敗：%v", err)
	}
	ride := newTestRide(t, rides, cust.ID, constants.RideStatusRequested)
	if err := rides.AcceptRide(ride.ID, driver.ID, 300); err != nil {
		t.Fatalf("指派司機失敗：%v", err)
	}
	fp := &fakePublisher{}
	return chatIdemFixture{
		chat:      NewChatService(rides, messages, fp),
		fp:        fp,
		messages:  messages,
		customers: customers,
		rideID:    ride.ID,
		custID:    cust.ID,
		driverID:  driver.ID,
	}
}

func (f chatIdemFixture) countMessages(t *testing.T) int {
	t.Helper()
	rows, err := f.messages.ListByRide(f.rideID, 0, 100)
	if err != nil {
		t.Fatalf("讀訊息失敗：%v", err)
	}
	return len(rows)
}

func TestChatSendWithClientID_同鍵重送回既有那筆且不重播(t *testing.T) {
	f := newChatIdemFixture(t, "resend")

	first, err := f.chat.SendWithClientID(events.RoleCustomer, f.custID, f.rideID, "我在 7-11 門口", "c-1")
	if err != nil {
		t.Fatalf("第一次送出失敗：%v", err)
	}
	// 這就是「上一次其實成功了、只是回應遺失」之後 App 的重試。
	second, err := f.chat.SendWithClientID(events.RoleCustomer, f.custID, f.rideID, "我在 7-11 門口", "c-1")
	if err != nil {
		t.Fatalf("重送不該失敗：%v", err)
	}

	if second.ID != first.ID {
		t.Fatalf("重送應回既有那筆（id %d），得到 id %d", first.ID, second.ID)
	}
	if got := f.countMessages(t); got != 1 {
		t.Fatalf("同一個鍵不該多出一則訊息，DB 共有 %d 則", got)
	}
	// 第一次送出推播給行程雙方＝2 則；重送不該再發，否則對方會看到同一句話兩次。
	if got := f.fp.count(); got != 2 {
		t.Fatalf("重送不該重播 WS 事件，累計發了 %d 則（應為 2）", got)
	}
	if first.ClientMsgID == nil || *first.ClientMsgID != "c-1" {
		t.Fatalf("寫入的訊息要留著鍵，得到 %v", first.ClientMsgID)
	}
}

func TestChatSendWithClientID_不同鍵的同內容是兩則(t *testing.T) {
	f := newChatIdemFixture(t, "twice")

	if _, err := f.chat.SendWithClientID(events.RoleCustomer, f.custID, f.rideID, "在嗎", "c-1"); err != nil {
		t.Fatalf("第一次送出失敗：%v", err)
	}
	// 使用者真的想再說一次同一句話——這是合法行為，不能被去重吃掉。
	if _, err := f.chat.SendWithClientID(events.RoleCustomer, f.custID, f.rideID, "在嗎", "c-2"); err != nil {
		t.Fatalf("第二次送出失敗：%v", err)
	}

	if got := f.countMessages(t); got != 2 {
		t.Fatalf("不同鍵的同內容應是兩則，DB 共有 %d 則", got)
	}
	if got := f.fp.count(); got != 4 {
		t.Fatalf("兩則訊息各推播雙方＝4 則，得到 %d", got)
	}
}

func TestChatSendWithClientID_去重只在同趟同發話者(t *testing.T) {
	f := newChatIdemFixture(t, "scope")

	// 鍵由客戶端產生，兩端各自產號、撞號是可能的。
	// 撞號時若被當成同一則，後方那位就發不出訊息了。
	if _, err := f.chat.SendWithClientID(events.RoleCustomer, f.custID, f.rideID, "乘客說的", "same-key"); err != nil {
		t.Fatalf("乘客送出失敗：%v", err)
	}
	if _, err := f.chat.SendWithClientID(events.RoleDriver, f.driverID, f.rideID, "司機說的", "same-key"); err != nil {
		t.Fatalf("司機送出失敗（撞號不該被當成重送）：%v", err)
	}

	rows, err := f.messages.ListByRide(f.rideID, 0, 100)
	if err != nil {
		t.Fatalf("讀訊息失敗：%v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("不同發話者的同一個鍵應各自成立，DB 共有 %d 則", len(rows))
	}
	if rows[0].Body != "乘客說的" || rows[1].Body != "司機說的" {
		t.Fatalf("兩則內容錯誤：%q / %q", rows[0].Body, rows[1].Body)
	}
}

func TestChatSendWithClientID_不帶鍵維持舊行為(t *testing.T) {
	f := newChatIdemFixture(t, "nokey")

	first, err := f.chat.Send(events.RoleCustomer, f.custID, f.rideID, "沒帶鍵一")
	if err != nil {
		t.Fatalf("送出失敗：%v", err)
	}
	second, err := f.chat.Send(events.RoleCustomer, f.custID, f.rideID, "沒帶鍵二")
	if err != nil {
		t.Fatalf("送出失敗：%v", err)
	}

	if first.ID == second.ID {
		t.Fatal("沒帶鍵時每次都該是新訊息")
	}
	if first.ClientMsgID != nil || second.ClientMsgID != nil {
		t.Fatal("沒帶鍵的訊息不該被塞進鍵（partial index 靠 NULL 放行它們）")
	}
	if got := f.countMessages(t); got != 2 {
		t.Fatalf("應有兩則，得到 %d", got)
	}
}

func TestChatSendWithClientID_鍵過長要擋在服務層(t *testing.T) {
	f := newChatIdemFixture(t, "toolong")

	_, err := f.chat.SendWithClientID(
		events.RoleCustomer, f.custID, f.rideID, "哈囉",
		strings.Repeat("x", chatMaxClientMsgIDLen+1),
	)
	if !errors.Is(err, ErrClientMsgIDTooLong) {
		t.Fatalf("鍵超過 DB 欄位長度應回 ErrClientMsgIDTooLong，得到 %v", err)
	}
	if got := f.countMessages(t); got != 0 {
		t.Fatalf("被擋下的送出不該寫入任何訊息，DB 共有 %d 則", got)
	}
}

func TestChatSendWithClientID_非參與者拿鍵也探不到別人的訊息(t *testing.T) {
	f := newChatIdemFixture(t, "authz")
	// **路人必須建在同一個 DB 裡**：再開一次 newServiceTestDB 會拿到重置過的狀態，
	// 路人的 id 剛好等於行程乘客的 id，授權就假通過了（第一版就是這樣寫錯的）。
	stranger, err := f.customers.FindOrCreateByLineUserID("U_idem_stranger", "路人")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	if stranger.ID == f.custID {
		t.Fatalf("路人與行程乘客同一個 id（%d），這個案子測不到授權", stranger.ID)
	}

	if _, err := f.chat.SendWithClientID(events.RoleCustomer, f.custID, f.rideID, "私訊", "c-1"); err != nil {
		t.Fatalf("乘客送出失敗：%v", err)
	}

	// 授權必須排在查鍵**之前**：否則外人拿鍵去試，就能從「有沒有回既有訊息」
	// 反推出那趟有沒有這則訊息。
	if _, err := f.chat.SendWithClientID(events.RoleCustomer, stranger.ID, f.rideID, "亂入", "c-1"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("非參與者應回 ErrForbidden，得到 %v", err)
	}
}
