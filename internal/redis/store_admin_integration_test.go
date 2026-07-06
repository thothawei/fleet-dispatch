package redisstore

import (
	"context"
	"testing"
)

func TestOnlineDriverLocations_列出在線司機(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, 60)

	if err := store.UpdateDriverLocation(ctx, 101, 25.03, 121.56); err != nil {
		t.Fatalf("更新位置失敗: %v", err)
	}
	if err := store.UpdateDriverLocation(ctx, 102, 25.04, 121.57); err != nil {
		t.Fatalf("更新位置失敗: %v", err)
	}

	locs, err := store.OnlineDriverLocations(ctx)
	if err != nil {
		t.Fatalf("OnlineDriverLocations 失敗: %v", err)
	}
	if len(locs) < 2 {
		t.Fatalf("預期至少 2 位在線司機，得到 %d", len(locs))
	}
	found := map[int64]bool{}
	for _, l := range locs {
		found[l.DriverID] = true
		if l.Lat == 0 && l.Lng == 0 {
			t.Fatalf("司機 %d 座標為空", l.DriverID)
		}
	}
	if !found[101] || !found[102] {
		t.Fatalf("未列出預期司機: %+v", locs)
	}
}
