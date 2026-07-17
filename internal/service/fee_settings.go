package service

import (
	"errors"
	"sync"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/model"
	"line-fleet-dispatch/internal/repository"
)

var ErrInvalidFeeSettings = errors.New("費率設定超出允許範圍")

// FeeSettings 費率設定的執行期快取（啟動時自 DB 載入單列）。
// 與 DispatchSettings 不同：費率**持久化於 DB**，PUT 會同時寫 DB 與更新快取，
// 重啟不會還原——避免重啟後新行程用到錯誤費率而算錯帳。
// 金額一律以「分」計；手續費以 bps 計（1500 = 15%），全程整數運算避免浮點誤差。
type FeeSettings struct {
	mu   sync.RWMutex
	repo *repository.FeeSettingsRepository

	baseFareCents             int64
	perKmFareCents            int64
	minFareCents              int64
	commissionBps             int
	monthlyMembershipFeeCents int64
	lostItemFeeBps            int
	petCleaningFeeBps         int
}

// NewFeeSettings 自 DB 載入費率設定到記憶體快取。
func NewFeeSettings(repo *repository.FeeSettingsRepository) (*FeeSettings, error) {
	row, err := repo.Get()
	if err != nil {
		return nil, err
	}
	return &FeeSettings{
		repo:                      repo,
		baseFareCents:             row.BaseFareCents,
		perKmFareCents:            row.PerKmFareCents,
		minFareCents:              row.MinFareCents,
		commissionBps:             row.CommissionBps,
		monthlyMembershipFeeCents: row.MonthlyMembershipFeeCents,
		lostItemFeeBps:            row.LostItemFeeBps,
		petCleaningFeeBps:         row.PetCleaningFeeBps,
	}, nil
}

// JSON 供 admin GET 回傳（欄位皆為「分」/bps）。
func (s *FeeSettings) JSON() map[string]int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]int64{
		"base_fare_cents":              s.baseFareCents,
		"per_km_fare_cents":            s.perKmFareCents,
		"min_fare_cents":               s.minFareCents,
		"commission_bps":               int64(s.commissionBps),
		"monthly_membership_fee_cents": s.monthlyMembershipFeeCents,
		"lost_item_fee_bps":            int64(s.lostItemFeeBps),
		"pet_cleaning_fee_bps":         int64(s.petCleaningFeeBps),
	}
}

// CustomerJSON 乘客可讀的費率（P5）——**白名單輸出，不是把 JSON() 過濾**。
// 另建一份的理由：過濾式寫法在日後新增內部費率欄位時，很容易忘了把它加進黑名單而外洩；
// 白名單預設什麼都不給，新欄位不會自己跑出來。
// 絕不可包含 commission_bps（手續費）／monthly_membership_fee_cents（月會費）等內部費率。
func (s *FeeSettings) CustomerJSON() map[string]int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]int64{
		// 乘客選寵物用車時要當場看到加價（P5）。
		"pet_cleaning_fee_bps": int64(s.petCleaningFeeBps),
	}
}

// PetCleaningFeeBps 寵物車清潔費%（bps）。
func (s *FeeSettings) PetCleaningFeeBps() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.petCleaningFeeBps
}

// LostItemFeeBps 遺失物處理費%（bps），供協尋單建立時快照。
func (s *FeeSettings) LostItemFeeBps() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lostItemFeeBps
}

// MonthlyMembershipFeeCents 供月報表計算應付總公司之會費部分。
func (s *FeeSettings) MonthlyMembershipFeeCents() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.monthlyMembershipFeeCents
}

// RideQuote 一趟行程的定格計費（皆為「分」，一律落在整數元）。
type RideQuote struct {
	FareCents        int64
	CommissionCents  int64
	CleaningFeeCents int64
	DriverNetCents   int64
}

// Quote 依實際里程（公尺）與**乘客指定的車種**計算定格計費。
//
//	fare         = max(min_fare, roundNtd(base + round(per_km × 距離公尺 / 1000)))  // 車資四捨五入到整數元
//	cleaning_fee = floorNtd(fare × pet_cleaning_fee_bps / 10000)  // 僅 requiredVehicleType=='pet'；捨去到整數元（利乘客）
//	commission   = floorNtd(fare × commission_bps / 10000)        // **基準只有 fare**，清潔費不計入抽成
//	driver_net   = fare − commission + cleaning_fee               // 清潔費全額歸司機（清潔成本補償，非營收）
//
// requiredVehicleType 取自 `rides.required_vehicle_type`（乘客要什麼車），
// **不可傳司機的 vehicle_type**——乘客沒指定寵物車、卻剛好被派到寵物車司機時不得加收，
// 那位乘客沒有要求寵物服務（O6 拍板 2026-07-16）。
//
// 台幣無小數：所有金額都是整數元（分為 100 的倍數），支付/報表不會出現不可支付的小數
// （min_fare 由設定輸入，本身已是整數元）。距離為 0 時 fare 會落到 min_fare，不會算成 0；
// 呼叫端（TrackingService.Complete）已對軌跡稀疏/缺失做退路——以 OSRM pickup→dropoff 路線里程
// 作地板（見 billableDistanceM），故傳進來的 distanceM 通常已是「軌跡 vs 路線」的較大者。
func (s *FeeSettings) Quote(distanceM int, requiredVehicleType string) RideQuote {
	s.mu.RLock()
	base, perKm, minFare := s.baseFareCents, s.perKmFareCents, s.minFareCents
	bps, cleaningBps := s.commissionBps, s.petCleaningFeeBps
	s.mu.RUnlock()

	if distanceM < 0 {
		distanceM = 0
	}
	q := RideQuote{}
	q.FareCents = roundNtd(base + roundDiv(perKm*int64(distanceM), 1000))
	if q.FareCents < minFare {
		q.FareCents = minFare
	}
	if requiredVehicleType == constants.VehicleTypePet {
		q.CleaningFeeCents = floorNtd(q.FareCents * int64(cleaningBps) / 10000)
	}
	q.CommissionCents = floorNtd(q.FareCents * int64(bps) / 10000)
	q.DriverNetCents = q.FareCents - q.CommissionCents + q.CleaningFeeCents
	return q
}

