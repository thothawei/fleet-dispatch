package service

import (
	"errors"
	"sort"

	"line-fleet-dispatch/internal/constants"
)

// 停靠點驗證錯誤（N2）。分開成多個錯誤而非一個籠統的 ErrInvalidStops——
// 乘客填錯時要知道錯在哪，handler 一律對應 400。
var (
	ErrTooManyStops        = errors.New("停靠點數量超過上限")
	ErrTooManyPassengers   = errors.New("乘客人數超過上限")
	ErrInvalidStopKind     = errors.New("停靠點種類無效")
	ErrDuplicateStopSeq    = errors.New("停靠點順序重複")
	ErrUnpairedStop        = errors.New("每位乘客都必須有上車點與下車點")
	ErrDropoffBeforePickup = errors.New("乘客的下車點必須排在其上車點之後")
	ErrMissingPassenger    = errors.New("多停靠點行程必須指定乘客標籤")
	// ErrStopsUnavailable 收到 stops 但未注入停靠點 repo（設定錯誤，非乘客的錯）。
	ErrStopsUnavailable = errors.New("多停靠點行程未啟用")
)

// StopInput 建單時傳入的單一停靠點（N3）。
type StopInput struct {
	Seq            int
	Kind           string
	Lat, Lng       float64
	Address        string
	PassengerLabel string
}

// validateStops 檢查多停靠點行程的完整性（N2）。空 slice ＝ 傳統單點訂單，直接放行。
//
// 規則（2026-07-16 拍板：5 位乘客、各自上下車 → 最多 10 個停靠點）：
//  1. 停靠點數 ≤ 10、乘客數 ≤ 5
//  2. kind 必須是 pickup／dropoff；座標合法
//  3. seq 不得重複（seq ＝「第幾站」，重複的話「下一站是誰」沒有答案）
//  4. 每位乘客必須**成對**出現 pickup ＋ dropoff
//  5. 同一位乘客的 dropoff.seq 必須 > pickup.seq——不能先下車再上車
//
// 這些是「司機能不能執行這趟行程」的前提，不是美觀問題：
// 少一半的乘客、或叫司機先送再接，都是無法執行的行程。
func validateStops(stops []StopInput) error {
	if len(stops) == 0 {
		return nil // 單點訂單，維持現行行為
	}
	if len(stops) > constants.MaxRideStops {
		return ErrTooManyStops
	}

	seqSeen := make(map[int]bool, len(stops))
	type pair struct{ pickupSeq, dropoffSeq int }
	byPassenger := make(map[string]*pair)

	for _, s := range stops {
		if !constants.IsValidStopKind(s.Kind) {
			return ErrInvalidStopKind
		}
		if err := validatePickupCoords(s.Lat, s.Lng); err != nil {
			return err
		}
		if s.Seq < 1 {
			return ErrDuplicateStopSeq // seq 從 1 起算；0 或負數視同無效順序
		}
		if seqSeen[s.Seq] {
			return ErrDuplicateStopSeq
		}
		seqSeen[s.Seq] = true

		// 多停靠點時必須分得出「這站是誰」，否則司機不知道在等誰。
		if s.PassengerLabel == "" {
			return ErrMissingPassenger
		}
		p, ok := byPassenger[s.PassengerLabel]
		if !ok {
			p = &pair{pickupSeq: -1, dropoffSeq: -1}
			byPassenger[s.PassengerLabel] = p
		}
		switch s.Kind {
		case constants.StopKindPickup:
			if p.pickupSeq != -1 {
				return ErrUnpairedStop // 同一位乘客兩個上車點
			}
			p.pickupSeq = s.Seq
		case constants.StopKindDropoff:
			if p.dropoffSeq != -1 {
				return ErrUnpairedStop // 同一位乘客兩個下車點
			}
			p.dropoffSeq = s.Seq
		}
	}

	if len(byPassenger) > constants.MaxRidePassengers {
		return ErrTooManyPassengers
	}
	for _, p := range byPassenger {
		if p.pickupSeq == -1 || p.dropoffSeq == -1 {
			return ErrUnpairedStop
		}
		if p.dropoffSeq <= p.pickupSeq {
			return ErrDropoffBeforePickup
		}
	}
	return nil
}

// sortStopsBySeq 回傳依 seq 由小到大排序的副本（不改動呼叫端的 slice）。
// 建單時客戶端不保證照順序送，但落庫與後續讀取都以 seq 為準。
func sortStopsBySeq(stops []StopInput) []StopInput {
	out := make([]StopInput, len(stops))
	copy(out, stops)
	sort.Slice(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out
}

// firstPickup 最先的上車點——寫進 rides.pickup_point，派單據此找最近司機（N3 相容性）。
func firstPickup(sorted []StopInput) (StopInput, bool) {
	for _, s := range sorted {
		if s.Kind == constants.StopKindPickup {
			return s, true
		}
	}
	return StopInput{}, false
}

// finalDropoff 最終目的地＝seq 最大的 dropoff——寫進 rides.dropoff_point。
// 既有的地圖、司機導航、F3 路線里程退路都讀這個欄位，不必為多停靠點全面改寫。
func finalDropoff(sorted []StopInput) (StopInput, bool) {
	for i := len(sorted) - 1; i >= 0; i-- {
		if sorted[i].Kind == constants.StopKindDropoff {
			return sorted[i], true
		}
	}
	return StopInput{}, false
}
