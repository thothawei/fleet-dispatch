package service

import (
	"errors"
	"strings"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/model"
	"line-fleet-dispatch/internal/repository"
)

var (
	// ErrInvalidPlaceKind kind 不在白名單。
	ErrInvalidPlaceKind = errors.New("地點種類無效")
	// ErrEmptyPlaceLabel 名稱為空。
	ErrEmptyPlaceLabel = errors.New("地點名稱不可為空")
	// ErrPlaceLabelTooLong 名稱過長。
	ErrPlaceLabelTooLong = errors.New("地點名稱過長")
	// ErrEmptyPlaceAddress 地址為空。
	ErrEmptyPlaceAddress = errors.New("地址不可為空")
	// ErrPlaceAddressTooLong 地址過長。
	ErrPlaceAddressTooLong = errors.New("地址過長")
	// ErrTooManyPlaces 已達常用地點數量上限。
	ErrTooManyPlaces = errors.New("常用地點數量已達上限")
)

const (
	maxPlaceLabelRunes   = 40
	maxPlaceAddressRunes = 200
	// maxPlacesPerCustomer 單一乘客的地點上限。純粹是防呆——沒有人真的有 50 個常用地點，
	// 但沒有上限的清單遲早會被腳本灌爆。
	maxPlacesPerCustomer = 50
)

// SavedPlaceService 常用地點（住家／公司／自訂）。
type SavedPlaceService struct {
	places *repository.SavedPlaceRepository
}

func NewSavedPlaceService(places *repository.SavedPlaceRepository) *SavedPlaceService {
	return &SavedPlaceService{places: places}
}

// SavePlaceInput 新增／更新常用地點的輸入。
type SavePlaceInput struct {
	Kind    string
	Label   string
	Address string
	Lat     float64
	Lng     float64
}

// List 該乘客的常用地點（住家 → 公司 → 其他）。
func (s *SavedPlaceService) List(customerID int64) ([]model.CustomerSavedPlace, error) {
	return s.places.ListByCustomer(customerID)
}

// Create 新增常用地點。
//
// **home／work 是 upsert 語意**：那兩個是「插槽」不是「清單項目」——乘客在 UI 上按的是
// 「設定住家」，他預期的結果是住家變成新地址，而不是收到一個「你已經有住家了」的錯誤，
// 還得先去把舊的刪掉。custom 才是真的每次新增一筆。
func (s *SavedPlaceService) Create(customerID int64, in SavePlaceInput) (*model.CustomerSavedPlace, error) {
	kind, label, address, err := normalizePlaceInput(in)
	if err != nil {
		return nil, err
	}
	if err := validatePickupCoords(in.Lat, in.Lng); err != nil {
		return nil, err
	}
	point := model.GeoPoint{Lat: in.Lat, Lng: in.Lng}

	if constants.IsSlotSavedPlaceKind(kind) {
		existing, err := s.places.FindByKind(customerID, kind)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			ok, err := s.places.Update(existing.ID, customerID, label, address, point)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, ErrNotFound
			}
			return s.places.GetOwned(existing.ID, customerID)
		}
	}

	n, err := s.places.CountByCustomer(customerID)
	if err != nil {
		return nil, err
	}
	if n >= maxPlacesPerCustomer {
		return nil, ErrTooManyPlaces
	}

	place := &model.CustomerSavedPlace{
		CustomerID: customerID,
		Kind:       kind,
		Label:      label,
		Address:    address,
		Point:      point,
	}
	if err := s.places.Create(place); err != nil {
		// 競態：兩個請求同時設住家，先查後寫之間對方插進來了。
		// 這裡不回錯給乘客——他要的結果（住家＝這個地址）用覆蓋一樣能達成。
		if errors.Is(err, repository.ErrDuplicateSlot) && constants.IsSlotSavedPlaceKind(kind) {
			existing, ferr := s.places.FindByKind(customerID, kind)
			if ferr != nil || existing == nil {
				return nil, err
			}
			if _, uerr := s.places.Update(existing.ID, customerID, label, address, point); uerr != nil {
				return nil, uerr
			}
			return s.places.GetOwned(existing.ID, customerID)
		}
		return nil, err
	}
	return place, nil
}

// Update 改既有地點的名稱／地址／座標。kind 不可改（見 repository.Update 說明）。
func (s *SavedPlaceService) Update(customerID, id int64, in SavePlaceInput) (*model.CustomerSavedPlace, error) {
	existing, err := s.places.GetOwned(id, customerID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, ErrNotFound
	}
	// kind 沿用既有值：呼叫端不必重送，送了也不算數。
	in.Kind = existing.Kind
	_, label, address, err := normalizePlaceInput(in)
	if err != nil {
		return nil, err
	}
	if err := validatePickupCoords(in.Lat, in.Lng); err != nil {
		return nil, err
	}
	ok, err := s.places.Update(id, customerID, label, address,
		model.GeoPoint{Lat: in.Lat, Lng: in.Lng})
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotFound
	}
	return s.places.GetOwned(id, customerID)
}

// Delete 刪除自己的常用地點。
func (s *SavedPlaceService) Delete(customerID, id int64) error {
	ok, err := s.places.Delete(id, customerID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}

// normalizePlaceInput 修剪空白並驗證；回傳正規化後的 kind／label／address。
//
// label 留空時以 kind 的預設名稱補上——乘客在「設定住家」流程裡本來就沒有輸入名稱的欄位，
// 強迫他取一個名字只是多一步。
func normalizePlaceInput(in SavePlaceInput) (kind, label, address string, err error) {
	kind = strings.TrimSpace(in.Kind)
	if kind == "" {
		kind = constants.SavedPlaceKindCustom
	}
	if !constants.IsValidSavedPlaceKind(kind) {
		return "", "", "", ErrInvalidPlaceKind
	}
	label = strings.TrimSpace(in.Label)
	if label == "" {
		label = defaultPlaceLabel(kind)
	}
	if label == "" {
		return "", "", "", ErrEmptyPlaceLabel
	}
	if len([]rune(label)) > maxPlaceLabelRunes {
		return "", "", "", ErrPlaceLabelTooLong
	}
	address = strings.TrimSpace(in.Address)
	if address == "" {
		return "", "", "", ErrEmptyPlaceAddress
	}
	if len([]rune(address)) > maxPlaceAddressRunes {
		return "", "", "", ErrPlaceAddressTooLong
	}
	return kind, label, address, nil
}

// defaultPlaceLabel 插槽種類的預設顯示名稱；custom 沒有預設（必須自己取名）。
func defaultPlaceLabel(kind string) string {
	switch kind {
	case constants.SavedPlaceKindHome:
		return "住家"
	case constants.SavedPlaceKindWork:
		return "公司"
	default:
		return ""
	}
}
