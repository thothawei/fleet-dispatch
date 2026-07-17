package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"line-fleet-dispatch/internal/constants"
	lineclient "line-fleet-dispatch/internal/line"
	"line-fleet-dispatch/internal/model"
	osrmclient "line-fleet-dispatch/internal/osrm"
	"line-fleet-dispatch/internal/repository"
)

// fakeOSRM 記錄收到的路徑請求，供斷言「送了哪些座標」。
type fakeOSRM struct {
	mu    sync.Mutex
	paths []string
	distM float64
}

func newFakeOSRM(t *testing.T, distM float64) (*osrmclient.Client, *fakeOSRM) {
	t.Helper()
	f := &fakeOSRM{distM: distM}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.paths = append(f.paths, r.URL.Path)
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":   "Ok",
			"routes": []map[string]any{{"duration": 600.0, "distance": f.distM}},
		})
	}))
	t.Cleanup(srv.Close)
	return osrmclient.NewClient(srv.URL), f
}

func (f *fakeOSRM) lastPath(t *testing.T) string {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.paths) == 0 {
		t.Fatal("OSRM 未被呼叫")
	}
	return f.paths[len(f.paths)-1]
}

// routeStopsFixture 一趟已上車的四停行程（A 上／B 上／A 下／B 下）。
type routeStopsFixture struct {
	svc      *TrackingService
	rides    *repository.RideRepository
	stops    *repository.RideStopRepository
	osrm     *fakeOSRM
	rideID   int64
	driverID int64
	stopIDs  []int64
}

