package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"line-fleet-dispatch/internal/constants"
)

// 座標點數＝path 中 `/driving/` 之後以 `;` 分隔的段數。
func coordCount(t *testing.T, path string) int {
	t.Helper()
	i := strings.Index(path, "/driving/")
	if i < 0 {
		t.Fatalf("OSRM path 不含 /driving/：%s", path)
	}
	return strings.Count(path[i:], ";") + 1
}

func ptr(f float64) *float64 { return &f }

// 單點行程：終點座標齊全 → 依 OSRM 距離算車資，total ＝ fare（未指定寵物車無清潔費）。
func TestEstimateSinglePoint(t *testing.T) {
	osrm, fake := newFakeOSRM(t, 5000) // 5km
	fees := newFeeSettingsForTest(8500, 2000, 8500, 1500, 300000)
	svc := NewEstimateService(osrm, fees)

	res, err := svc.Estimate(context.Background(), EstimateRequest{
		PickupLat:  25.033,
		PickupLng:  121.5654,
		DropoffLat: ptr(25.0517),
		DropoffLng: ptr(121.5170),
	})
	if err != nil {
		t.Fatalf("預估失敗：%v", err)
	}
	// fare = roundNtd(8500 + round(2000*5000/1000)) = 8500 + 10000 = 18500
	if res.FareCents != 18500 {
		t.Errorf("fare = %d，預期 18500", res.FareCents)
	}
	if res.CleaningFeeCents != 0 {
		t.Errorf("未指定寵物車不應有清潔費，得 %d", res.CleaningFeeCents)
	}
	if res.TotalCents != 18500 {
		t.Errorf("total = %d，預期 18500（＝fare）", res.TotalCents)
	}
	if res.DistanceM != 5000 {
		t.Errorf("distance = %d，預期 5000", res.DistanceM)
	}
	if n := coordCount(t, fake.lastPath(t)); n != 2 {
		t.Errorf("單點行程應送 2 個座標，得 %d", n)
	}
}

// 指定寵物車 → total ＝ fare + 清潔費。
func TestEstimatePetCleaningFee(t *testing.T) {
	osrm, _ := newFakeOSRM(t, 5000)
	fees := newFeeSettingsWithCleaning(8500, 2000, 8500, 1500, 2000) // 清潔費 20%
	svc := NewEstimateService(osrm, fees)

	res, err := svc.Estimate(context.Background(), EstimateRequest{
		PickupLat:           25.033,
		PickupLng:           121.5654,
		DropoffLat:          ptr(25.0517),
		DropoffLng:          ptr(121.5170),
		RequiredVehicleType: constants.VehicleTypePet,
	})
	if err != nil {
		t.Fatalf("預估失敗：%v", err)
	}
	// fare 18500；清潔費 = floorNtd(18500*2000/10000) = 3700；total = 22200
	if res.CleaningFeeCents != 3700 {
		t.Errorf("清潔費 = %d，預期 3700", res.CleaningFeeCents)
	}
	if res.TotalCents != 22200 {
		t.Errorf("total = %d，預期 22200（fare 18500 + 清潔費 3700）", res.TotalCents)
	}
}

// 多停靠點：全程所有停靠點都入路線（預估階段還沒有人被跳過）。
func TestEstimateMultiStop(t *testing.T) {
	osrm, fake := newFakeOSRM(t, 8000)
	fees := newFeeSettingsForTest(8500, 2000, 8500, 1500, 300000)
	svc := NewEstimateService(osrm, fees)

	stops := []StopInput{
		{Seq: 3, Kind: constants.StopKindDropoff, Lat: 25.05, Lng: 121.52, PassengerLabel: "A"},
		{Seq: 1, Kind: constants.StopKindPickup, Lat: 25.03, Lng: 121.56, PassengerLabel: "A"},
		{Seq: 4, Kind: constants.StopKindDropoff, Lat: 25.06, Lng: 121.51, PassengerLabel: "B"},
		{Seq: 2, Kind: constants.StopKindPickup, Lat: 25.04, Lng: 121.55, PassengerLabel: "B"},
	}
	res, err := svc.Estimate(context.Background(), EstimateRequest{Stops: stops})
	if err != nil {
		t.Fatalf("預估失敗：%v", err)
	}
	// fare = roundNtd(8500 + round(2000*8000/1000)) = 8500 + 16000 = 24500
	if res.FareCents != 24500 {
		t.Errorf("fare = %d，預期 24500", res.FareCents)
	}
	if n := coordCount(t, fake.lastPath(t)); n != 4 {
		t.Errorf("四停行程應送 4 個座標，得 %d", n)
	}
}

