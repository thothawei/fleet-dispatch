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

// validateOptionalDropoffCoords 目的地座標為選填；若提供則 lat/lng 須成對且合法。
func validateOptionalDropoffCoords(lat, lng *float64) error {
	hasLat := lat != nil
	hasLng := lng != nil
	if hasLat != hasLng {
		return ErrInvalidCoords
	}
	if !hasLat {
		return nil
	}
	return validatePickupCoords(*lat, *lng)
}
