package service

// validatePickupCoords 檢查上車座標落在合法經緯度範圍且非 (0,0)。
func validatePickupCoords(lat, lng float64) error {
	if lat < -90 || lat > 90 || lng < -180 || lng > 180 {
		return ErrInvalidCoords
	}
	if lat == 0 && lng == 0 {
		return ErrInvalidCoords
	}
	return nil
}
