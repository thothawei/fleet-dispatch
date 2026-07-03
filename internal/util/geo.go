package util

import (
	"fmt"
	"math"
)

const earthRadiusM = 6371000

// HaversineM 兩點直線距離（公尺）
func HaversineM(lat1, lng1, lat2, lng2 float64) float64 {
	rad := math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLng := (lng2 - lng1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLng/2)*math.Sin(dLng/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadiusM * c
}

// EstimateETASec 直線距離估 ETA（秒），假設平均 30km/h
func EstimateETASec(distanceM float64) int {
	speedMPS := 30.0 * 1000 / 3600
	if speedMPS <= 0 {
		return 0
	}
	return int(distanceM / speedMPS)
}

// GoogleMapsNavURL 司機導航 deep link
func GoogleMapsNavURL(lat, lng float64) string {
	return fmt.Sprintf("https://www.google.com/maps/dir/?api=1&destination=%f,%f&travelmode=driving", lat, lng)
}
