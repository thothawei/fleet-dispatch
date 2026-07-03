package repository

import (
	"errors"
	"time"

	"gorm.io/gorm"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/model"
)

type CustomerRepository struct {
	db *gorm.DB
}

func NewCustomerRepository(db *gorm.DB) *CustomerRepository {
	return &CustomerRepository{db: db}
}

func (r *CustomerRepository) FindOrCreateByLineUserID(lineUserID, displayName string) (*model.Customer, error) {
	var customer model.Customer
	err := r.db.Where("line_user_id = ?", lineUserID).First(&customer).Error
	if err == nil {
		return &customer, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	now := time.Now()
	customer = model.Customer{
		LineUserID: lineUserID,
		Name:       displayName,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := r.db.Create(&customer).Error; err != nil {
		return nil, err
	}
	return &customer, nil
}

type RideRepository struct {
	db *gorm.DB
}

func NewRideRepository(db *gorm.DB) *RideRepository {
	return &RideRepository{db: db}
}

func (r *RideRepository) Create(ride *model.Ride) error {
	result := r.db.Exec(`
		INSERT INTO rides (
			customer_id, status, pickup_point, pickup_address,
			requested_at, created_at, updated_at
		) VALUES (
			?, ?,
			ST_SetSRID(ST_MakePoint(?, ?), 4326)::geography,
			?, ?, ?, ?
		)
	`,
		ride.CustomerID,
		ride.Status,
		ride.PickupPoint.Lng,
		ride.PickupPoint.Lat,
		ride.PickupAddress,
		ride.RequestedAt,
		ride.CreatedAt,
		ride.UpdatedAt,
	)
	if result.Error != nil {
		return result.Error
	}
	return r.db.Raw(`
		SELECT id FROM rides
		WHERE customer_id = ? AND requested_at = ?
		ORDER BY id DESC LIMIT 1
	`, ride.CustomerID, ride.RequestedAt).Scan(&ride.ID).Error
}

type DriverRepository struct {
	db *gorm.DB
}

func NewDriverRepository(db *gorm.DB) *DriverRepository {
	return &DriverRepository{db: db}
}

func (r *DriverRepository) FindByLineUserID(lineUserID string) (*model.Driver, error) {
	var d model.Driver
	err := r.db.Where("line_user_id = ?", lineUserID).First(&d).Error
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *DriverRepository) FindByID(id int64) (*model.Driver, error) {
	var d model.Driver
	err := r.db.First(&d, id).Error
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *DriverRepository) FindOrCreate(lineUserID, name string) (*model.Driver, error) {
	d, err := r.FindByLineUserID(lineUserID)
	if err == nil {
		return d, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	now := time.Now()
	d = &model.Driver{
		LineUserID: lineUserID,
		Name:       name,
		Status:     constants.DriverStatusIdle,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := r.db.Create(d).Error; err != nil {
		return nil, err
	}
	return d, nil
}

func (r *DriverRepository) CreateSimulated(name string, lineUserID string) (*model.Driver, error) {
	now := time.Now()
	d := &model.Driver{
		LineUserID: lineUserID,
		Name:       name,
		Status:     constants.DriverStatusIdle,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := r.db.Create(d).Error; err != nil {
		return nil, err
	}
	return d, nil
}

func (r *DriverRepository) UpdateStatus(id int64, status int16) error {
	return r.db.Model(&model.Driver{}).Where("id = ?", id).Updates(map[string]interface{}{
		"status":     status,
		"updated_at": time.Now(),
	}).Error
}

func (r *DriverRepository) ListIdle() ([]model.Driver, error) {
	var drivers []model.Driver
	err := r.db.Where("status = ?", constants.DriverStatusIdle).Find(&drivers).Error
	return drivers, err
}

func (r *RideRepository) FindActiveByDriver(driverID int64) (*model.Ride, error) {
	var ride model.Ride
	err := r.db.Where("driver_id = ? AND status IN ?", driverID,
		[]int16{constants.RideStatusAccepted, constants.RideStatusPickedUp},
	).Order("id DESC").First(&ride).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ride, nil
}

func (r *RideRepository) GetByID(id int64) (*model.Ride, error) {
	var ride model.Ride
	err := r.db.First(&ride, id).Error
	if err != nil {
		return nil, err
	}
	return &ride, nil
}

func (r *RideRepository) GetPickupCoords(id int64) (lat, lng float64, err error) {
	var result struct {
		Lat float64
		Lng float64
	}
	err = r.db.Raw(`
		SELECT ST_Y(pickup_point::geometry) AS lat, ST_X(pickup_point::geometry) AS lng
		FROM rides WHERE id = ?
	`, id).Scan(&result).Error
	return result.Lat, result.Lng, err
}

func (r *RideRepository) UpdateStatus(id int64, status int16) error {
	return r.db.Model(&model.Ride{}).Where("id = ?", id).Updates(map[string]interface{}{
		"status":     status,
		"updated_at": time.Now(),
	}).Error
}

func (r *RideRepository) AcceptRide(id, driverID int64, etaSec int) error {
	now := time.Now()
	return r.db.Model(&model.Ride{}).Where("id = ? AND status IN ?", id,
		[]int16{constants.RideStatusRequested, constants.RideStatusAssigned},
	).Updates(map[string]interface{}{
		"driver_id":      driverID,
		"status":         constants.RideStatusAccepted,
		"accepted_at":    now,
		"eta_pickup_sec": etaSec,
		"updated_at":     now,
	}).Error
}

func (r *RideRepository) MarkPickedUp(id int64) error {
	now := time.Now()
	return r.db.Model(&model.Ride{}).Where("id = ? AND status = ?", id, constants.RideStatusAccepted).
		Updates(map[string]interface{}{
			"status":       constants.RideStatusPickedUp,
			"picked_up_at": now,
			"updated_at":   now,
		}).Error
}

func (r *RideRepository) CompleteRide(id int64, distanceM int) error {
	now := time.Now()
	return r.db.Model(&model.Ride{}).Where("id = ? AND status = ?", id, constants.RideStatusPickedUp).
		Updates(map[string]interface{}{
			"status":       constants.RideStatusCompleted,
			"completed_at": now,
			"distance_m":   distanceM,
			"updated_at":   now,
		}).Error
}

func (r *RideRepository) GetCustomerLineUserID(rideID int64) (string, error) {
	var lineUserID string
	err := r.db.Raw(`
		SELECT c.line_user_id FROM customers c
		JOIN rides r ON r.customer_id = c.id
		WHERE r.id = ?
	`, rideID).Scan(&lineUserID).Error
	return lineUserID, err
}

type TrackRepository struct {
	db *gorm.DB
}

func NewTrackRepository(db *gorm.DB) *TrackRepository {
	return &TrackRepository{db: db}
}

func (r *TrackRepository) Insert(rideID, driverID int64, lat, lng float64) error {
	return r.db.Exec(`
		INSERT INTO ride_tracks (ride_id, driver_id, location, recorded_at)
		VALUES (?, ?, ST_SetSRID(ST_MakePoint(?, ?), 4326)::geography, NOW())
	`, rideID, driverID, lng, lat).Error
}

func (r *TrackRepository) GeoJSON(rideID int64) (string, error) {
	var geojson string
	err := r.db.Raw(`
		SELECT COALESCE(
			ST_AsGeoJSON(ST_MakeLine(array_agg(location::geometry ORDER BY recorded_at))),
			'{"type":"LineString","coordinates":[]}'
		)
		FROM ride_tracks WHERE ride_id = ?
	`, rideID).Scan(&geojson).Error
	return geojson, err
}

type ReportRepository struct {
	db *gorm.DB
}

func NewReportRepository(db *gorm.DB) *ReportRepository {
	return &ReportRepository{db: db}
}

type DailyDriverReport struct {
	DriverID       int64   `json:"driver_id"`
	DriverName     string  `json:"driver_name"`
	TripCount      int     `json:"trip_count"`
	TotalDistanceM int     `json:"total_distance_m"`
	AvgPickupSec   float64 `json:"avg_pickup_sec"`
}

func (r *ReportRepository) DailyDriverStats(date string) ([]DailyDriverReport, error) {
	var rows []DailyDriverReport
	err := r.db.Raw(`
		SELECT
			r.driver_id,
			d.name AS driver_name,
			COUNT(*)::int AS trip_count,
			COALESCE(SUM(r.distance_m), 0)::int AS total_distance_m,
			COALESCE(AVG(EXTRACT(EPOCH FROM (r.accepted_at - r.requested_at))), 0) AS avg_pickup_sec
		FROM rides r
		JOIN drivers d ON d.id = r.driver_id
		WHERE r.status = ? AND r.completed_at::date = ?::date
		GROUP BY r.driver_id, d.name
		ORDER BY trip_count DESC
	`, constants.RideStatusCompleted, date).Scan(&rows).Error
	return rows, err
}

func (r *RideRepository) IsWithinPickup(id int64, lat, lng float64, radiusM float64) (bool, error) {
	var within bool
	err := r.db.Raw(`
		SELECT ST_DWithin(
			pickup_point,
			ST_SetSRID(ST_MakePoint(?, ?), 4326)::geography,
			?
		) FROM rides WHERE id = ?
	`, lng, lat, radiusM, id).Scan(&within).Error
	return within, err
}

func (r *RideRepository) TrackDistanceM(rideID int64) (int, error) {
	var dist float64
	err := r.db.Raw(`
		SELECT COALESCE(
			ST_Length(ST_MakeLine(array_agg(location::geometry ORDER BY recorded_at))::geography),
			0
		) FROM ride_tracks WHERE ride_id = ?
	`, rideID).Scan(&dist).Error
	return int(dist), err
}
