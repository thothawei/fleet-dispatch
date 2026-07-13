package repository

import (
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

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

func (r *CustomerRepository) FindByID(id int64) (*model.Customer, error) {
	var customer model.Customer
	if err := r.db.First(&customer, id).Error; err != nil {
		return nil, err
	}
	return &customer, nil
}

func (r *CustomerRepository) SetPassword(id int64, passwordHash string) error {
	return r.db.Model(&model.Customer{}).Where("id = ?", id).Updates(map[string]interface{}{
		"password_hash": passwordHash,
		"updated_at":    time.Now(),
	}).Error
}

type RideRepository struct {
	db *gorm.DB
}

func NewRideRepository(db *gorm.DB) *RideRepository {
	return &RideRepository{db: db}
}

func (r *RideRepository) Create(ride *model.Ride) error {
	// 用 RETURNING 直接取回自增 id，避免以 requested_at 反查造成的競態/誤取
	if ride.DropoffPoint != nil {
		return r.db.Raw(`
			INSERT INTO rides (
				customer_id, status, pickup_point, pickup_address,
				dropoff_point, dropoff_address,
				requested_at, created_at, updated_at
			) VALUES (
				?, ?,
				ST_SetSRID(ST_MakePoint(?, ?), 4326)::geography,
				?,
				ST_SetSRID(ST_MakePoint(?, ?), 4326)::geography,
				?,
				?, ?, ?
			)
			RETURNING id
		`,
			ride.CustomerID,
			ride.Status,
			ride.PickupPoint.Lng,
			ride.PickupPoint.Lat,
			ride.PickupAddress,
			ride.DropoffPoint.Lng,
			ride.DropoffPoint.Lat,
			ride.DropoffAddress,
			ride.RequestedAt,
			ride.CreatedAt,
			ride.UpdatedAt,
		).Scan(&ride.ID).Error
	}
	return r.db.Raw(`
		INSERT INTO rides (
			customer_id, status, pickup_point, pickup_address,
			dropoff_address,
			requested_at, created_at, updated_at
		) VALUES (
			?, ?,
			ST_SetSRID(ST_MakePoint(?, ?), 4326)::geography,
			?,
			?,
			?, ?, ?
		)
		RETURNING id
	`,
		ride.CustomerID,
		ride.Status,
		ride.PickupPoint.Lng,
		ride.PickupPoint.Lat,
		ride.PickupAddress,
		ride.DropoffAddress,
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
	err := r.db.Where("status = ?", constants.DriverStatusIdle).Limit(MaxListRows).Find(&drivers).Error
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

// CompleteRide 完成行程並定格計費欄位（fare/commission/net 為 nil 時該欄位不寫入，維持 NULL）。
func (r *RideRepository) CompleteRide(id int64, distanceM int, fareCents, commissionCents, driverNetCents *int64) error {
	now := time.Now()
	updates := map[string]interface{}{
		"status":       constants.RideStatusCompleted,
		"completed_at": now,
		"distance_m":   distanceM,
		"updated_at":   now,
	}
	if fareCents != nil {
		updates["fare_amount_cents"] = *fareCents
	}
	if commissionCents != nil {
		updates["commission_amount_cents"] = *commissionCents
	}
	if driverNetCents != nil {
		updates["driver_net_amount_cents"] = *driverNetCents
	}
	return r.db.Model(&model.Ride{}).Where("id = ? AND status = ?", id, constants.RideStatusPickedUp).
		Updates(updates).Error
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
	TotalDistanceM int64   `json:"total_distance_m"` // int64 防大量加總溢位（F9-2）
	AvgPickupSec   float64 `json:"avg_pickup_sec"`
	// 金額欄位（分）：營業額、手續費、司機實得（F5）。
	TotalRevenueCents    int64 `json:"total_revenue_cents"`
	TotalCommissionCents int64 `json:"total_commission_cents"`
	DriverNetCents       int64 `json:"driver_net_cents"`
}

func (r *ReportRepository) DailyDriverStats(date string) ([]DailyDriverReport, error) {
	// date 為 YYYY-MM-DD。用半開區間 [date, date+1) 而非 completed_at::date 轉型，
	// 讓查詢 sargable、能走 idx_rides_status_completed 索引（F9-1）；加總一律 ::bigint 防溢位（F9-2）。
	var rows []DailyDriverReport
	err := r.db.Raw(`
		SELECT
			r.driver_id,
			d.name AS driver_name,
			COUNT(*)::int AS trip_count,
			COALESCE(SUM(r.distance_m), 0)::bigint AS total_distance_m,
			COALESCE(AVG(EXTRACT(EPOCH FROM (r.accepted_at - r.requested_at))), 0) AS avg_pickup_sec,
			COALESCE(SUM(r.fare_amount_cents), 0)::bigint AS total_revenue_cents,
			COALESCE(SUM(r.commission_amount_cents), 0)::bigint AS total_commission_cents,
			COALESCE(SUM(r.driver_net_amount_cents), 0)::bigint AS driver_net_cents
		FROM rides r
		JOIN drivers d ON d.id = r.driver_id
		WHERE r.status = ?
		  AND r.completed_at >= ?::date
		  AND r.completed_at < (?::date + INTERVAL '1 day')
		GROUP BY r.driver_id, d.name
		ORDER BY trip_count DESC
	`, constants.RideStatusCompleted, date, date).Scan(&rows).Error
	return rows, err
}

// MonthlyDriverReport 月營運報表每司機一列。MembershipFeeCents / OwedToHqCents
// 屬「應付總公司」語意，由 handler 依當前費率補上（repo 只回聚合數字）。
type MonthlyDriverReport struct {
	DriverID             int64  `json:"driver_id"`
	DriverName           string `json:"driver_name"`
	TripCount            int    `json:"trip_count"`
	TotalRevenueCents    int64  `json:"total_revenue_cents"`
	TotalCommissionCents int64  `json:"total_commission_cents"`
	DriverNetCents       int64  `json:"driver_net_cents"`
	MembershipFeeCents   int64  `json:"membership_fee_cents"`
	OwedToHqCents        int64  `json:"owed_to_hq_cents"`
}

// RollupRideDay 於行程完成後，重算該行程所屬 (司機, 台北日) 的彙總桶（F9-3）。
// 以「重算整桶」而非「+1 增量」實作——冪等、永遠等於 rides 的即時聚合，可安全重跑、自我修復。
func (r *ReportRepository) RollupRideDay(rideID int64) error {
	return r.db.Exec(`
		INSERT INTO daily_driver_earnings (driver_id, day, trip_count, revenue_cents, commission_cents, net_cents, updated_at)
		SELECT r2.driver_id,
		       (r2.completed_at AT TIME ZONE 'Asia/Taipei')::date AS day,
		       COUNT(*)::int,
		       COALESCE(SUM(r2.fare_amount_cents), 0)::bigint,
		       COALESCE(SUM(r2.commission_amount_cents), 0)::bigint,
		       COALESCE(SUM(r2.driver_net_amount_cents), 0)::bigint,
		       NOW()
		FROM rides r2
		JOIN rides r1 ON r1.id = ?
		WHERE r2.status = ? AND r2.driver_id = r1.driver_id
		  AND (r2.completed_at AT TIME ZONE 'Asia/Taipei')::date = (r1.completed_at AT TIME ZONE 'Asia/Taipei')::date
		GROUP BY r2.driver_id, (r2.completed_at AT TIME ZONE 'Asia/Taipei')::date
		ON CONFLICT (driver_id, day) DO UPDATE SET
		  trip_count       = EXCLUDED.trip_count,
		  revenue_cents    = EXCLUDED.revenue_cents,
		  commission_cents = EXCLUDED.commission_cents,
		  net_cents        = EXCLUDED.net_cents,
		  updated_at       = NOW()
	`, rideID, constants.RideStatusCompleted).Error
}

// MonthlyDriverStats 月營運報表（F6）。讀 F9-3 預聚合表 daily_driver_earnings（每司機 ≤31 列）
// 而非即時 GROUP BY 全表 rides。day 為 Taipei 日界，與 [monthStart, +1 月) 的月界一致。
func (r *ReportRepository) MonthlyDriverStats(month string) ([]MonthlyDriverReport, error) {
	monthStart := month + "-01"
	var rows []MonthlyDriverReport
	err := r.db.Raw(`
		SELECT
			dde.driver_id,
			d.name AS driver_name,
			SUM(dde.trip_count)::int AS trip_count,
			SUM(dde.revenue_cents)::bigint AS total_revenue_cents,
			SUM(dde.commission_cents)::bigint AS total_commission_cents,
			SUM(dde.net_cents)::bigint AS driver_net_cents
		FROM daily_driver_earnings dde
		JOIN drivers d ON d.id = dde.driver_id
		WHERE dde.day >= ?::date AND dde.day < (?::date + INTERVAL '1 month')
		GROUP BY dde.driver_id, d.name
		ORDER BY total_revenue_cents DESC
	`, monthStart, monthStart).Scan(&rows).Error
	return rows, err
}

// DriverEarnings 單一司機當月收入聚合（F7）。
type DriverEarnings struct {
	TripCount            int   `json:"trip_count"`
	TotalRevenueCents    int64 `json:"total_revenue_cents"`
	TotalCommissionCents int64 `json:"total_commission_cents"`
	DriverNetCents       int64 `json:"driver_net_cents"`
}

// DriverMonthlyEarnings 查單一司機當月收入（F7）。month 為 YYYY-MM。
// 讀 F9-3 預聚合表（≤31 列）；SUM 無 GROUP BY 恆回一列（無資料則為 0）。
func (r *ReportRepository) DriverMonthlyEarnings(driverID int64, month string) (DriverEarnings, error) {
	monthStart := month + "-01"
	var e DriverEarnings
	err := r.db.Raw(`
		SELECT
			COALESCE(SUM(trip_count), 0)::int AS trip_count,
			COALESCE(SUM(revenue_cents), 0)::bigint AS total_revenue_cents,
			COALESCE(SUM(commission_cents), 0)::bigint AS total_commission_cents,
			COALESCE(SUM(net_cents), 0)::bigint AS driver_net_cents
		FROM daily_driver_earnings
		WHERE driver_id = ? AND day >= ?::date AND day < (?::date + INTERVAL '1 month')
	`, driverID, monthStart, monthStart).Scan(&e).Error
	return e, err
}

// FeeSettingsRepository 費率設定（單列）讀寫。
type FeeSettingsRepository struct {
	db *gorm.DB
}

func NewFeeSettingsRepository(db *gorm.DB) *FeeSettingsRepository {
	return &FeeSettingsRepository{db: db}
}

// Get 讀取單列費率設定（id=1，由 migration 種下，恆存在）。
func (r *FeeSettingsRepository) Get() (model.FleetSettings, error) {
	var s model.FleetSettings
	err := r.db.Where("id = ?", 1).First(&s).Error
	return s, err
}

// Update 覆寫費率設定（單列 id=1）。
func (r *FeeSettingsRepository) Update(s *model.FleetSettings) error {
	return r.db.Model(&model.FleetSettings{}).Where("id = ?", 1).
		Updates(map[string]interface{}{
			"base_fare_cents":              s.BaseFareCents,
			"per_km_fare_cents":            s.PerKmFareCents,
			"min_fare_cents":               s.MinFareCents,
			"commission_bps":               s.CommissionBps,
			"monthly_membership_fee_cents": s.MonthlyMembershipFeeCents,
			"lost_item_fee_bps":            s.LostItemFeeBps,
			"updated_by":                   s.UpdatedBy,
			"updated_at":                   time.Now(),
		}).Error
}

// MembershipInvoiceRepository 會費帳單讀寫（F8）。
type MembershipInvoiceRepository struct {
	db *gorm.DB
}

func NewMembershipInvoiceRepository(db *gorm.DB) *MembershipInvoiceRepository {
	return &MembershipInvoiceRepository{db: db}
}

// GenerateForMonth 為「當月有完成行程的司機」各產生一筆會費帳單（冪等）。
// amountCents 為產生當下的月會費快照；同司機同月已存在的帳單不會被覆寫（ON CONFLICT DO NOTHING），
// 故重跑安全、只補新活躍司機。回傳新建筆數。period 為 YYYY-MM。
func (r *MembershipInvoiceRepository) GenerateForMonth(period string, amountCents int64) (int64, error) {
	monthStart := period + "-01"
	// SELECT-list 的 ? 需顯式轉型：pgx 預設把裸參數綁為 text，Postgres 不會自動把
	// text 塞進 bigint 欄位（amount_cents），故對 period/amount 明確 cast。
	res := r.db.Exec(`
		INSERT INTO membership_invoices (driver_id, period, amount_cents, status, created_at, updated_at)
		SELECT DISTINCT r.driver_id, ?::text, ?::bigint, 'unpaid', NOW(), NOW()
		FROM rides r
		WHERE r.status = ? AND r.driver_id IS NOT NULL
		  AND r.completed_at >= ?::date
		  AND r.completed_at < (?::date + INTERVAL '1 month')
		ON CONFLICT (driver_id, period) DO NOTHING
	`, period, amountCents, constants.RideStatusCompleted, monthStart, monthStart)
	return res.RowsAffected, res.Error
}

// MembershipInvoiceRow 帳單列（含司機名），供後台顯示。
type MembershipInvoiceRow struct {
	ID          int64      `json:"id"`
	DriverID    int64      `json:"driver_id"`
	DriverName  string     `json:"driver_name"`
	Period      string     `json:"period"`
	AmountCents int64      `json:"amount_cents"`
	Status      string     `json:"status"`
	PaidAt      *time.Time `json:"paid_at"`
}

// List 依 period（必填）與 status（選填 unpaid/paid，空字串為全部）列帳單。
func (r *MembershipInvoiceRepository) List(period, status string) ([]MembershipInvoiceRow, error) {
	var rows []MembershipInvoiceRow
	err := r.db.Raw(`
		SELECT mi.id, mi.driver_id, d.name AS driver_name, mi.period,
		       mi.amount_cents, mi.status, mi.paid_at
		FROM membership_invoices mi
		JOIN drivers d ON d.id = mi.driver_id
		WHERE mi.period = ? AND (? = '' OR mi.status = ?)
		ORDER BY mi.driver_id
		LIMIT ?
	`, period, status, status, MaxListRows).Scan(&rows).Error
	return rows, err
}

// SetPaid 標記帳單為已繳/未繳（含 paid_at）。回傳是否更新到（找不到 id 回 false）。
func (r *MembershipInvoiceRepository) SetPaid(id int64, paid bool) (bool, error) {
	updates := map[string]interface{}{"updated_at": time.Now()}
	if paid {
		updates["status"] = "paid"
		updates["paid_at"] = time.Now()
	} else {
		updates["status"] = "unpaid"
		updates["paid_at"] = nil
	}
	res := r.db.Model(&model.MembershipInvoice{}).Where("id = ?", id).Updates(updates)
	return res.RowsAffected > 0, res.Error
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

func (r *DriverRepository) ListAll() ([]model.Driver, error) {
	var drivers []model.Driver
	err := r.db.Order("id ASC").Limit(MaxListRows).Find(&drivers).Error
	return drivers, err
}

// AdminRideRow 後台訂單列表的投影（不含地理欄位，避免 GeoPoint 掃描問題）
type AdminRideRow struct {
	ID            int64      `json:"id"`
	CustomerID    int64      `json:"customer_id"`
	DriverID      *int64     `json:"driver_id"`
	Status        int16      `json:"status"`
	PickupAddress string     `json:"pickup_address"`
	RequestedAt   time.Time  `json:"requested_at"`
	CompletedAt   *time.Time `json:"completed_at"`
	DistanceM     *int       `json:"distance_m"`
}

const (
	RideListDefaultLimit = 100
	RideListMaxLimit     = 500
)

// MaxListRows 是「逐筆」列表查詢的硬上限，避免任何清單無上限回傳拖垮 DB/記憶體（F9-5）。
// 設得遠高於現實規模作安全網；drivers／membership 若逼近此值代表車隊已大到該改真分頁
// （offset/keyset，比照 RideRepository.List）。
const MaxListRows = 5000

// RideListFilter 後台訂單列表的查詢條件；欄位為零值即不套用該條件。
// From/To 是 YYYY-MM-DD，比對 requested_at 的日期（含頭尾），與 DailyDriverStats 的
// `completed_at::date` 慣例一致。
type RideListFilter struct {
	Status *int16
	From   string
	To     string
	Q      string // 上車點地址模糊比對，或訂單 ID 的子字串比對
	Limit  int
	Offset int
}

// applyRideListFilter 只組 WHERE，不含投影與排序——Count 與取列共用同一組條件。
func (r *RideRepository) applyRideListFilter(f RideListFilter) *gorm.DB {
	q := r.db.Model(&model.Ride{})
	if f.Status != nil {
		q = q.Where("status = ?", *f.Status)
	}
	if f.From != "" {
		q = q.Where("requested_at::date >= ?::date", f.From)
	}
	if f.To != "" {
		q = q.Where("requested_at::date <= ?::date", f.To)
	}
	if f.Q != "" {
		like := "%" + f.Q + "%"
		q = q.Where("(pickup_address ILIKE ? OR CAST(id AS TEXT) LIKE ?)", like, like)
	}
	return q
}

// List 後台訂單列表：依條件篩選，依 id 由新到舊分頁。
// 第二個回傳值是符合條件的總筆數（不受 limit/offset 影響），供前端分頁器使用。
func (r *RideRepository) List(f RideListFilter) ([]AdminRideRow, int64, error) {
	if f.Limit <= 0 || f.Limit > RideListMaxLimit {
		f.Limit = RideListDefaultLimit
	}
	if f.Offset < 0 {
		f.Offset = 0
	}

	var total int64
	if err := r.applyRideListFilter(f).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []AdminRideRow
	err := r.applyRideListFilter(f).
		Select("id", "customer_id", "driver_id", "status", "pickup_address", "requested_at", "completed_at", "distance_m").
		Order("id DESC").Limit(f.Limit).Offset(f.Offset).
		Scan(&rows).Error
	return rows, total, err
}

// ListRecent 取最近的訂單（不分頁）。保留給既有呼叫端。
func (r *RideRepository) ListRecent(status *int16, limit int) ([]AdminRideRow, error) {
	rows, _, err := r.List(RideListFilter{Status: status, Limit: limit})
	return rows, err
}

type AdminRepository struct {
	db *gorm.DB
}

func NewAdminRepository(db *gorm.DB) *AdminRepository {
	return &AdminRepository{db: db}
}

func (r *AdminRepository) FindByUsername(username string) (*model.Admin, error) {
	var a model.Admin
	if err := r.db.Where("username = ?", username).First(&a).Error; err != nil {
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

func (r *AdminRepository) FindByID(id int64) (*model.Admin, error) {
	var a model.Admin
	if err := r.db.First(&a, id).Error; err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *AdminRepository) ListAll() ([]model.Admin, error) {
	var list []model.Admin
	err := r.db.Order("id asc").Limit(MaxListRows).Find(&list).Error
	return list, err
}

func (r *AdminRepository) UpdateAdmin(a *model.Admin) error {
	return r.db.Model(a).Select("Role", "PasswordHash", "IsActive", "UpdatedAt").Updates(a).Error
}

// CountActiveSuperadmins 可帶交易 handle；tx 為 nil 時用預設 db
func (r *AdminRepository) CountActiveSuperadmins(tx *gorm.DB) (int64, error) {
	db := r.db
	if tx != nil {
		db = tx
	}
	var n int64
	err := db.Model(&model.Admin{}).
		Where("role = ? AND is_active = ?", "superadmin", true).Count(&n).Error
	return n, err
}

// LockActiveSuperadmins 於交易內對 active superadmin 列加 FOR UPDATE row lock 並回其數量。
// 讓並發的降級/停用交易序列化，避免 write-skew 造成零 superadmin。
// Postgres 不能把 FOR UPDATE 直接套在聚合 COUNT 上，故改用 Find 鎖列後取切片長度。
func (r *AdminRepository) LockActiveSuperadmins(tx *gorm.DB) (int64, error) {
	if tx == nil {
		return 0, errors.New("LockActiveSuperadmins 需在交易內呼叫")
	}
	var rows []model.Admin
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("role = ? AND is_active = ?", "superadmin", true).
		Find(&rows).Error
	return int64(len(rows)), err
}

// Tx 在交易內執行 fn
func (r *AdminRepository) Tx(fn func(*gorm.DB) error) error {
	return r.db.Transaction(fn)
}
