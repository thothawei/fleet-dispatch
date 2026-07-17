package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"

	"line-fleet-dispatch/internal/model"
)

// P1 的 schema 保證：乘客指定車種存在 ride 上，值域由 DB CHECK 把關。
// 它是清潔費（O6）加收與否的判斷依據，值髒掉直接影響計費。
func TestRideRequiredVehicleTypeSchema(t *testing.T) {
	db := newMigratedTestDB(t)
	rides := NewRideRepository(db)
	customers := NewCustomerRepository(db)

	cust, err := customers.FindOrCreateByLineUserID("U_req_veh", "乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗: %v", err)
	}

	newRide := func() *model.Ride {
		now := time.Now()
		return &model.Ride{
			CustomerID:    cust.ID,
			PickupPoint:   model.GeoPoint{Lat: 25.03, Lng: 121.56},
			PickupAddress: "台北車站",
			RequestedAt:   now,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
	}

	t.Run("既有建單路徑預設為不指定", func(t *testing.T) {
		// RideRepository.Create 走 raw INSERT 且不含新欄位——DB default '' 必須生效，
		// 否則既有 App／LINE 建單全掛（P2 才會開始帶車種）。
		ride := newRide()
		if err := rides.Create(ride); err != nil {
			t.Fatalf("建立訂單失敗: %v", err)
		}
		got, err := rides.GetByID(ride.ID)
		if err != nil {
			t.Fatalf("重讀訂單失敗: %v", err)
		}
		if got.RequiredVehicleType != "" {
			t.Fatalf("未指定車種時應為空字串，得到 %q", got.RequiredVehicleType)
		}
	})

	t.Run("白名單車種可寫入", func(t *testing.T) {
		for _, v := range []string{"", "sedan", "suv", "van7", "accessible", "pet"} {
			r := newRide()
			r.RequiredVehicleType = v
			if err := db.Create(r).Error; err != nil {
				t.Fatalf("車種 %q 應為合法值: %v", v, err)
			}
		}
	})

	t.Run("非白名單車種由 DB CHECK 擋下", func(t *testing.T) {
		r := newRide()
		r.RequiredVehicleType = "spaceship"
		err := db.Create(r).Error
		if err == nil {
			t.Fatal("非白名單車種不該寫得進去")
		}
		if !strings.Contains(strings.ToLower(err.Error()), "chk_rides_required_vehicle_type") {
			t.Fatalf("應由 chk_rides_required_vehicle_type 擋下，實際錯誤: %v", err)
		}
	})
}

// P1 migration 可逆性：up → down 到 O1 為止 → 再 up。
// down 寫壞平常沒人發現，等到要 rollback 才炸。
func TestRideRequiredVehicleMigrationReversible(t *testing.T) {
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

	hasColumn := func() bool {
		var n int64
		db.Raw(`SELECT count(*) FROM information_schema.columns
		        WHERE table_name='rides' AND column_name='required_vehicle_type'`).Scan(&n)
		return n > 0
	}
	hasConstraint := func() bool {
		var n int64
		db.Raw(`SELECT count(*) FROM pg_constraint WHERE conname='chk_rides_required_vehicle_type'`).Scan(&n)
		return n > 0
	}

	if !hasColumn() {
		t.Fatal("up 後應有 required_vehicle_type 欄位")
	}
	if !hasConstraint() {
		t.Fatal("up 後應有車種 CHECK")
	}

	// 以版本而非「往回一步」表達意圖：日後有人在 P1 之後加 migration，
	// 這個測試仍然驗的是 P1 的 down。
	if err := m.Migrate(17); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("down 到 version 17 失敗: %v", err)
	}
	if hasColumn() {
		t.Fatal("down 後不應還有 required_vehicle_type 欄位")
	}
	if hasConstraint() {
		t.Fatal("down 後不應還有車種 CHECK")
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("再次 up 失敗: %v", err)
	}
	if !hasColumn() || !hasConstraint() {
		t.Fatal("再次 up 後 P1 的欄位與 CHECK 應回來")
	}
}