// roundDiv 非負整數四捨五入除法（a >= 0, b > 0）。
func roundDiv(a, b int64) int64 {
	if b <= 0 {
		return 0
	}
	return (a + b/2) / b
}

// 台幣無小數：金額一律落在整數元（分為 100 的倍數）。
// roundNtd 把「分」四捨五入到整數元；floorNtd 無條件捨去到整數元（兩者皆假設 cents >= 0）。
func roundNtd(cents int64) int64 {
	if cents < 0 {
		return 0
	}
	return roundDiv(cents, 100) * 100
}

func floorNtd(cents int64) int64 {
	if cents < 0 {
		return 0
	}
	return (cents / 100) * 100
}

// Update 覆寫費率設定（nil 欄位表示不變），先驗證、寫 DB，再更新快取。
func (s *FeeSettings) Update(baseFareCents, perKmFareCents, minFareCents, commissionBps, monthlyMembershipFeeCents, lostItemFeeBps, petCleaningFeeBps, updatedBy *int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	next := model.FleetSettings{
		BaseFareCents:             s.baseFareCents,
		PerKmFareCents:            s.perKmFareCents,
		MinFareCents:              s.minFareCents,
		CommissionBps:             s.commissionBps,
		MonthlyMembershipFeeCents: s.monthlyMembershipFeeCents,
		LostItemFeeBps:            s.lostItemFeeBps,
		PetCleaningFeeBps:         s.petCleaningFeeBps,
		UpdatedBy:                 updatedBy,
	}
	if baseFareCents != nil {
		next.BaseFareCents = *baseFareCents
	}
	if perKmFareCents != nil {
		next.PerKmFareCents = *perKmFareCents
	}
	if minFareCents != nil {
		next.MinFareCents = *minFareCents
	}
	if commissionBps != nil {
		next.CommissionBps = int(*commissionBps)
	}
	if monthlyMembershipFeeCents != nil {
		next.MonthlyMembershipFeeCents = *monthlyMembershipFeeCents
	}
	if lostItemFeeBps != nil {
		next.LostItemFeeBps = int(*lostItemFeeBps)
	}
	if petCleaningFeeBps != nil {
		next.PetCleaningFeeBps = int(*petCleaningFeeBps)
	}

	if err := validateFeeSettings(next); err != nil {
		return err
	}
	if err := s.repo.Update(&next); err != nil {
		return err
	}
	s.baseFareCents = next.BaseFareCents
	s.perKmFareCents = next.PerKmFareCents
	s.minFareCents = next.MinFareCents
	s.commissionBps = next.CommissionBps
	s.monthlyMembershipFeeCents = next.MonthlyMembershipFeeCents
	s.lostItemFeeBps = next.LostItemFeeBps
	s.petCleaningFeeBps = next.PetCleaningFeeBps
	return nil
}

// MaxPetCleaningFeeBps 寵物車清潔費硬上限 30%（O6 拍板 2026-07-16）。
// 同一上限也寫在 DB CHECK（chk_fleet_settings_pet_cleaning_fee_bps）：
// 這裡擋是為了回 400 給後台，DB 擋是最後防線（這是乘客實際被收的錢，寫錯就是超收）。
const MaxPetCleaningFeeBps = 3000

func validateFeeSettings(s model.FleetSettings) error {
	const maxFare = int64(1_000_000)         // 單筆金額上限 1 萬元（分）
	const maxMembership = int64(100_000_000) // 月會費上限 100 萬元（分）
	switch {
	case s.BaseFareCents < 0 || s.BaseFareCents > maxFare:
		return ErrInvalidFeeSettings
	case s.PerKmFareCents < 0 || s.PerKmFareCents > maxFare:
		return ErrInvalidFeeSettings
	case s.MinFareCents < 0 || s.MinFareCents > maxFare:
		return ErrInvalidFeeSettings
	case s.CommissionBps < 0 || s.CommissionBps > 10000:
		return ErrInvalidFeeSettings
	case s.MonthlyMembershipFeeCents < 0 || s.MonthlyMembershipFeeCents > maxMembership:
		return ErrInvalidFeeSettings
	case s.LostItemFeeBps < 0 || s.LostItemFeeBps > 10000:
		return ErrInvalidFeeSettings
	case s.PetCleaningFeeBps < 0 || s.PetCleaningFeeBps > MaxPetCleaningFeeBps:
		return ErrInvalidFeeSettings
	default:
		return nil
	}
}
