package repository

import (
	"strconv"
	"testing"
	"time"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/model"
)

// seedRidesForList 建立 3 筆訂單：
//
//	#1 台北車站   REQUESTED  requested_at = 2026-07-01
//	#2 台北101    COMPLETED  requested_at = 2026-07-05
//	#3 高雄車站   REQUESTED  requested_at = 2026-07-10
func seedRidesForList(t *testing.T, rideRepo *RideRepository, custRepo *CustomerRepository) []int64 {
	t.Helper()
	cust, err := custRepo.FindOrCreateByLineUserID("U_ride_list_test", "測試客")
	if err != nil {
		t.Fatalf("建立客戶失敗: %v", err)
	}

	seeds := []struct {
		address string
		status  int16
		day     string
	}{
		{"台北車站", constants.RideStatusRequested, "2026-07-01"},
		{"台北101", constants.RideStatusCompleted, "2026-07-05"},
		{"高雄車站", constants.RideStatusRequested, "2026-07-10"},
	}

	ids := make([]int64, 0, len(seeds))
	for _, s := range seeds {
		day, err := time.Parse("2006-01-02", s.day)
		if err != nil {
			t.Fatalf("測試日期解析失敗: %v", err)
		}
		ride := &model.Ride{
			CustomerID:    cust.ID,
			Status:        s.status,
			PickupPoint:   model.GeoPoint{Lat: 25.03, Lng: 121.56},
			PickupAddress: s.address,
			RequestedAt:   day,
			CreatedAt:     day,
			UpdatedAt:     day,
		}
		if err := rideRepo.Create(ride); err != nil {
			t.Fatalf("建立訂單 %s 失敗: %v", s.address, err)
		}
		ids = append(ids, ride.ID)
	}
	return ids
}

func TestRideList_日期區間依requested_at含頭尾(t *testing.T) {
	db := newMigratedTestDB(t)
	rideRepo := NewRideRepository(db)
	seedRidesForList(t, rideRepo, NewCustomerRepository(db))

	rows, total, err := rideRepo.List(RideListFilter{From: "2026-07-01", To: "2026-07-05"})
	if err != nil {
		t.Fatalf("List 失敗: %v", err)
	}
	if total != 2 || len(rows) != 2 {
		t.Fatalf("07-01~07-05 應為 2 筆（含頭尾），得 total=%d rows=%d", total, len(rows))
	}

	// 邊界：單日區間應抓到當天那筆
	rows, total, err = rideRepo.List(RideListFilter{From: "2026-07-10", To: "2026-07-10"})
	if err != nil {
		t.Fatalf("List 失敗: %v", err)
	}
	if total != 1 || rows[0].PickupAddress != "高雄車站" {
		t.Fatalf("單日區間應只抓到高雄車站，得 total=%d rows=%+v", total, rows)
	}

	// 區間外應為空
	_, total, err = rideRepo.List(RideListFilter{From: "2026-07-11", To: "2026-07-12"})
	if err != nil {
		t.Fatalf("List 失敗: %v", err)
	}
	if total != 0 {
		t.Fatalf("區間外應為 0 筆，得 %d", total)
	}
}

func TestRideList_關鍵字比對地址與訂單ID(t *testing.T) {
	db := newMigratedTestDB(t)
	rideRepo := NewRideRepository(db)
	ids := seedRidesForList(t, rideRepo, NewCustomerRepository(db))

	// 地址子字串：「台北」應命中兩筆
	_, total, err := rideRepo.List(RideListFilter{Q: "台北"})
	if err != nil {
		t.Fatalf("List 失敗: %v", err)
	}
	if total != 2 {
		t.Fatalf("關鍵字「台北」應命中 2 筆，得 %d", total)
	}

	// 訂單 ID：以第一筆的 id 當關鍵字，至少要命中它自己
	rows, _, err := rideRepo.List(RideListFilter{Q: strconv.FormatInt(ids[0], 10)})
	if err != nil {
		t.Fatalf("List 失敗: %v", err)
	}
	var hit bool
	for _, r := range rows {
		if r.ID == ids[0] {
			hit = true
		}
	}
	if !hit {
		t.Fatalf("以 id=%d 當關鍵字應命中該筆，得 %+v", ids[0], rows)
	}

	// 不存在的關鍵字
	_, total, err = rideRepo.List(RideListFilter{Q: "不存在的地址"})
	if err != nil {
		t.Fatalf("List 失敗: %v", err)
	}
	if total != 0 {
		t.Fatalf("不存在的關鍵字應為 0 筆，得 %d", total)
	}
}

