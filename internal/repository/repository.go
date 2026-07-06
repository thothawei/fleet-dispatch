package repository

import (
	"errors"
	"fmt"
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

func (r *CustomerRepository) FindByLineUserID(lineUserID string) (*model.Customer, error) {
	var customer model.Customer
	err := r.db.Where("line_user_id = ?", lineUserID).First(&customer).Error
	if err != nil {
		return nil, err
	}
	return &customer, nil
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
	// 用 RETURNING 直接取回自增 id，避免以 requested_at 反查造成的競態/誤取
	return r.db.Raw(`
		INSERT INTO rides (
			customer_id, status, pickup_point, pickup_address,
			requested_at, created_at, updated_at
		) VALUES (
			?, ?,
			ST_SetSRID(ST_MakePoint(?, ?), 4326)::geography,
			?, ?, ?, ?
		)
		RETURNING id
	`,
		ride.CustomerID,
		ride.Status,
		ride.PickupPoint.Lng,
		ride.PickupPoint.Lat,
		ride.PickupAddress,
		ride.RequestedAt,
		ride.CreatedAt,
		ride.UpdatedAt,
	).Scan(&ride.ID).Error
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

func (r *DriverRepository) SetPassword(id int64, passwordHash string) error {
	return r.db.Model(&model.Driver{}).Where("id = ?", id).Updates(map[string]interface{}{
		"password_hash": passwordHash,
		"updated_at":    time.Now(),
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

// FindActiveByCustomer 找客戶進行中的訂單（REQUESTED/ASSIGNED/ACCEPTED/PICKED_UP）
func (r *RideRepository) FindActiveByCustomer(customerID int64) (*model.Ride, error) {
	var ride model.Ride
	err := r.db.Where("customer_id = ? AND status IN ?", customerID,
		[]int16{
			constants.RideStatusRequested, constants.RideStatusAssigned,
			constants.RideStatusAccepted, constants.RideStatusPickedUp,
		},
	).Order("id DESC").First(&ride).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ride, nil
}

// CancelRide 條件式取消（僅在允許的狀態才改），回傳是否真的取消到，避免與 accept/complete 競態
func (r *RideRepository) CancelRide(id int64, allowed []int16) (bool, error) {
	res := r.db.Model(&model.Ride{}).Where("id = ? AND status IN ?", id, allowed).
		Updates(map[string]interface{}{
			"status":       constants.RideStatusCancelled,
			"completed_at": time.Now(),
			"updated_at":   time.Now(),
		})
	return res.RowsAffected > 0, res.Error
}

// ResetToRequested 司機放棄已接訂單時，將訂單清回可重派狀態
func (r *RideRepository) ResetToRequested(id int64) error {
	return r.db.Model(&model.Ride{}).Where("id = ?", id).
		Updates(map[string]interface{}{
			"status":         constants.RideStatusRequested,
			"driver_id":      nil,
			"accepted_at":    nil,
			"eta_pickup_sec": nil,
			"updated_at":     time.Now(),
		}).Error
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

// EnsureTrackPartitions 確保「當月 ~ 當月+monthsAhead」的月分區都存在（冪等，可安全重複執行）。
// 邊界一律用 UTC 午夜（與 migration 建的分區一致），避免連線時區不同造成重疊。
func (r *TrackRepository) EnsureTrackPartitions(monthsAhead int) error {
	now := time.Now().UTC()
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i <= monthsAhead; i++ {
		m := start.AddDate(0, i, 0)
		next := m.AddDate(0, 1, 0)
		name := fmt.Sprintf("ride_tracks_%04d_%02d", m.Year(), int(m.Month()))
		stmt := fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s PARTITION OF ride_tracks FOR VALUES FROM ('%s') TO ('%s')`,
			name,
			m.Format("2006-01-02")+" 00:00:00+00",
			next.Format("2006-01-02")+" 00:00:00+00",
		)
		if err := r.db.Exec(stmt).Error; err != nil {
			return fmt.Errorf("建立分區 %s 失敗: %w", name, err)
		}
	}
	return nil
}

// DropOldTrackPartitions 刪除早於保留期的舊月分區（retentionMonths<=0 表示不刪，預設關閉）。回傳刪除的分區名
func (r *TrackRepository) DropOldTrackPartitions(retentionMonths int) ([]string, error) {
	if retentionMonths <= 0 {
		return nil, nil
	}
	now := time.Now().UTC()
	cutoff := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -retentionMonths, 0)

	var names []string
	err := r.db.Raw(`
		SELECT c.relname FROM pg_inherits i
		JOIN pg_class c ON c.oid = i.inhrelid
		JOIN pg_class p ON p.oid = i.inhparent
		WHERE p.relname = 'ride_tracks'
	`).Scan(&names).Error
	if err != nil {
		return nil, err
	}

	var dropped []string
	for _, name := range names {
		var y, mo int
		if _, e := fmt.Sscanf(name, "ride_tracks_%d_%d", &y, &mo); e != nil || mo < 1 || mo > 12 {
			continue
		}
		part := time.Date(y, time.Month(mo), 1, 0, 0, 0, 0, time.UTC)
		if part.Before(cutoff) {
			if e := r.db.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s", name)).Error; e != nil {
				return dropped, e
			}
			dropped = append(dropped, name)
		}
	}
	return dropped, nil
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

type AdminRepository struct {
	db *gorm.DB
}

func NewAdminRepository(db *gorm.DB) *AdminRepository {
	return &AdminRepository{db: db}
}

func (r *AdminRepository) FindByEmail(email string) (*model.Admin, error) {
	var a model.Admin
	if err := r.db.Where("email = ?", email).First(&a).Error; err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *AdminRepository) Create(a *model.Admin) error {
	return r.db.Create(a).Error
}

func (r *AdminRepository) CountAll() (int64, error) {
	var n int64
	err := r.db.Model(&model.Admin{}).Count(&n).Error
	return n, err
}
