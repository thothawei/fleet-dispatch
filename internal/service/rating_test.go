package service

import (
	"errors"
	"strings"
	"testing"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/repository"
)

// completedRideForRating 造一趟「已完成且有司機」的行程，供評分測試使用。
func completedRideForRating(t *testing.T, rides *repository.RideRepository, customerID, driverID int64) int64 {
	t.Helper()
	ride := newTestRide(t, rides, customerID, constants.RideStatusRequested)
	if err := rides.AcceptRide(ride.ID, driverID, 300); err != nil {
		t.Fatalf("指派司機失敗：%v", err)
	}
	if err := rides.MarkPickedUp(ride.ID); err != nil {
		t.Fatalf("標記上車失敗：%v", err)
	}
	fare, comm, net := int64(10000), int64(1500), int64(8500)
	if err := rides.CompleteRide(ride.ID, 3000, &fare, &comm, &net, nil); err != nil {
		t.Fatalf("完成行程失敗：%v", err)
	}
	return ride.ID
}

// TestRatingFlow 驗證 B5 評分主流程與四道守門：
// 星等值域、只有本人、只有已完成行程、一趟一評。
func TestRatingFlow(t *testing.T) {
	db := newServiceTestDB(t)
	customers := repository.NewCustomerRepository(db)
	drivers := repository.NewDriverRepository(db)
	rides := repository.NewRideRepository(db)
	ratings := repository.NewRideRatingRepository(db)

	owner, err := customers.FindOrCreateByLineUserID("U_rating_owner", "評分乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	other, err := customers.FindOrCreateByLineUserID("U_rating_other", "路人乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	driver, err := drivers.FindOrCreate("U_rating_driver", "被評分司機")
	if err != nil {
		t.Fatalf("建立司機失敗：%v", err)
	}

	svc := NewRatingService(rides, ratings)
	rideID := completedRideForRating(t, rides, owner.ID, driver.ID)

	// 星等值域：0 與 6 都不可接受（App 只給得出 1–5，這是直打 API 的防線）
	for _, bad := range []int{0, 6, -1} {
		if _, err := svc.RateByCustomer(owner.ID, rideID, bad, ""); !errors.Is(err, ErrInvalidScore) {
			t.Fatalf("星等 %d 應被拒，got %v", bad, err)
		}
	}

	// 評論字數上限（DB CHECK 之前先擋，錯誤訊息才說得清楚）
	if _, err := svc.RateByCustomer(owner.ID, rideID, 5, strings.Repeat("啊", ratingCommentMaxRunes+1)); !errors.Is(err, ErrCommentTooLong) {
		t.Fatalf("超長評論應被拒，got %v", err)
	}

	// 非本人的行程：403 而非 404——訂單存在，只是不是你的
	if _, err := svc.RateByCustomer(other.ID, rideID, 5, ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("他人行程評分應被拒，got %v", err)
	}

	// 不存在的行程
	if _, err := svc.RateByCustomer(owner.ID, 999999, 5, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在的行程應回 ErrNotFound，got %v", err)
	}

	// 未完成的行程沒有可評的服務
	activeRide := newTestRide(t, rides, owner.ID, constants.RideStatusPickedUp)
	err = func() error { _, e := svc.RateByCustomer(owner.ID, activeRide.ID, 5, ""); return e }()
	if !errors.Is(err, ErrRideNotRatable) {
		t.Fatalf("未完成行程評分應被拒，got %v", err)
	}
	// **文案也要驗**：錯誤訊息會原樣顯示在乘客的評分對話框上。
	// 只斷言 errors.Is 的話，重用了遺失物的 ErrRideNotCompleted（文案是
	// 「僅已完成的行程可申請遺失物協尋」）照樣會綠——2026-07-27 實跑才抓到。
	if strings.Contains(err.Error(), "遺失物") {
		t.Fatalf("評分的錯誤訊息不該提到遺失物協尋：%q", err.Error())
	}

	// 主流程：評論前後空白會被 trim
	rating, err := svc.RateByCustomer(owner.ID, rideID, 4, "  司機很準時  ")
	if err != nil {
		t.Fatalf("評分失敗：%v", err)
	}
	if rating.Score != 4 || rating.Comment != "司機很準時" {
		t.Fatalf("評分內容錯誤：%+v", rating)
	}
	if rating.DriverID != driver.ID || rating.CustomerID != owner.ID {
		t.Fatalf("評分歸屬錯誤：%+v", rating)
	}

	// 一趟一評：不可重評（分數送出即定論，見 migration 000023）
	if _, err := svc.RateByCustomer(owner.ID, rideID, 1, "反悔了"); !errors.Is(err, ErrRatingExists) {
		t.Fatalf("重複評分應被拒，got %v", err)
	}

	// 回讀：FindByRide 拿得到剛才那筆
	got, err := ratings.FindByRide(rideID)
	if err != nil || got == nil || got.Score != 4 {
		t.Fatalf("回讀評分失敗：%v / %+v", err, got)
	}
}

// TestRatingSummaryByDriver 司機平均分：無評分回 (0,0)，多筆取平均。
func TestRatingSummaryByDriver(t *testing.T) {
	db := newServiceTestDB(t)
	customers := repository.NewCustomerRepository(db)
	drivers := repository.NewDriverRepository(db)
	rides := repository.NewRideRepository(db)
	ratings := repository.NewRideRatingRepository(db)

	owner, err := customers.FindOrCreateByLineUserID("U_summary_owner", "評分乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	driver, err := drivers.FindOrCreate("U_summary_driver", "被評分司機")
	if err != nil {
		t.Fatalf("建立司機失敗：%v", err)
	}
	svc := NewRatingService(rides, ratings)

	// 新司機沒有評分是常態，不該是錯誤
	summary, err := svc.SummaryByDriver(driver.ID)
	if err != nil {
		t.Fatalf("無評分時查詢不該失敗：%v", err)
	}
	if summary.Count != 0 || summary.Average != 0 {
		t.Fatalf("無評分應回 (0,0)，得到 %+v", summary)
	}

	for _, score := range []int{5, 4, 3} {
		rideID := completedRideForRating(t, rides, owner.ID, driver.ID)
		if _, err := svc.RateByCustomer(owner.ID, rideID, score, ""); err != nil {
			t.Fatalf("評分 %d 失敗：%v", score, err)
		}
	}
	summary, err = svc.SummaryByDriver(driver.ID)
	if err != nil {
		t.Fatalf("查詢平均分失敗：%v", err)
	}
	if summary.Count != 3 || summary.Average != 4 {
		t.Fatalf("平均分應為 4（3 則），得到 %+v", summary)
	}
}

// TestCustomerRideViewCarriesRating 乘客查自己訂單時帶回已給過的評分（B5 讀回路徑）；
// 未評分時 Rating 為 nil，App 端據此顯示評分入口。
func TestCustomerRideViewCarriesRating(t *testing.T) {
	db := newServiceTestDB(t)
	customers := repository.NewCustomerRepository(db)
	drivers := repository.NewDriverRepository(db)
	rides := repository.NewRideRepository(db)
	tracks := repository.NewTrackRepository(db)
	ratings := repository.NewRideRatingRepository(db)

	owner, err := customers.FindOrCreateByLineUserID("U_view_owner", "評分乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	driver, err := drivers.FindOrCreate("U_view_driver", "被評分司機")
	if err != nil {
		t.Fatalf("建立司機失敗：%v", err)
	}

	query := NewRideQueryService(tracks, rides)
	query.SetRatings(ratings)
	rideID := completedRideForRating(t, rides, owner.ID, driver.ID)

	view, err := query.GetRideForCustomer(owner.ID, rideID)
	if err != nil {
		t.Fatalf("查訂單失敗：%v", err)
	}
	if view.Rating != nil {
		t.Fatalf("尚未評分時 Rating 應為 nil，得到 %+v", view.Rating)
	}

	svc := NewRatingService(rides, ratings)
	if _, err := svc.RateByCustomer(owner.ID, rideID, 5, "很棒"); err != nil {
		t.Fatalf("評分失敗：%v", err)
	}
	view, err = query.GetRideForCustomer(owner.ID, rideID)
	if err != nil {
		t.Fatalf("查訂單失敗：%v", err)
	}
	if view.Rating == nil || view.Rating.Score != 5 || view.Rating.Comment != "很棒" {
		t.Fatalf("評分未帶回訂單視圖：%+v", view.Rating)
	}

	// 歷史列表也要看得出評過沒（完成卡關掉後的唯一補評路徑）
	rows, err := query.ListRecentByCustomer(owner.ID, 0)
	if err != nil {
		t.Fatalf("查歷史列表失敗：%v", err)
	}
	var found bool
	for _, row := range rows {
		if row.ID != rideID {
			continue
		}
		found = true
		if row.RatingScore == nil || *row.RatingScore != 5 {
			t.Fatalf("歷史列表未帶回星等：%+v", row.RatingScore)
		}
	}
	if !found {
		t.Fatalf("歷史列表沒有這趟行程（%d）", rideID)
	}
}

// TestRatingSummaryByDrivers 批次彙總（後台司機列表用）：一條 GROUP BY 取代 N+1。
// **沒評分的司機不出現在 map 裡**，呼叫端以零值呈現「尚無評分」。
func TestRatingSummaryByDrivers(t *testing.T) {
	db := newServiceTestDB(t)
	customers := repository.NewCustomerRepository(db)
	drivers := repository.NewDriverRepository(db)
	rides := repository.NewRideRepository(db)
	ratings := repository.NewRideRatingRepository(db)

	owner, err := customers.FindOrCreateByLineUserID("U_batch_owner", "評分乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	d1, err := drivers.FindOrCreate("U_batch_d1", "司機一")
	if err != nil {
		t.Fatalf("建立司機失敗：%v", err)
	}
	d2, err := drivers.FindOrCreate("U_batch_d2", "司機二")
	if err != nil {
		t.Fatalf("建立司機失敗：%v", err)
	}
	d3, err := drivers.FindOrCreate("U_batch_d3", "沒被評過的司機")
	if err != nil {
		t.Fatalf("建立司機失敗：%v", err)
	}

	svc := NewRatingService(rides, ratings)
	// 司機一：5 與 3 → 平均 4；司機二：2 → 平均 2；司機三：無評分
	for _, tc := range []struct {
		driverID int64
		score    int
	}{{d1.ID, 5}, {d1.ID, 3}, {d2.ID, 2}} {
		rideID := completedRideForRating(t, rides, owner.ID, tc.driverID)
		if _, err := svc.RateByCustomer(owner.ID, rideID, tc.score, ""); err != nil {
			t.Fatalf("評分失敗：%v", err)
		}
	}

	got, err := ratings.SummaryByDrivers([]int64{d1.ID, d2.ID, d3.ID})
	if err != nil {
		t.Fatalf("批次彙總失敗：%v", err)
	}
	if s := got[d1.ID]; s.Count != 2 || s.Average != 4 {
		t.Fatalf("司機一應為 4 分／2 則，得到 %+v", s)
	}
	if s := got[d2.ID]; s.Count != 1 || s.Average != 2 {
		t.Fatalf("司機二應為 2 分／1 則，得到 %+v", s)
	}
	if _, ok := got[d3.ID]; ok {
		t.Fatalf("沒評分的司機不該出現在 map 裡（呼叫端以零值呈現「尚無評分」）")
	}
	// 與逐列查詢一致——批次是為了效能，不是為了不同的答案
	single, err := svc.SummaryByDriver(d1.ID)
	if err != nil || single.Average != got[d1.ID].Average || single.Count != got[d1.ID].Count {
		t.Fatalf("批次與逐列結果不一致：%+v vs %+v（err=%v）", got[d1.ID], single, err)
	}

	// 空清單不該打 DB，也不該爆
	empty, err := ratings.SummaryByDrivers(nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("空清單應回空 map，得到 %+v（err=%v）", empty, err)
	}
}