func TestRideList_分頁total不受limit影響(t *testing.T) {
	db := newMigratedTestDB(t)
	rideRepo := NewRideRepository(db)
	seedRidesForList(t, rideRepo, NewCustomerRepository(db))

	// 第一頁：1 筆資料，但 total 仍是全部 3 筆
	rows, total, err := rideRepo.List(RideListFilter{Limit: 1, Offset: 0})
	if err != nil {
		t.Fatalf("List 失敗: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("limit=1 應只回 1 筆，得 %d", len(rows))
	}
	if total != 3 {
		t.Fatalf("total 應為 3（不受 limit 影響），得 %d", total)
	}
	first := rows[0].ID

	// 第二頁：offset 位移後應是不同的訂單，且順序為 id 由大到小
	rows, _, err = rideRepo.List(RideListFilter{Limit: 1, Offset: 1})
	if err != nil {
		t.Fatalf("List 失敗: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("第二頁應回 1 筆，得 %d", len(rows))
	}
	if rows[0].ID == first {
		t.Fatalf("offset=1 應換一筆，仍是 id=%d", first)
	}
	if rows[0].ID > first {
		t.Fatalf("應依 id 由新到舊，第二頁 id=%d 不該大於第一頁 id=%d", rows[0].ID, first)
	}

	// offset 超出範圍：空結果但 total 不變
	rows, total, err = rideRepo.List(RideListFilter{Limit: 10, Offset: 99})
	if err != nil {
		t.Fatalf("List 失敗: %v", err)
	}
	if len(rows) != 0 || total != 3 {
		t.Fatalf("offset 超界應回 0 筆但 total=3，得 rows=%d total=%d", len(rows), total)
	}
}

func TestRideList_狀態與關鍵字可疊加(t *testing.T) {
	db := newMigratedTestDB(t)
	rideRepo := NewRideRepository(db)
	seedRidesForList(t, rideRepo, NewCustomerRepository(db))

	completed := int16(constants.RideStatusCompleted)
	rows, total, err := rideRepo.List(RideListFilter{Status: &completed, Q: "台北"})
	if err != nil {
		t.Fatalf("List 失敗: %v", err)
	}
	// 台北有兩筆，但只有台北101 是 COMPLETED
	if total != 1 || rows[0].PickupAddress != "台北101" {
		t.Fatalf("狀態＋關鍵字應只命中台北101，得 total=%d rows=%+v", total, rows)
	}
}

func TestRideList_預設limit與非法值(t *testing.T) {
	db := newMigratedTestDB(t)
	rideRepo := NewRideRepository(db)
	seedRidesForList(t, rideRepo, NewCustomerRepository(db))

	// limit=0 / 負數 / 超上限 都應套用預設值，而不是回空
	for _, limit := range []int{0, -5, RideListMaxLimit + 1} {
		rows, _, err := rideRepo.List(RideListFilter{Limit: limit})
		if err != nil {
			t.Fatalf("limit=%d List 失敗: %v", limit, err)
		}
		if len(rows) != 3 {
			t.Fatalf("limit=%d 應套預設值回 3 筆，得 %d", limit, len(rows))
		}
	}

	// offset 負數視為 0
	rows, _, err := rideRepo.List(RideListFilter{Offset: -1})
	if err != nil {
		t.Fatalf("List 失敗: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("offset=-1 應視為 0，得 %d 筆", len(rows))
	}
}

func TestRideListRecent_仍沿用List(t *testing.T) {
	db := newMigratedTestDB(t)
	rideRepo := NewRideRepository(db)
	seedRidesForList(t, rideRepo, NewCustomerRepository(db))

	rows, err := rideRepo.ListRecent(nil, 2)
	if err != nil {
		t.Fatalf("ListRecent 失敗: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("ListRecent(nil, 2) 應回 2 筆，得 %d", len(rows))
	}
}
