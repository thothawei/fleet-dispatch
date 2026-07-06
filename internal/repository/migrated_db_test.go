package repository

import (
	"context"
	"testing"

	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"

	"line-fleet-dispatch/internal/database"
)

// newMigratedTestDB 起真 PostGIS 容器並跑「全部 db/migrations」，得到與正式一致的完整 schema。
// Docker 不可用時跳過。
func newMigratedTestDB(t *testing.T) *gorm.DB {
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
	// connStr 為 postgres://test:test@host:port/test?sslmode=disable，正是 migrate URL 格式
	if err := database.RunMigrations(connStr, "../../db/migrations"); err != nil {
		t.Fatalf("跑 migration 失敗: %v", err)
	}
	db, err := gorm.Open(gormpostgres.Open(connStr), &gorm.Config{})
	if err != nil {
		t.Fatalf("連線失敗: %v", err)
	}
	return db
}
