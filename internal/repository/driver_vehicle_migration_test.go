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

// O1 migration 可逆性：up → down 1 步 → 再 up，欄位／約束都要正確消失與回來。
// down 寫壞（例如漏 DROP CONSTRAINT）平常不會有人發現，等到要 rollback 時才炸。
func TestDriverVehicleMigrationReversible(t *testing.T) {
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
	defer m.Close()

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("up 失敗: %v", err)
	}

	db, err := gorm.Open(gormpostgres.Open(connStr), &gorm.Config{})
	if err != nil {
		t.Fatalf("連線失敗: %v", err)
	}

	hasColumn := func(col string) bool {
		var n int64
		db.Raw(`SELECT count(*) FROM information_schema.columns
		        WHERE table_name='drivers' AND column_name=?`, col).Scan(&n)
		return n > 0
	}
	hasConstraint := func(name string) bool {
		var n int64
		db.Raw(`SELECT count(*) FROM pg_constraint WHERE conname=?`, name).Scan(&n)
		return n > 0
	}
	hasIndex := func(name string) bool {
		var n int64
		db.Raw(`SELECT count(*) FROM pg_indexes WHERE indexname=?`, name).Scan(&n)
		return n > 0
	}

	// up 後：欄位、CHECK、partial unique index 都在
	for _, col := range []string{"vehicle_type", "plate_number"} {
		if !hasColumn(col) {
			t.Fatalf("up 後應有欄位 %s", col)
		}
	}
	if !hasConstraint("chk_drivers_vehicle_type") {
		t.Fatal("up 後應有車種 CHECK")
	}
	if !hasIndex("uq_drivers_plate_number") {
		t.Fatal("up 後應有車牌 partial unique index")
	}

	// down 一步：O1 的產物要全部消失（含 CHECK 與 index，不能只 drop 欄位）
	if err := m.Steps(-1); err != nil {
		t.Fatalf("down 失敗: %v", err)
	}
	for _, col := range []string{"vehicle_type", "plate_number"} {
		if hasColumn(col) {
			t.Fatalf("down 後不應還有欄位 %s", col)
		}
	}
	if hasConstraint("chk_drivers_vehicle_type") {
		t.Fatal("down 後不應還有車種 CHECK")
	}
	if hasIndex("uq_drivers_plate_number") {
		t.Fatal("down 後不應還有車牌 index")
	}

	// 再 up：可重複套用
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("再次 up 失敗: %v", err)
	}
	if !hasColumn("vehicle_type") || !hasConstraint("chk_drivers_vehicle_type") ||
		!hasIndex("uq_drivers_plate_number") {
		t.Fatal("再次 up 後 O1 的欄位／約束／索引應全部回來")
	}
}
