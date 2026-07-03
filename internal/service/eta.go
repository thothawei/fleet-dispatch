package service

import (
	"context"
	"fmt"

	osrmclient "line-fleet-dispatch/internal/osrm"
)

// ETAService 計算接客 ETA
type ETAService struct {
	osrm *osrmclient.Client
}

func NewETAService(osrm *osrmclient.Client) *ETAService {
	return &ETAService{osrm: osrm}
}

func (s *ETAService) PickupETA(ctx context.Context, driverLat, driverLng, pickupLat, pickupLng float64) (sec int, distanceM int) {
	sec, distanceM, _ = s.osrm.RouteDuration(ctx, driverLat, driverLng, pickupLat, pickupLng)
	return sec, distanceM
}

func FormatETAMessage(distanceM, etaSec int) string {
	minutes := etaSec / 60
	if minutes < 1 {
		minutes = 1
	}
	km := float64(distanceM) / 1000
	return fmt.Sprintf("司機距您 %.1f 公里，約 %d 分鐘抵達", km, minutes)
}