// 缺目的地座標 → 400（ErrInvalidCoords）：沒終點無法算路線。
func TestEstimateMissingDropoffCoords(t *testing.T) {
	osrm, _ := newFakeOSRM(t, 5000)
	fees := newFeeSettingsForTest(8500, 2000, 8500, 1500, 300000)
	svc := NewEstimateService(osrm, fees)

	_, err := svc.Estimate(context.Background(), EstimateRequest{
		PickupLat: 25.033,
		PickupLng: 121.5654,
		// 無 dropoff 座標
	})
	if !errors.Is(err, ErrInvalidCoords) {
		t.Errorf("缺目的地座標應回 ErrInvalidCoords，得 %v", err)
	}
}

// 車種非白名單 → ErrInvalidVehicleType。
func TestEstimateInvalidVehicleType(t *testing.T) {
	osrm, _ := newFakeOSRM(t, 5000)
	fees := newFeeSettingsForTest(8500, 2000, 8500, 1500, 300000)
	svc := NewEstimateService(osrm, fees)

	_, err := svc.Estimate(context.Background(), EstimateRequest{
		PickupLat:           25.033,
		PickupLng:           121.5654,
		DropoffLat:          ptr(25.0517),
		DropoffLng:          ptr(121.5170),
		RequiredVehicleType: "spaceship",
	})
	if !errors.Is(err, ErrInvalidVehicleType) {
		t.Errorf("非法車種應回 ErrInvalidVehicleType，得 %v", err)
	}
}

// 距離極短 → fare 落到 min_fare 下限（不會算成 0 或低於下限）。
func TestEstimateMinFareFloor(t *testing.T) {
	osrm, _ := newFakeOSRM(t, 1000) // 1km
	fees := newFeeSettingsForTest(5000, 2000, 12000, 1000, 0)
	svc := NewEstimateService(osrm, fees)

	res, err := svc.Estimate(context.Background(), EstimateRequest{
		PickupLat:  25.033,
		PickupLng:  121.5654,
		DropoffLat: ptr(25.0517),
		DropoffLng: ptr(121.5170),
	})
	if err != nil {
		t.Fatalf("預估失敗：%v", err)
	}
	// fare 原算 = 5000 + round(2000*1000/1000) = 7000 < 12000 → 夾到 min_fare 12000
	if res.FareCents != 12000 {
		t.Errorf("fare = %d，預期夾到 min_fare 12000", res.FareCents)
	}
}

// 停靠點填錯（少一半的乘客）→ 驗證錯誤，不放行。
func TestEstimateUnpairedStops(t *testing.T) {
	osrm, _ := newFakeOSRM(t, 5000)
	fees := newFeeSettingsForTest(8500, 2000, 8500, 1500, 300000)
	svc := NewEstimateService(osrm, fees)

	// A 只有上車、沒有下車 → 無法執行的行程。
	stops := []StopInput{
		{Seq: 1, Kind: constants.StopKindPickup, Lat: 25.03, Lng: 121.56, PassengerLabel: "A"},
	}
	_, err := svc.Estimate(context.Background(), EstimateRequest{Stops: stops})
	if err == nil {
		t.Fatal("未成對的停靠點應回錯誤，卻成功")
	}
	if !errors.Is(err, ErrUnpairedStop) {
		t.Errorf("預期 ErrUnpairedStop，得 %v", err)
	}
}

// 服務未就緒（osrm／fees 缺）→ ErrEstimateUnavailable，不是 panic。
func TestEstimateUnavailableWhenNotConfigured(t *testing.T) {
	fees := newFeeSettingsForTest(8500, 2000, 8500, 1500, 300000)
	svc := NewEstimateService(nil, fees) // 無 OSRM
	_, err := svc.Estimate(context.Background(), EstimateRequest{
		PickupLat: 25.033, PickupLng: 121.5654,
		DropoffLat: ptr(25.0517), DropoffLng: ptr(121.5170),
	})
	if !errors.Is(err, ErrEstimateUnavailable) {
		t.Errorf("無 OSRM 應回 ErrEstimateUnavailable，得 %v", err)
	}
}
