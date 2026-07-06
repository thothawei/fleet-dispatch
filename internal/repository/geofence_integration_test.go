package repository

import (
	"context"
	"os"
	"testing"

	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func init() {
	_ = os.Setenv("TESTCONTAINERS_RYUK_DISABLED", "true")
}

// newTestDB 起一個真 PostGIS 容器並建最小 rides 表，Docker 不可用時跳過
func newTestDB(t *testing.T) *gorm.DB {
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

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("取得連線字串失敗: %v", err)
	}
	db, err := gorm.Open(gormpostgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("連線失敗: %v", err)
	}
	if err := db.Exec("CREATE EXTENSION IF NOT EXISTS postgis").Error; err != nil {
		t.Fatalf("建立 postgis 擴充失敗: %v", err)
	}
	if err := db.Exec(`CREATE TABLE rides (id bigserial PRIMARY KEY, pickup_point geography(Point,4326) NOT NULL)`).Error; err != nil {
		t.Fatalf("建立 rides 表失敗: %v", err)
	}
	return db
}

// TestIsWithinPickup_GeofenceBoundary 圍籬邊界：約 89m 應在 100m 內、約 167m 應在外
func TestIsWithinPickup_GeofenceBoundary(t *testing.T) {
	db := newTestDB(t)
	repo := NewRideRepository(db)

	// 上車點 (lng=121.566, lat=25.034)
	if err := db.Exec(
		`INSERT INTO rides (id, pickup_point) VALUES (1, ST_SetSRID(ST_MakePoint(?, ?), 4326)::geography)`,
		121.566, 25.034,
	).Error; err != nil {
		t.Fatalf("插入訂單失敗: %v", err)
	}

	// 約 89m 北（0.0008 緯度 ≈ 89m）→ 在 100m 圍籬內
	within, err := repo.IsWithinPickup(1, 25.0348, 121.566, 100)
	if err != nil {
		t.Fatalf("IsWithinPickup 失敗: %v", err)
	}
	if !within {
		t.Fatal("約 89m 應判定在 100m 圍籬內")
	}

	// 約 167m 北（0.0015 緯度 ≈ 167m）→ 在 100m 圍籬外
	within, err = repo.IsWithinPickup(1, 25.0355, 121.566, 100)
	if err != nil {
		t.Fatalf("IsWithinPickup 失敗: %v", err)
	}
	if within {
		t.Fatal("約 167m 不應判定在 100m 圍籬內")
	}
}
