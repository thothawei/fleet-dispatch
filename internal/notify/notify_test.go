package notify

import (
	"context"
	"errors"
	"testing"
)

type pushCall struct {
	kind    string // "offer"／"update"，用來確認走的是哪一條路徑
	devices []Device
	rideID  int64
	title   string
	body    string
	data    map[string]string
}

type recordingPusher struct {
	calls []pushCall
	err   error // 非 nil 時模擬送出失敗
}

func (r *recordingPusher) SendRideOffer(ctx context.Context, devices []Device, rideID int64, title, body string, data map[string]string) error {
	_ = ctx
	r.calls = append(r.calls, pushCall{"offer", devices, rideID, title, body, data})
	return r.err
}

func (r *recordingPusher) SendRideUpdate(ctx context.Context, devices []Device, rideID int64, title, body string, data map[string]string) error {
	_ = ctx
	r.calls = append(r.calls, pushCall{"update", devices, rideID, title, body, data})
	return r.err
}

type mapLookup map[int64][]Device

func (m mapLookup) ListBySubject(role string, subjectID int64) ([]Device, error) {
	_ = role
	return m[subjectID], nil
}

// roleLookup 依「角色＋主體」查裝置——用來釘住司機／乘客不會查錯人。
type roleLookup map[string]map[int64][]Device

func (m roleLookup) ListBySubject(role string, subjectID int64) ([]Device, error) {
	return m[role][subjectID], nil
}

type failingLookup struct{}

func (failingLookup) ListBySubject(role string, subjectID int64) ([]Device, error) {
	_, _ = role, subjectID
	return nil, errors.New("db down")
}

func TestDispatcher_NotifyDriverRideOffer(t *testing.T) {
	pusher := &recordingPusher{}
	d := NewDispatcher(mapLookup{
		7: {{Platform: PlatformFCM, Token: "tok-aaaa"}},
	}, pusher)

	d.NotifyDriverRideOffer(context.Background(), 7, 99, "新派單", "請開啟 App 接單",
		map[string]string{"type": "ride.assigned", "ride_id": "99"})

	if len(pusher.calls) != 1 {
		t.Fatalf("預期送出 1 次，得到 %d", len(pusher.calls))
	}
	if pusher.calls[0].rideID != 99 {
		t.Fatalf("rideID=%d", pusher.calls[0].rideID)
	}
	if len(pusher.calls[0].devices) != 1 || pusher.calls[0].devices[0].Token != "tok-aaaa" {
		t.Fatalf("devices=%v", pusher.calls[0].devices)
	}
	// data 要一路帶到 pusher（App 被殺喚醒接單卡靠它）。
	if pusher.calls[0].data["type"] != "ride.assigned" || pusher.calls[0].data["ride_id"] != "99" {
		t.Fatalf("data=%v", pusher.calls[0].data)
	}
}

func TestDispatcher_無裝置時不送(t *testing.T) {
	pusher := &recordingPusher{}
	d := NewDispatcher(mapLookup{}, pusher)
	d.NotifyDriverRideOffer(context.Background(), 1, 2, "t", "b", nil)
	if len(pusher.calls) != 0 {
		t.Fatalf("不應送出，得到 %d", len(pusher.calls))
	}
}

// 乘客端推播的送出路徑：先前只有司機那一條，乘客註冊了 token 也永遠收不到任何推播。
func TestDispatcher_NotifyCustomerRideUpdate(t *testing.T) {
	pusher := &recordingPusher{}
	d := NewDispatcher(roleLookup{
		RoleCustomer: {5: {{Platform: PlatformFCM, Token: "cust-tok"}}},
		RoleDriver:   {5: {{Platform: PlatformFCM, Token: "driver-tok"}}},
	}, pusher)

	d.NotifyCustomerRideUpdate(context.Background(), 5, 42, "司機已抵達", "請準備上車",
		map[string]string{"type": "driver.arrived", "ride_id": "42"})

	if len(pusher.calls) != 1 {
		t.Fatalf("預期送出 1 次，得到 %d", len(pusher.calls))
	}
	call := pusher.calls[0]
	// 走的是 update 那條，不是派單邀請。
	if call.kind != "update" {
		t.Errorf("kind=%q，預期 update", call.kind)
	}
	// 同一個 ID 在司機表也有裝置——查錯角色會拿到 driver-tok。
	if len(call.devices) != 1 || call.devices[0].Token != "cust-tok" {
		t.Errorf("devices=%v，應只查乘客的裝置", call.devices)
	}
	if call.rideID != 42 || call.title != "司機已抵達" {
		t.Errorf("rideID=%d title=%q", call.rideID, call.title)
	}
	if call.data["type"] != "driver.arrived" || call.data["ride_id"] != "42" {
		t.Errorf("data=%v", call.data)
	}
}

func TestDispatcher_乘客無裝置時不送(t *testing.T) {
	pusher := &recordingPusher{}
	d := NewDispatcher(roleLookup{}, pusher)
	d.NotifyCustomerRideUpdate(context.Background(), 1, 2, "t", "b", nil)
	if len(pusher.calls) != 0 {
		t.Fatalf("不應送出，得到 %d", len(pusher.calls))
	}
}

// 查 token 失敗與送出失敗都只打 log——推播不該讓接單／完成這些主鏈路動作跟著爆。
func TestDispatcher_失敗不中斷(t *testing.T) {
	d := NewDispatcher(failingLookup{}, &recordingPusher{})
	d.NotifyCustomerRideUpdate(context.Background(), 1, 2, "t", "b", nil)

	failing := &recordingPusher{err: errors.New("fcm down")}
	d2 := NewDispatcher(roleLookup{
		RoleCustomer: {1: {{Platform: PlatformFCM, Token: "tok"}}},
	}, failing)
	d2.NotifyCustomerRideUpdate(context.Background(), 1, 2, "t", "b", nil)
	if len(failing.calls) != 1 {
		t.Fatalf("仍應嘗試送出一次，得到 %d", len(failing.calls))
	}
}

// nil Dispatcher 也要安全：service 端沒注入 notifier 時就是 nil。
func TestDispatcher_nil安全(t *testing.T) {
	var d *Dispatcher
	d.NotifyCustomerRideUpdate(context.Background(), 1, 2, "t", "b", nil)
	d.NotifyDriverRideOffer(context.Background(), 1, 2, "t", "b", nil)
}
