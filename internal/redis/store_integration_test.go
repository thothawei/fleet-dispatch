package redisstore

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
)

func init() {
	// 用 t.Cleanup 自行清理容器，關掉 ryuk 避免額外拉映像
	_ = os.Setenv("TESTCONTAINERS_RYUK_DISABLED", "true")
}

// newTestStore 起一個真 Redis 容器，Docker 不可用時跳過整合測試
func newTestStore(t *testing.T, offlineSec int) *Store {
	t.Helper()
	ctx := context.Background()
	container, err := tcredis.Run(ctx, "redis:7-alpine")
	if err != nil {
		t.Skipf("略過整合測試（Docker/testcontainers 不可用）: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	endpoint, err := container.Endpoint(ctx, "")
	if err != nil {
		t.Fatalf("取得 redis endpoint 失敗: %v", err)
	}
	return NewStore(redis.NewClient(&redis.Options{Addr: endpoint}), offlineSec)
}

// TestTryLockRide_OnlyOneWinner 搶單併發：N 個 goroutine 同搶一單，只有 1 位成功
func TestTryLockRide_OnlyOneWinner(t *testing.T) {
	s := newTestStore(t, 60)
	ctx := context.Background()

	const n = 50
	var wins int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(driverID int64) {
			defer wg.Done()
			<-start // 一起起跑，最大化競態
			if ok, err := s.TryLockRide(ctx, 1, driverID); err == nil && ok {
				atomic.AddInt64(&wins, 1)
			}
		}(int64(i + 1))
	}
	close(start)
	wg.Wait()

	if wins != 1 {
		t.Fatalf("搶單併發應只有 1 位成功，實際 %d", wins)
	}
}

// TestNearbyDriverIDs_NearestAndOfflineFilter 派單：回傳由近到遠、且過濾離線司機
func TestNearbyDriverIDs_NearestAndOfflineFilter(t *testing.T) {
	s := newTestStore(t, 60)
	ctx := context.Background()
	pickupLat, pickupLng := 25.034, 121.566

	// driver 1：最近、在線
	if err := s.UpdateDriverLocation(ctx, 1, 25.0341, 121.5661); err != nil {
		t.Fatal(err)
	}
	// driver 2：較遠（約 2km）、在線
	if err := s.UpdateDriverLocation(ctx, 2, 25.052, 121.566); err != nil {
		t.Fatal(err)
	}
	// driver 3：很近、但離線（把 updated_at 覆寫成一小時前）
	if err := s.UpdateDriverLocation(ctx, 3, 25.0342, 121.5662); err != nil {
		t.Fatal(err)
	}
	if err := s.client.HSet(ctx, "driver:3:loc", "updated_at", time.Now().Add(-time.Hour).Unix()).Err(); err != nil {
		t.Fatal(err)
	}

	ids, err := s.NearbyDriverIDs(ctx, pickupLat, pickupLng, 5000, 5)
	if err != nil {
		t.Fatalf("NearbyDriverIDs 失敗: %v", err)
	}

	if len(ids) != 2 {
		t.Fatalf("應回 2 位在線司機（離線的 3 被過濾），實際 %v", ids)
	}
	if ids[0] != 1 || ids[1] != 2 {
		t.Fatalf("應由近到遠 [1,2]，實際 %v", ids)
	}
	for _, id := range ids {
		if id == 3 {
			t.Fatalf("離線司機 3 不應出現，實際 %v", ids)
		}
	}
}
