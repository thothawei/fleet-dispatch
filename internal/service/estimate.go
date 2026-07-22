package service

import (
	"context"
	"errors"

	"line-fleet-dispatch/internal/constants"
	osrmclient "line-fleet-dispatch/internal/osrm"
)

// ErrEstimateUnavailable 未注入 OSRM／費率設定時無法計算預估（設定問題，非乘客輸入錯）。
var ErrEstimateUnavailable = errors.New("車資預估暫時無法使用")

// EstimateService 乘客建單**前**的車資預估（懸而未決 #1）。
//
// 純唯讀：不建單、不寫 DB、不派單——單純「以全程規劃路線試算車資」，讓乘客在多停靠點行程
// 排了一堆繞路之前就先看到大概金額，而不是到行程結束才知道（N5「先搭後知價」的解法）。
//
// 與完成時計費（TrackingService.Complete）共用同一份 FeeSettings.Quote，確保預估與實收落在
// **同一套計費規則**上；兩者的差異只來自「規劃路線 vs 實際行駛路線」（繞路、跳過站、GPS 軌跡），
// 這也是為什麼要對乘客標明「預估、實際依行駛路線可能不同」。
type EstimateService struct {
	osrm *osrmclient.Client
	fees *FeeSettings
}

// NewEstimateService 建立預估服務。osrm／fees 皆必要，缺任一則 Estimate 回 ErrEstimateUnavailable。
func NewEstimateService(osrm *osrmclient.Client, fees *FeeSettings) *EstimateService {
	return &EstimateService{osrm: osrm, fees: fees}
}

// EstimateRequest 預估輸入。座標／車種／停靠點與 CustomerCreateRequest 同形狀，
// 但**目的地座標為必填**——沒有終點座標就算不出路線，也就無法預估
// （建單時 dropoff 可只給地址字串，那條路徑無座標可路由，本就不該顯示預估）。
type EstimateRequest struct {
	PickupLat, PickupLng   float64
	DropoffLat, DropoffLng *float64
	// RequiredVehicleType 選填：''＝不指定。指定寵物車（pet）時預估會含清潔費。
	RequiredVehicleType string
	// Stops 選填（N3）：多乘客／多停靠點。有給時起訖由 stops 推導，忽略上面的 pickup／dropoff。
	Stops []StopInput
}

// EstimateResult 預估結果（金額皆為「分」，一律整數元）。
// **只回乘客該知道的欄位**：車資、清潔費、合計、距離、時間——
// 不回手續費／實得等內部費率（比照 FeeSettings.CustomerJSON 的白名單方針）。
type EstimateResult struct {
	DistanceM        int
	DurationSec      int
	FareCents        int64
	CleaningFeeCents int64
	TotalCents       int64
}

// Estimate 以全程規劃路線（起點 → 各停靠點 → 最終目的地，**含繞路**）試算車資。
//
// 距離來自 OSRM RouteVia（與 N5 計費同一支多點路線 API）；OSRM 不可用時 RouteVia 內部
// 退回逐段 haversine，因此預估**永遠算得出一個數**（近似值），不會因路網引擎暫時掛掉就報錯——
// 這正是預估該有的行為，寧可給近似也不要讓乘客卡住。
func (s *EstimateService) Estimate(ctx context.Context, req EstimateRequest) (EstimateResult, error) {
	if s == nil || s.osrm == nil || s.fees == nil {
		return EstimateResult{}, ErrEstimateUnavailable
	}
	// 車種選填；有給就必須是白名單 code（與建單同一條件，錯誤在此變 400 而非讓後續流程收到髒值）。
	if req.RequiredVehicleType != "" && !constants.IsValidVehicleType(req.RequiredVehicleType) {
		return EstimateResult{}, ErrInvalidVehicleType
	}
	points, err := s.routePoints(&req)
	if err != nil {
		return EstimateResult{}, err
	}
	durationSec, distanceM, err := s.osrm.RouteVia(ctx, points)
	if err != nil {
		// points 保證 ≥2，RouteVia 只有點數不足才回錯；理論上到不了這裡，防禦性處理。
		return EstimateResult{}, err
	}
	q := s.fees.Quote(distanceM, req.RequiredVehicleType)
	return EstimateResult{
		DistanceM:        distanceM,
		DurationSec:      durationSec,
		FareCents:        q.FareCents,
		CleaningFeeCents: q.CleaningFeeCents,
		TotalCents:       q.FareCents + q.CleaningFeeCents,
	}, nil
}

// routePoints 依輸入推導「起點 → 各停靠點 → 終點」的有序座標，套用與建單相同的驗證。
//
// 多停靠點時**所有停靠點都入路線**——預估階段還沒有人被跳過（跳過是司機途中的動作），
// 這與 billableStopPoints（排除已跳過者）刻意不同：那是實際計費，這是全程預估。
func (s *EstimateService) routePoints(req *EstimateRequest) ([]osrmclient.Point, error) {
	if len(req.Stops) > 0 {
		if err := validateStops(req.Stops); err != nil {
			return nil, err
		}
		sorted := sortStopsBySeq(req.Stops)
		if _, ok := firstPickup(sorted); !ok {
			return nil, ErrUnpairedStop
		}
		if _, ok := finalDropoff(sorted); !ok {
			return nil, ErrUnpairedStop
		}
		pts := make([]osrmclient.Point, 0, len(sorted))
		for _, st := range sorted {
			pts = append(pts, osrmclient.Point{Lat: st.Lat, Lng: st.Lng})
		}
		return pts, nil
	}
	// 單點訂單：起點合法 + 終點座標**必填**（沒座標無法路由）。
	if err := validatePickupCoords(req.PickupLat, req.PickupLng); err != nil {
		return nil, err
	}
	if req.DropoffLat == nil || req.DropoffLng == nil {
		return nil, ErrInvalidCoords
	}
	if err := validatePickupCoords(*req.DropoffLat, *req.DropoffLng); err != nil {
		return nil, err
	}
	return []osrmclient.Point{
		{Lat: req.PickupLat, Lng: req.PickupLng},
		{Lat: *req.DropoffLat, Lng: *req.DropoffLng},
	}, nil
}
