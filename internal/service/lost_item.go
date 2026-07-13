package service

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/events"
	"line-fleet-dispatch/internal/model"
	"line-fleet-dispatch/internal/repository"
)

var (
	ErrLostItemExists     = errors.New("此行程已有進行中的協尋單")
	ErrRideNotCompleted   = errors.New("僅已完成的行程可申請遺失物協尋")
	ErrBadLostItemState   = errors.New("協尋單目前狀態不允許此操作")
	ErrEmptyDescription   = errors.New("請描述遺失的物品")
	ErrDescriptionTooLong = errors.New("物品描述長度超過上限")
)

// lostItemDescMaxRunes 物品描述長度上限（DB 另有 char_length ≤ 500 的最後防線）。
const lostItemDescMaxRunes = 300

// LostItemService 遺失物協尋：乘客對已完成行程建單 → 司機尋獲 → 乘客支付處理費 → 歸還。
// 處理費＝該趟車資 × 處理費%（建立當下快照），% 由後台費率設定調整。
type LostItemService struct {
	rides     *repository.RideRepository
	items     *repository.LostItemRepository
	fees      *FeeSettings
	publisher events.Publisher
}

func NewLostItemService(rides *repository.RideRepository, items *repository.LostItemRepository, fees *FeeSettings, publisher events.Publisher) *LostItemService {
	return &LostItemService{rides: rides, items: items, fees: fees, publisher: publisher}
}

// CreateByCustomer 乘客對自己的已完成行程建立協尋單，處理費於此刻快照。
func (s *LostItemService) CreateByCustomer(customerID, rideID int64, description string) (*model.LostItemRequest, error) {
	description = strings.TrimSpace(description)
	if description == "" {
		return nil, ErrEmptyDescription
	}
	if utf8.RuneCountInString(description) > lostItemDescMaxRunes {
		return nil, ErrDescriptionTooLong
	}
	ride, err := s.rides.GetByID(rideID)
	if err != nil {
		return nil, ErrNotFound
	}
	if ride.CustomerID != customerID {
		return nil, ErrForbidden
	}
	if ride.Status != constants.RideStatusCompleted || ride.DriverID == nil {
		return nil, ErrRideNotCompleted
	}
	if existing, err := s.items.FindActiveByRide(rideID); err != nil {
		return nil, err
	} else if existing != nil {
		return nil, ErrLostItemExists
	}

	// 處理費快照：round(車資 × bps / 10000)。無車資的舊行程（如 LINE 建單未計費）費用為 0。
	bps := s.fees.LostItemFeeBps()
	var fare int64
	if ride.FareAmountCents != nil {
		fare = *ride.FareAmountCents
	}
	item := &model.LostItemRequest{
		RideID:      rideID,
		CustomerID:  customerID,
		DriverID:    *ride.DriverID,
		Description: description,
		FeeCents:    roundDiv(fare*int64(bps), 10000),
		FeeBps:      bps,
		Status:      constants.LostItemStatusOpen,
	}
	if err := s.items.Create(item); err != nil {
		return nil, err
	}
	s.publishItem(item, events.TypeLostItemCreated)
	return item, nil
}

// MarkFound 司機標記已尋獲（open → found），之後等待乘客支付處理費。
func (s *LostItemService) MarkFound(driverID, itemID int64) (*model.LostItemRequest, error) {
	return s.transition(events.RoleDriver, driverID, itemID,
		[]string{constants.LostItemStatusOpen}, constants.LostItemStatusFound, false)
}

// Pay 乘客支付處理費（found → paid）。目前為記帳式確認（無金流），付訖後司機安排歸還。
func (s *LostItemService) Pay(customerID, itemID int64) (*model.LostItemRequest, error) {
	return s.transition(events.RoleCustomer, customerID, itemID,
		[]string{constants.LostItemStatusFound}, constants.LostItemStatusPaid, true)
}

