package service

import (
	"errors"
	"sync"
)

var ErrInvalidDispatchSettings = errors.New("派單參數超出允許範圍")

// DispatchSettings 執行期可調的派單參數（啟動時自 env 載入，admin PUT 可覆寫；重啟還原 env 預設）。
type DispatchSettings struct {
	mu sync.RWMutex

	RadiusM         int
	MaxDrivers      int
	OfferTimeoutSec int
	MaxAttempts     int
	RateLimitPerMin int
}

// NewDispatchSettings 自設定檔建立執行期派單參數。
func NewDispatchSettings(radiusM, maxDrivers, offerTimeoutSec, maxAttempts, rateLimitPerMin int) *DispatchSettings {
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	if rateLimitPerMin < 1 {
		rateLimitPerMin = 5
	}
	return &DispatchSettings{
		RadiusM:         radiusM,
		MaxDrivers:      maxDrivers,
		OfferTimeoutSec: offerTimeoutSec,
		MaxAttempts:     maxAttempts,
		RateLimitPerMin: rateLimitPerMin,
	}
}

// Snapshot 回傳目前參數副本（供派單/叫車限流讀取）。
func (s *DispatchSettings) Snapshot() (radiusM, maxDrivers, offerTimeoutSec, maxAttempts, rateLimitPerMin int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.RadiusM, s.MaxDrivers, s.OfferTimeoutSec, s.MaxAttempts, s.RateLimitPerMin
}

// JSON 供 admin GET 回傳。
func (s *DispatchSettings) JSON() map[string]int {
	radiusM, maxDrivers, offerTimeoutSec, maxAttempts, rateLimitPerMin := s.Snapshot()
	return map[string]int{
		"radius_m":           radiusM,
		"max_drivers":        maxDrivers,
		"offer_timeout_sec":  offerTimeoutSec,
		"max_attempts":       maxAttempts,
		"rate_limit_per_min": rateLimitPerMin,
	}
}

// Update 覆寫派單參數（nil 欄位表示不變）。
func (s *DispatchSettings) Update(radiusM, maxDrivers, offerTimeoutSec, maxAttempts, rateLimitPerMin *int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	next := struct {
		radius, maxDrv, timeout, attempts, rate int
	}{
		s.RadiusM, s.MaxDrivers, s.OfferTimeoutSec, s.MaxAttempts, s.RateLimitPerMin,
	}
	if radiusM != nil {
		next.radius = *radiusM
	}
	if maxDrivers != nil {
		next.maxDrv = *maxDrivers
	}
	if offerTimeoutSec != nil {
		next.timeout = *offerTimeoutSec
	}
	if maxAttempts != nil {
		next.attempts = *maxAttempts
	}
	if rateLimitPerMin != nil {
		next.rate = *rateLimitPerMin
	}
	if err := validateDispatchSettings(next.radius, next.maxDrv, next.timeout, next.attempts, next.rate); err != nil {
		return err
	}
	s.RadiusM, s.MaxDrivers, s.OfferTimeoutSec, s.MaxAttempts, s.RateLimitPerMin = next.radius, next.maxDrv, next.timeout, next.attempts, next.rate
	return nil
}

func validateDispatchSettings(radiusM, maxDrivers, offerTimeoutSec, maxAttempts, rateLimitPerMin int) error {
	switch {
	case radiusM < 100 || radiusM > 50000:
		return ErrInvalidDispatchSettings
	case maxDrivers < 1 || maxDrivers > 20:
		return ErrInvalidDispatchSettings
	case offerTimeoutSec < 5 || offerTimeoutSec > 120:
		return ErrInvalidDispatchSettings
	case maxAttempts < 1 || maxAttempts > 10:
		return ErrInvalidDispatchSettings
	case rateLimitPerMin < 1 || rateLimitPerMin > 30:
		return ErrInvalidDispatchSettings
	default:
		return nil
	}
}
