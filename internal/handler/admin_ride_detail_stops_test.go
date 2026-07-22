package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/model"
	"line-fleet-dispatch/internal/repository"
)

// TestAdminRideDetail_帶停靠點 驗 GET /api/admin/rides/:id 會帶 stops（N，admin 訂單詳情）。
// 客服要能回答「這趟載了誰、停了哪幾站、哪站被跳過」；沒有這個鍵，後台只看得到
// 由停靠點推導出的單一上車／下車點，中間的乘客完全消失。
func TestAdminRideDetail_帶停靠點(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newMigratedTestDB(t) // Docker 不可用時內部 t.Skip

	customers := repository.NewCustomerRepository(db)
	cust, err := customers.FindOrCreateByLineUserID("U_admin_stops", "多乘客乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	now := time.Now()
	ride := &model.Ride{
		CustomerID:    cust.ID,
		Status:        constants.RideStatusRequested,
		PickupPoint:   model.GeoPoint{Lat: 25.033, Lng: 121.5654},
		PickupAddress: "台北101",
		RequestedAt:   now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := db.Create(ride).Error; err != nil {
		t.Fatalf("建立行程失敗：%v", err)
	}

	stopRepo := repository.NewRideStopRepository(db)
	rows := []repository.StopRow{
		{Seq: 1, Kind: constants.StopKindPickup, Lat: 25.033, Lng: 121.5654, Address: "台北101", PassengerLabel: "A"},
		{Seq: 2, Kind: constants.StopKindPickup, Lat: 25.0400, Lng: 121.5600, Address: "國父紀念館", PassengerLabel: "B"},
		{Seq: 3, Kind: constants.StopKindDropoff, Lat: 25.0478, Lng: 121.5170, Address: "台北車站", PassengerLabel: "A"},
		{Seq: 4, Kind: constants.StopKindDropoff, Lat: 25.0421, Lng: 121.5080, Address: "西門町", PassengerLabel: "B"},
	}
	if err := stopRepo.CreateForRide(ride.ID, rows); err != nil {
		t.Fatalf("建立停靠點失敗：%v", err)
	}

	h := &AdminHandler{
		rides:  repository.NewRideRepository(db),
		tracks: repository.NewTrackRepository(db),
	}
	h.SetRideStops(stopRepo)
	r := gin.New()
	r.GET("/api/admin/rides/:id", h.RideDetail)

	do := func(id string) map[string]any {
		t.Helper()
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/admin/rides/"+id, nil)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("預期 200，得到 %d：%s", w.Code, w.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("回應不是合法 JSON：%v", err)
		}
		return body
	}

	body := do(itoa(ride.ID))
	raw, ok := body["stops"].([]any)
	if !ok {
		t.Fatalf("多停靠點行程應帶 stops，實際回應鍵：%v", keysOf(body))
	}
	if len(raw) != 4 {
		t.Fatalf("預期 4 站，得到 %d", len(raw))
	}
	// 形狀必須與司機／乘客端相同（共用 service.StopViews）——三端說法不一致就沒得對帳。
	first, _ := raw[0].(map[string]any)
	for _, key := range []string{"id", "seq", "kind", "lat", "lng", "passenger_label", "address"} {
		if _, ok := first[key]; !ok {
			t.Fatalf("停靠點缺少欄位 %s：%+v", key, first)
		}
	}
	if first["passenger_label"] != "A" || first["kind"] != constants.StopKindPickup {
		t.Fatalf("第一站應為乘客 A 的上車點：%+v", first)
	}
	// 未處理的站不該憑空出現 arrived_at／skipped_at（兩者皆無＝待處理）。
	if _, ok := first["arrived_at"]; ok {
		t.Fatalf("尚未到達的站不該帶 arrived_at：%+v", first)
	}

	// 單點訂單不該多出一個空 stops 陣列——前端據此決定要不要顯示停靠點區塊。
	single := &model.Ride{
		CustomerID:    cust.ID,
		Status:        constants.RideStatusRequested,
		PickupPoint:   model.GeoPoint{Lat: 25.03, Lng: 121.56},
		PickupAddress: "單點訂單",
		RequestedAt:   now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := db.Create(single).Error; err != nil {
		t.Fatalf("建立單點行程失敗：%v", err)
	}
	if _, ok := do(itoa(single.ID))["stops"]; ok {
		t.Fatal("單點訂單不該帶 stops 鍵")
	}
}

// TestAdminRideDetail_未注入停靠點repo仍可用 停靠點是加值資訊，不該讓訂單詳情整頁掛掉。
func TestAdminRideDetail_未注入停靠點repo仍可用(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newMigratedTestDB(t)

	customers := repository.NewCustomerRepository(db)
	cust, err := customers.FindOrCreateByLineUserID("U_admin_nostops", "乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	now := time.Now()
	ride := &model.Ride{
		CustomerID: cust.ID, Status: constants.RideStatusRequested,
		PickupPoint: model.GeoPoint{Lat: 25.03, Lng: 121.56}, PickupAddress: "台北車站",
		RequestedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(ride).Error; err != nil {
		t.Fatalf("建立行程失敗：%v", err)
	}

	// 刻意不呼叫 SetRideStops
	h := &AdminHandler{rides: repository.NewRideRepository(db), tracks: repository.NewTrackRepository(db)}
	r := gin.New()
	r.GET("/api/admin/rides/:id", h.RideDetail)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/admin/rides/"+itoa(ride.ID), nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("未注入停靠點 repo 仍應回 200，得到 %d：%s", w.Code, w.Body.String())
	}
}

func itoa(id int64) string {
	return strconv.FormatInt(id, 10)
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
