package service

import (
	"errors"
	"sync"

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
	}
}

// MonthlyMembershipFeeCents 供月報表計算應付總公司之會費部分。
func (s *FeeSettings) MonthlyMembershipFeeCents() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.monthlyMembershipFeeCents
}

// Quote 依實際里程（公尺）計算車資、手續費、司機實得（皆為「分」）。
//
//	fare       = max(min_fare, base + round(per_km × 距離公尺 / 1000))
//	commission = round(fare × bps / 10000)
//	driver_net = fare − commission
//
// 距離為 0 時 fare 會落到 min_fare，不會算成 0；此外呼叫端（TrackingService.Complete）
// 已對軌跡稀疏/缺失做退路——以 OSRM pickup→dropoff 路線里程作地板（見 billableDistanceM），
// 故傳進來的 distanceM 通常已是「軌跡 vs 路線」的較大者。
func (s *FeeSettings) Quote(distanceM int) (fareCents, commissionCents, driverNetCents int64) {
	s.mu.RLock()
	base, perKm, minFare, bps := s.baseFareCents, s.perKmFareCents, s.minFareCents, s.commissionBps
	s.mu.RUnlock()

	if distanceM < 0 {
		distanceM = 0
	}
	fareCents = base + roundDiv(perKm*int64(distanceM), 1000)
	if fareCents < minFare {
		fareCents = minFare
	}
	commissionCents = roundDiv(fareCents*int64(bps), 10000)
	driverNetCents = fareCents - commissionCents
	return
}

// roundDiv 非負整數四捨五入除法（a >= 0, b > 0）。
func roundDiv(a, b int64) int64 {
	if b <= 0 {
		return 0
	}
	return (a + b/2) / b
}

// Update 覆寫費率設定（nil 欄位表示不變），先驗證、寫 DB，再更新快取。
func (s *FeeSettings) Update(baseFareCents, perKmFareCents, minFareCents, commissionBps, monthlyMembershipFeeCents, updatedBy *int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	next := model.FleetSettings{
		BaseFareCents:             s.baseFareCents,
		PerKmFareCents:            s.perKmFareCents,
		MinFareCents:              s.minFareCents,
		CommissionBps:             s.commissionBps,
		MonthlyMembershipFeeCents: s.monthlyMembershipFeeCents,
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
	return nil
}

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
	default:
		return nil
	}
}
