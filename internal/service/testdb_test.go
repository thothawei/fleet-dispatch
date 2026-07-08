package service

import (
	"context"
	"os"
	"testing"

	"github.com/redis/go-redis/v9"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"

	"line-fleet-dispatch/internal/database"
	redisstore "line-fleet-dispatch/internal/redis"
)

func init() {
	// 用 t.Cleanup 自行清理容器，關掉 ryuk 避免額外拉映像（比照 repository/redis 套件的整合測試慣例）
	_ = os.Setenv("TESTCONTAINERS_RYUK_DISABLED", "true")
}

// newServiceTestDB 起真 PostGIS 容器並跑「全部 db/migrations」，得到與正式一致的完整 schema。
// Docker 不可用時跳過。
func newServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	ctx := context.Background()
	container, err := tcpostgres.Run(ctx, "postgis/postgis:16-3.4",
		tcpostgres.WithDatabase("test"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Skipf("略過整合測試（Docker/testcontainers 不可用）: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("取得連線字串失敗: %v", err)
	}
	if err := database.RunMigrations(connStr, "../../db/migrations"); err != nil {
		t.Fatalf("跑 migration 失敗: %v", err)
	}
	db, err := gorm.Open(gormpostgres.Open(connStr), &gorm.Config{})
	if err != nil {
		t.Fatalf("連線失敗: %v", err)
	}
	return db
}

// newServiceTestRedis 起真 Redis 容器，Docker 不可用時跳過
func newServiceTestRedis(t *testing.T) *redisstore.Store {
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
	return redisstore.NewStore(redis.NewClient(&redis.Options{Addr: endpoint}), 60)
}