// MarkReturned 司機標記已歸還（paid → returned），結案。
func (s *LostItemService) MarkReturned(driverID, itemID int64) (*model.LostItemRequest, error) {
	return s.transition(events.RoleDriver, driverID, itemID,
		[]string{constants.LostItemStatusPaid}, constants.LostItemStatusReturned, false)
}

// Close 乘客或司機結案（open/found → closed：未尋獲或取消）。已付款後不可 close，只能歸還。
func (s *LostItemService) Close(role string, subjectID, itemID int64) (*model.LostItemRequest, error) {
	return s.transition(role, subjectID, itemID,
		[]string{constants.LostItemStatusOpen, constants.LostItemStatusFound}, constants.LostItemStatusClosed, false)
}

// GetForRide 取該行程最新協尋單（本趟乘客／司機；admin 可稽核）；無則回 (nil, nil)。
func (s *LostItemService) GetForRide(role string, subjectID, rideID int64) (*model.LostItemRequest, error) {
	ride, err := s.rides.GetByID(rideID)
	if err != nil {
		return nil, ErrNotFound
	}
	if role != events.RoleAdmin {
		if err := authorizeRideParticipant(ride, role, subjectID); err != nil {
			return nil, err
		}
	}
	return s.items.LatestByRide(rideID)
}

// ListByDriver 司機的未結案協尋單（工作清單）。
func (s *LostItemService) ListByDriver(driverID int64) ([]model.LostItemRequest, error) {
	return s.items.ListByDriver(driverID, activeLostItemStatuses())
}

// ListByCustomer 乘客的未結案協尋單。
func (s *LostItemService) ListByCustomer(customerID int64) ([]model.LostItemRequest, error) {
	return s.items.ListByCustomer(customerID, activeLostItemStatuses())
}

func activeLostItemStatuses() []string {
	return []string{constants.LostItemStatusOpen, constants.LostItemStatusFound, constants.LostItemStatusPaid}
}

// transition 共用守門轉換：驗擁有權 → 條件式 UPDATE（防競態）→ 重讀 → 推播 lost_item.updated。
func (s *LostItemService) transition(role string, subjectID, itemID int64, from []string, to string, markPaid bool) (*model.LostItemRequest, error) {
	item, err := s.items.GetByID(itemID)
	if err != nil {
		return nil, ErrNotFound
	}
	switch role {
	case events.RoleCustomer:
		if item.CustomerID != subjectID {
			return nil, ErrForbidden
		}
	case events.RoleDriver:
		if item.DriverID != subjectID {
			return nil, ErrForbidden
		}
	default:
		return nil, ErrForbidden
	}
	ok, err := s.items.TransitionStatus(itemID, from, to, markPaid)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrBadLostItemState
	}
	item, err = s.items.GetByID(itemID)
	if err != nil {
		return nil, err
	}
	s.publishItem(item, events.TypeLostItemUpdated)
	return item, nil
}

// publishItem 推播協尋單事件給行程雙方。
func (s *LostItemService) publishItem(item *model.LostItemRequest, eventType string) {
	if s.publisher == nil {
		return
	}
	ev := events.Event{
		Type:    eventType,
		RideID:  item.RideID,
		Payload: lostItemPayload(item),
	}
	s.publisher.Publish(events.Recipient{Role: events.RoleCustomer, ID: item.CustomerID}, ev)
	s.publisher.Publish(events.Recipient{Role: events.RoleDriver, ID: item.DriverID}, ev)
}

// lostItemPayload 序列化協尋單為 WS 事件 payload（與 REST 回應同欄位）。
func lostItemPayload(item *model.LostItemRequest) map[string]any {
	var paidAt any
	if item.PaidAt != nil {
		paidAt = item.PaidAt.Format(time.RFC3339)
	}
	return map[string]any{
		"id":          item.ID,
		"ride_id":     item.RideID,
		"customer_id": item.CustomerID,
		"driver_id":   item.DriverID,
		"description": item.Description,
		"fee_cents":   item.FeeCents,
		"fee_bps":     item.FeeBps,
		"status":      item.Status,
		"paid_at":     paidAt,
		"created_at":  item.CreatedAt.Format(time.RFC3339),
	}
}
