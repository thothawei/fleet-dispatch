package service

import (
	"errors"
	"strings"
	"time"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/model"
	"line-fleet-dispatch/internal/repository"
)

var (
	// ErrScheduleTooSoon 預約時間太近，該直接叫車而不是預約。
	ErrScheduleTooSoon = errors.New("預約時間太接近，請直接叫車")
	// ErrScheduleTooFar 預約時間超過可預約範圍。
	ErrScheduleTooFar = errors.New("預約時間超過可預約範圍")
	// ErrTooManySchedules 未完成的預約數已達上限。
	ErrTooManySchedules = errors.New("待執行的預約數量已達上限")
	// ErrScheduleNotCancellable 預約已轉單／已取消，不能再取消。
	ErrScheduleNotCancellable = errors.New("此預約已無法取消")
	// ErrScheduleNoteTooLong 備註過長。
	ErrScheduleNoteTooLong = errors.New("備註過長")
)

const (
	maxScheduleNoteRunes = 200
	// maxPendingSchedules 單一乘客同時可存在的 pending 預約數。
	maxPendingSchedules = 20
	// scheduledDispatchBatch 每輪最多處理幾筆，避免單輪長時間佔住 DB 連線。
	scheduledDispatchBatch = 100
)

// ScheduledRideService 預約行程的建立／查詢／取消。
//
// 「到點轉單」不在這裡，在 ScheduledRideDispatcher——那是背景排程器的職責，
// 與乘客觸發的同步請求分開，才不會有人因為別人的預約到點而被拖慢。
type ScheduledRideService struct {
	repo *repository.ScheduledRideRepository
}

func NewScheduledRideService(repo *repository.ScheduledRideRepository) *ScheduledRideService {
	return &ScheduledRideService{repo: repo}
}

// ScheduleRideInput 建立預約的輸入。
type ScheduleRideInput struct {
	ScheduledAt            time.Time
	PickupLat, PickupLng   float64
	PickupAddress          string
	DropoffAddress         string
	DropoffLat, DropoffLng *float64
	RequiredVehicleType    string
	Note                   string
}

// Create 建立預約。
//
// 刻意**不擋**「已有進行中訂單」——預約的是未來，現在正在搭車跟三小時後要用車沒有衝突。
// 那道檢查留在到點轉單那一刻（走 RideService.CreateByCustomer 的既有規則）。
func (s *ScheduledRideService) Create(customerID int64, in ScheduleRideInput) (*model.ScheduledRide, error) {
	return s.createAt(customerID, in, time.Now())
}

// createAt 是 Create 的可測版本（把「現在」變成參數，時間邊界才驗得了）。
func (s *ScheduledRideService) createAt(customerID int64, in ScheduleRideInput, now time.Time) (*model.ScheduledRide, error) {
	if in.ScheduledAt.IsZero() {
		return nil, ErrScheduleTooSoon
	}
	minAt := now.Add(constants.ScheduledRideMinLeadMinutes * time.Minute)
	if in.ScheduledAt.Before(minAt) {
		return nil, ErrScheduleTooSoon
	}
	maxAt := now.AddDate(0, 0, constants.ScheduledRideMaxDaysAhead)
	if in.ScheduledAt.After(maxAt) {
		return nil, ErrScheduleTooFar
	}
	if err := validatePickupCoords(in.PickupLat, in.PickupLng); err != nil {
		return nil, err
	}
	if err := validateOptionalDropoffCoords(in.DropoffLat, in.DropoffLng); err != nil {
		return nil, err
	}
	if in.RequiredVehicleType != "" && !constants.IsValidVehicleType(in.RequiredVehicleType) {
		return nil, ErrInvalidVehicleType
	}
	note := strings.TrimSpace(in.Note)
	if len([]rune(note)) > maxScheduleNoteRunes {
		return nil, ErrScheduleNoteTooLong
	}

	n, err := s.repo.CountPendingByCustomer(customerID)
	if err != nil {
		return nil, err
	}
	if n >= maxPendingSchedules {
		return nil, ErrTooManySchedules
	}

	row := &model.ScheduledRide{
		CustomerID:          customerID,
		ScheduledAt:         in.ScheduledAt,
		PickupPoint:         model.GeoPoint{Lat: in.PickupLat, Lng: in.PickupLng},
		PickupAddress:       strings.TrimSpace(in.PickupAddress),
		DropoffAddress:      strings.TrimSpace(in.DropoffAddress),
		RequiredVehicleType: in.RequiredVehicleType,
		Note:                note,
		Status:              constants.ScheduledRideStatusPending,
	}
	if in.DropoffLat != nil && in.DropoffLng != nil {
		row.DropoffPoint = &model.GeoPoint{Lat: *in.DropoffLat, Lng: *in.DropoffLng}
	}
	if err := s.repo.Create(row); err != nil {
		return nil, err
	}
	return row, nil
}

// List 乘客的預約清單；upcomingOnly 只回還沒轉單的。
func (s *ScheduledRideService) List(customerID int64, upcomingOnly bool) ([]model.ScheduledRide, error) {
	return s.repo.ListByCustomer(customerID, upcomingOnly)
}

// Get 取自己的單筆預約。
func (s *ScheduledRideService) Get(customerID, id int64) (*model.ScheduledRide, error) {
	row, err := s.repo.GetOwned(id, customerID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, ErrNotFound
	}
	return row, nil
}

// Cancel 取消預約。
//
// 已被排程器轉單的回 ErrScheduleNotCancellable（handler 對映 409）——
// **不能宣稱取消成功**：那張真訂單已經在派單池裡，司機可能正在開過來。
// App 收到 409 要重讀這筆預約，讓乘客看到「已轉為訂單」並引導他去取消那張訂單。
func (s *ScheduledRideService) Cancel(customerID, id int64) (*model.ScheduledRide, error) {
	row, err := s.repo.GetOwned(id, customerID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, ErrNotFound
	}
	ok, err := s.repo.Cancel(id, customerID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrScheduleNotCancellable
	}
	return s.repo.GetOwned(id, customerID)
}
