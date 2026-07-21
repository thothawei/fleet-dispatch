package service

import (
	"testing"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/repository"
)

// 乘客送得出 stops（建單時），也要看得到自己排的行程走到哪一站。
// 形狀必須與司機端 DriverRideView.Stops 完全相同，App 端才能共用同一套解析。

func TestGetActiveRideByCustomer_多停靠點應帶stops(t *testing.T) {
	db := newServiceTestDB(t)
	customers := repository.NewCustomerRepository(db)
	tracks := repository.NewTrackRepository(db)
	rides := repository.NewRideRepository(db)
	stops := repository.NewRideStopRepository(db)

	cust, err := customers.FindOrCreateByLineUserID("U_cust_stops", "測試乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	ride := newTestRide(t, rides, cust.ID, constants.RideStatusAccepted)
	if err := stops.CreateForRide(ride.ID, []repository.StopRow{
		{Seq: 1, Kind: "pickup", Lat: 25.033, Lng: 121.5654, Address: "台北101", PassengerLabel: "A"},
		{Seq: 2, Kind: "dropoff", Lat: 25.0478, Lng: 121.517, Address: "台北車站", PassengerLabel: "A"},
	}); err != nil {
		t.Fatalf("建立停靠點失敗：%v", err)
	}

	q := NewRideQueryService(tracks, rides)
	q.SetStops(stops)

	got, err := q.GetActiveRideByCustomer(cust.ID)
	if err != nil {
		t.Fatalf("查詢失敗：%v", err)
	}
	if got == nil {
		t.Fatal("預期查到進行中訂單")
	}
	if len(got.Stops) != 2 {
		t.Fatalf("預期帶 2 個停靠點，得到 %d", len(got.Stops))
	}
	// 欄位形狀與司機端一致：座標攤平成 lat／lng，App 端不必另寫一套解析。
	first := got.Stops[0]
	for _, key := range []string{"id", "seq", "kind", "lat", "lng", "passenger_label", "address"} {
		if _, ok := first[key]; !ok {
			t.Errorf("停靠點缺少欄位 %s：%v", key, first)
		}
	}
	// 待處理的站不該帶到達／跳過時間——App 靠「兩者皆無」判斷待處理。
	if _, ok := first["arrived_at"]; ok {
		t.Errorf("未到達的站不該有 arrived_at：%v", first)
	}
}

func TestGetActiveRideByCustomer_到站標記要反映在乘客端(t *testing.T) {
	// 這是「行程進度」的核心：乘客要看得出司機走到第幾站。
	db := newServiceTestDB(t)
	customers := repository.NewCustomerRepository(db)
	tracks := repository.NewTrackRepository(db)
	rides := repository.NewRideRepository(db)
	stops := repository.NewRideStopRepository(db)

	cust, err := customers.FindOrCreateByLineUserID("U_cust_stops_progress", "測試乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	ride := newTestRide(t, rides, cust.ID, constants.RideStatusPickedUp)
	if err := stops.CreateForRide(ride.ID, []repository.StopRow{
		{Seq: 1, Kind: "pickup", Lat: 25.033, Lng: 121.5654, PassengerLabel: "A"},
		{Seq: 2, Kind: "pickup", Lat: 25.036, Lng: 121.568, PassengerLabel: "B"},
		{Seq: 3, Kind: "dropoff", Lat: 25.0478, Lng: 121.517, PassengerLabel: "A"},
	}); err != nil {
		t.Fatalf("建立停靠點失敗：%v", err)
	}
	rows, err := stops.ListByRide(ride.ID)
	if err != nil {
		t.Fatalf("讀停靠點失敗：%v", err)
	}
	if ok, err := stops.MarkArrived(rows[0].ID); err != nil || !ok {
		t.Fatalf("標記到達失敗：ok=%v err=%v", ok, err)
	}
	if ok, err := stops.MarkSkipped(rows[1].ID); err != nil || !ok {
		t.Fatalf("標記跳過失敗：ok=%v err=%v", ok, err)
	}

	q := NewRideQueryService(tracks, rides)
	q.SetStops(stops)

	got, err := q.GetActiveRideByCustomer(cust.ID)
	if err != nil || got == nil {
		t.Fatalf("查詢失敗：got=%v err=%v", got, err)
	}
	if _, ok := got.Stops[0]["arrived_at"]; !ok {
		t.Errorf("已到達的站要帶 arrived_at：%v", got.Stops[0])
	}
	if _, ok := got.Stops[1]["skipped_at"]; !ok {
		t.Errorf("已跳過的站要帶 skipped_at：%v", got.Stops[1])
	}
	if _, ok := got.Stops[2]["arrived_at"]; ok {
		t.Errorf("尚未處理的站不該有 arrived_at：%v", got.Stops[2])
	}
}

func TestGetRideForCustomer_多停靠點應帶stops(t *testing.T) {
	// 單筆查詢（含遺失物協尋回頭查）也要帶，否則歷史行程看不到當時的全程。
	db := newServiceTestDB(t)
	customers := repository.NewCustomerRepository(db)
	tracks := repository.NewTrackRepository(db)
	rides := repository.NewRideRepository(db)
	stops := repository.NewRideStopRepository(db)

	cust, err := customers.FindOrCreateByLineUserID("U_cust_stops_single", "測試乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	ride := newTestRide(t, rides, cust.ID, constants.RideStatusCompleted)
	if err := stops.CreateForRide(ride.ID, []repository.StopRow{
		{Seq: 1, Kind: "pickup", Lat: 25.033, Lng: 121.5654, PassengerLabel: "A"},
		{Seq: 2, Kind: "dropoff", Lat: 25.0478, Lng: 121.517, PassengerLabel: "A"},
	}); err != nil {
		t.Fatalf("建立停靠點失敗：%v", err)
	}

	q := NewRideQueryService(tracks, rides)
	q.SetStops(stops)

	got, err := q.GetRideForCustomer(cust.ID, ride.ID)
	if err != nil {
		t.Fatalf("查詢失敗：%v", err)
	}
	if len(got.Stops) != 2 {
		t.Fatalf("預期帶 2 個停靠點，得到 %d", len(got.Stops))
	}
}

func TestCustomerRideView_單點訂單不帶stops鍵(t *testing.T) {
	// omitempty：單點訂單的回應形狀一個位元組都不該變，舊版 App 不受影響。
	db := newServiceTestDB(t)
	customers := repository.NewCustomerRepository(db)
	tracks := repository.NewTrackRepository(db)
	rides := repository.NewRideRepository(db)
	stops := repository.NewRideStopRepository(db)

	cust, err := customers.FindOrCreateByLineUserID("U_cust_single_point", "測試乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	newTestRide(t, rides, cust.ID, constants.RideStatusRequested)

	q := NewRideQueryService(tracks, rides)
	q.SetStops(stops)

	got, err := q.GetActiveRideByCustomer(cust.ID)
	if err != nil || got == nil {
		t.Fatalf("查詢失敗：got=%v err=%v", got, err)
	}
	if got.Stops != nil {
		t.Fatalf("單點訂單不該有 stops，得到 %v", got.Stops)
	}
}

func TestCustomerRideView_未注入stops仍可查(t *testing.T) {
	// stops 是加值資訊，不是行程本體：沒注入 repo 時查詢照樣要成功。
	db := newServiceTestDB(t)
	customers := repository.NewCustomerRepository(db)
	tracks := repository.NewTrackRepository(db)
	rides := repository.NewRideRepository(db)

	cust, err := customers.FindOrCreateByLineUserID("U_cust_no_stop_repo", "測試乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	ride := newTestRide(t, rides, cust.ID, constants.RideStatusRequested)

	q := NewRideQueryService(tracks, rides) // 刻意不 SetStops
	got, err := q.GetActiveRideByCustomer(cust.ID)
	if err != nil || got == nil || got.ID != ride.ID {
		t.Fatalf("未注入 stops repo 時查詢仍應成功：got=%v err=%v", got, err)
	}
}
