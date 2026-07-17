package constants

// 停靠點種類（N1）。值域由 DB CHECK `chk_ride_stops_kind` 一併把關。
const (
	StopKindPickup  = "pickup"  // 該位乘客的上車點
	StopKindDropoff = "dropoff" // 該位乘客的下車點
)

// 多乘客／多停靠點上限（N2 拍板 2026-07-16）：
// **5 位乘客、各自一上一下 → 最多 10 個停靠點**。
const (
	MaxRidePassengers = 5
	MaxRideStops      = MaxRidePassengers * 2
)

// IsValidStopKind 是否為合法的停靠點種類。
func IsValidStopKind(s string) bool {
	return s == StopKindPickup || s == StopKindDropoff
}
