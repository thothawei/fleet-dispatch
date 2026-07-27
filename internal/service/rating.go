package service

import (
	"errors"
	"strings"
	"unicode/utf8"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/model"
	"line-fleet-dispatch/internal/repository"
)

var (
	// ErrInvalidScore 星等超出 1–5。App 端只給得出 1–5，這是直打 API 的防線。
	ErrInvalidScore = errors.New("評分必須是 1 到 5 顆星")
	// ErrRatingExists 一趟一評、不可重評（見 migration 000023 的理由）。
	ErrRatingExists = errors.New("此行程已評分過")
	// ErrCommentTooLong 評論字數超過上限。
	ErrCommentTooLong = errors.New("評論長度超過上限")
	// ErrRideNotRatable 行程尚未完成、或根本沒派到司機（派單前取消）。
	// **不可重用遺失物的 ErrRideNotCompleted**——它的文案是「僅已完成的行程可申請
	// 遺失物協尋」，會原樣顯示在乘客的評分對話框上，講的卻是另一個功能
	// （2026-07-27 模擬器實跑抓到；單元測試斷言 errors.Is 而非文案，所以測不到）。
	ErrRideNotRatable = errors.New("僅已完成的行程可以評分")
)

// ratingCommentMaxRunes 評論字數上限（DB 另有 char_length ≤ 500 的最後防線）。
const ratingCommentMaxRunes = 200

// RatingService 乘客評分司機（B5）：只有該趟乘客、只有已完成行程、一趟只能評一次。
type RatingService struct {
	rides   *repository.RideRepository
	ratings *repository.RideRatingRepository
}

func NewRatingService(rides *repository.RideRepository, ratings *repository.RideRatingRepository) *RatingService {
	return &RatingService{rides: rides, ratings: ratings}
}

// RateByCustomer 乘客對自己的已完成行程評分。
//
// 驗證順序刻意是「輸入 → 歸屬 → 狀態 → 重複」：先擋掉不必查 DB 的輸入錯誤，
// 再依序回答「這趟是不是你的」「這趟完成了沒」「你評過了沒」——
// 每一步的錯誤訊息都直接對應乘客該知道的下一步。
func (s *RatingService) RateByCustomer(customerID, rideID int64, score int, comment string) (*model.RideRating, error) {
	if score < 1 || score > 5 {
		return nil, ErrInvalidScore
	}
	comment = strings.TrimSpace(comment)
	if utf8.RuneCountInString(comment) > ratingCommentMaxRunes {
		return nil, ErrCommentTooLong
	}
	ride, err := s.rides.GetByID(rideID)
	if err != nil {
		return nil, ErrNotFound
	}
	if ride.CustomerID != customerID {
		return nil, ErrForbidden
	}
	// 沒司機的行程（派單前取消）沒有評分對象；未完成的行程還沒有可評的服務。
	if ride.Status != constants.RideStatusCompleted || ride.DriverID == nil {
		return nil, ErrRideNotRatable
	}
	if existing, err := s.ratings.FindByRide(rideID); err != nil {
		return nil, err
	} else if existing != nil {
		return nil, ErrRatingExists
	}

	rating := &model.RideRating{
		RideID:     rideID,
		CustomerID: customerID,
		DriverID:   *ride.DriverID,
		Score:      int16(score),
		Comment:    comment,
	}
	if err := s.ratings.Create(rating); err != nil {
		return nil, err
	}
	return rating, nil
}

// SummaryByDriver 司機的平均分與則數（司機主頁自己看）。
func (s *RatingService) SummaryByDriver(driverID int64) (repository.DriverRatingSummary, error) {
	return s.ratings.SummaryByDriver(driverID)
}