func newRouteStopsFixture(t *testing.T, prefix string, withStops bool) *routeStopsFixture {
	t.Helper()
	db := newServiceTestDB(t)
	redis := newServiceTestRedis(t)
	drivers := repository.NewDriverRepository(db)
	rides := repository.NewRideRepository(db)
	tracks := repository.NewTrackRepository(db)
	customers := repository.NewCustomerRepository(db)
	stopsRepo := repository.NewRideStopRepository(db)

	cust, err := customers.FindOrCreateByLineUserID(prefix+"_cust", "乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	driver, err := drivers.FindOrCreate(prefix+"_drv", "司機")
	if err != nil {
		t.Fatalf("建立司機失敗：%v", err)
	}

	now := time.Now()
	dropoff := model.GeoPoint{Lat: 25.0339, Lng: 121.5645}
	ride := &model.Ride{
		CustomerID: cust.ID, Status: constants.RideStatusRequested,
		PickupPoint: model.GeoPoint{Lat: 25.0478, Lng: 121.5170}, PickupAddress: "台北車站",
		DropoffPoint: &dropoff, DropoffAddress: "台北101",
		RequestedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := rides.Create(ride); err != nil {
		t.Fatalf("建立訂單失敗：%v", err)
	}

	var ids []int64
	if withStops {
		rows := []repository.StopRow{
			{Seq: 1, Kind: constants.StopKindPickup, Lat: 25.0478, Lng: 121.5170, PassengerLabel: "A"},
			{Seq: 2, Kind: constants.StopKindPickup, Lat: 25.0697, Lng: 121.5522, PassengerLabel: "B"},
			{Seq: 3, Kind: constants.StopKindDropoff, Lat: 25.0330, Lng: 121.5654, PassengerLabel: "A"},
			{Seq: 4, Kind: constants.StopKindDropoff, Lat: 25.0339, Lng: 121.5645, PassengerLabel: "B"},
		}
		if err := stopsRepo.CreateForRide(ride.ID, rows); err != nil {
			t.Fatalf("建立停靠點失敗：%v", err)
		}
		list, err := stopsRepo.ListByRide(ride.ID)
		if err != nil {
			t.Fatalf("讀停靠點失敗：%v", err)
		}
		for _, s := range list {
			ids = append(ids, s.ID)
		}
	}

	if err := rides.AcceptRide(ride.ID, driver.ID, 300); err != nil {
		t.Fatalf("接單失敗：%v", err)
	}
	if err := rides.UpdateStatus(ride.ID, constants.RideStatusPickedUp); err != nil {
		t.Fatalf("上車失敗：%v", err)
	}

	osrm, fake := newFakeOSRM(t, 12000)
	svc := NewTrackingService(drivers, rides, tracks, redis, lineclient.NewClient(""), nil, 0, 0, nil)
	svc.SetOSRM(osrm)
	svc.SetStops(stopsRepo)

	return &routeStopsFixture{
		svc: svc, rides: rides, stops: stopsRepo, osrm: fake,
		rideID: ride.ID, driverID: driver.ID, stopIDs: ids,
	}
}

// TestComplete_多停靠點走全程路線 N5：計費吃「起點 → 各停靠點 → 終點」的全程路線（含繞路），
// 不是「起點→最終目的地」的直達。
func TestComplete_多停靠點走全程路線(t *testing.T) {
	f := newRouteStopsFixture(t, "U_route_via", true)

	if err := f.svc.Complete(context.Background(), f.rideID, f.driverID); err != nil {
		t.Fatalf("完成行程失敗：%v", err)
	}

	// 送給 OSRM 的路徑應含全部四個停靠點的座標（依序）。
	path := f.osrm.lastPath(t)
	for _, want := range []string{
		"121.517000,25.047800", // A 上
		"121.552200,25.069700", // B 上（繞去松山）
		"121.565400,25.033000", // A 下
		"121.564500,25.033900", // B 下
	} {
		if !strings.Contains(path, want) {
			t.Fatalf("全程路線應含座標 %s：\n%s", want, path)
		}
	}
}

// TestComplete_跳過的停靠點不計入計費路線 2026-07-17 拍板的衍生結論：
// 乘客沒出現、司機沒去那站，就沒開那段路——計入等於收乘客沒走的路的錢。
func TestComplete_跳過的停靠點不計入計費路線(t *testing.T) {
	f := newRouteStopsFixture(t, "U_route_skip", true)

	// B 沒出現 → 跳過 B 的上車點（seq=2，繞去松山那段）與下車點（seq=4）。
	stopSvc := NewRideStopService(f.rides, f.stops)
	if err := stopSvc.MarkSkipped(f.driverID, f.rideID, f.stopIDs[1]); err != nil {
		t.Fatalf("跳過失敗：%v", err)
	}
	if err := stopSvc.MarkSkipped(f.driverID, f.rideID, f.stopIDs[3]); err != nil {
		t.Fatalf("跳過失敗：%v", err)
	}

	if err := f.svc.Complete(context.Background(), f.rideID, f.driverID); err != nil {
		t.Fatalf("完成行程失敗：%v", err)
	}

	path := f.osrm.lastPath(t)
	// 未跳過的兩站仍在。
	for _, want := range []string{"121.517000,25.047800", "121.565400,25.033000"} {
		if !strings.Contains(path, want) {
			t.Fatalf("未跳過的站應留在路線裡 %s：\n%s", want, path)
		}
	}
	// 被跳過的站不得出現——否則就是收了乘客沒走的路的錢。
	for _, gone := range []string{"121.552200,25.069700", "121.564500,25.033900"} {
		if strings.Contains(path, gone) {
			t.Fatalf("已跳過的站不該計入計費路線 %s：\n%s", gone, path)
		}
	}
}

// TestComplete_單點訂單維持兩點直達 N5 不得改變既有單點訂單的計費行為。
func TestComplete_單點訂單維持兩點直達(t *testing.T) {
	f := newRouteStopsFixture(t, "U_route_single", false) // 不建 stops

	if err := f.svc.Complete(context.Background(), f.rideID, f.driverID); err != nil {
		t.Fatalf("完成行程失敗：%v", err)
	}

	path := f.osrm.lastPath(t)
	// 只有 pickup 與 dropoff 兩個座標，中間一個分號。
	if strings.Count(path, ";") != 1 {
		t.Fatalf("單點訂單應走兩點直達路線：\n%s", path)
	}
	if !strings.Contains(path, "121.517000,25.047800") || !strings.Contains(path, "121.564500,25.033900") {
		t.Fatalf("兩點路線座標錯誤：\n%s", path)
	}
}

// TestComplete_全部跳過時退回兩點 跳過後不足兩點 → 退回現行的兩點直達，不該失敗。
func TestComplete_全部跳過時退回兩點(t *testing.T) {
	f := newRouteStopsFixture(t, "U_route_allskip", true)

	stopSvc := NewRideStopService(f.rides, f.stops)
	for _, id := range f.stopIDs[:3] { // 只留一站 → 不足兩點
		if err := stopSvc.MarkSkipped(f.driverID, f.rideID, id); err != nil {
			t.Fatalf("跳過失敗：%v", err)
		}
	}

	if err := f.svc.Complete(context.Background(), f.rideID, f.driverID); err != nil {
		t.Fatalf("完成行程不該失敗：%v", err)
	}
	if strings.Count(f.osrm.lastPath(t), ";") != 1 {
		t.Fatalf("不足兩點時應退回兩點直達：\n%s", f.osrm.lastPath(t))
	}
}
