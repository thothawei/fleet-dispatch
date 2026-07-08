package notify

import (
	"context"
	"testing"
)

type recordingPusher struct {
	calls []struct {
		devices []Device
		rideID  int64
		title   string
		body    string
	}
}

func (r *recordingPusher) SendRideOffer(ctx context.Context, devices []Device, rideID int64, title, body string) error {
	_ = ctx
	r.calls = append(r.calls, struct {
		devices []Device
		rideID  int64
		title   string
		body    string
	}{devices, rideID, title, body})
	return nil
}

type mapLookup map[int64][]Device

func (m mapLookup) ListBySubject(role string, subjectID int64) ([]Device, error) {
	_ = role
	return m[subjectID], nil
}

func TestDispatcher_NotifyDriverRideOffer(t *testing.T) {
	pusher := &recordingPusher{}
	d := NewDispatcher(mapLookup{
		7: {{Platform: PlatformFCM, Token: "tok-aaaa"}},
	}, pusher)

	d.NotifyDriverRideOffer(context.Background(), 7, 99, "新派單", "請開啟 App 接單")

	if len(pusher.calls) != 1 {
		t.Fatalf("預期送出 1 次，得到 %d", len(pusher.calls))
	}
	if pusher.calls[0].rideID != 99 {
		t.Fatalf("rideID=%d", pusher.calls[0].rideID)
	}
	if len(pusher.calls[0].devices) != 1 || pusher.calls[0].devices[0].Token != "tok-aaaa" {
		t.Fatalf("devices=%v", pusher.calls[0].devices)
	}
}

func TestDispatcher_無裝置時不送(t *testing.T) {
	pusher := &recordingPusher{}
	d := NewDispatcher(mapLookup{}, pusher)
	d.NotifyDriverRideOffer(context.Background(), 1, 2, "t", "b")
	if len(pusher.calls) != 0 {
		t.Fatalf("不應送出，得到 %d", len(pusher.calls))
	}
}
