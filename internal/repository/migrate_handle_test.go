package repository

import (
	"context"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// newMigrateHandle 起真 PostGIS 容器、跑完整 migration，回傳 DB 連線與 migrate handle。
// 給「可逆性測試」用——它們需要 migrate handle 才能 down 到指定版本，
// 而 newMigratedTestDB 只給 DB。Docker 不可用時跳過。
//
// 可逆性測試請以**版本**表達意圖（m.Migrate(N)），不要用 m.Steps(-1)：
// 後者卸的是最新的那個 migration，一旦有人在你之後新增 migration，
// 測試會安靜地改去驗別人的 down，你的 migration 反而失去覆蓋（O1 踩過，見 000018）。
func newMigrateHandle(t *testing.T) (*gorm.DB, *migrate.Migrate) {
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

	m, err := migrate.New("file://../../db/migrations", connStr)
	if err != nil {
		t.Fatalf("建立 migrate 失敗: %v", err)
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("up 失敗: %v", err)
	}

	db, err := gorm.Open(gormpostgres.Open(connStr), &gorm.Config{})
	if err != nil {
		t.Fatalf("連線失敗: %v", err)
	}
	return db, m
}
