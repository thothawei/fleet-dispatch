package service

import (
	"context"
	"errors"
	"time"

	"github.com/rs/zerolog/log"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/model"
	"line-fleet-dispatch/internal/repository"
)

// scheduledRideCreator 是 dispatcher 需要的建單能力（由 *RideService 滿足）。
// 抽成介面純粹是為了讓 Tick 測得起來——不然每個測試都得備妥 redis 與派單服務。
type scheduledRideCreator interface {
	CreateByCustomer(ctx context.Context, customerID int64, req CustomerCreateRequest) (*model.Ride, error)
}

// ScheduledRideDispatcher 背景排程器：把到點的預約轉成真訂單。
//
// 轉單走的是**與乘客手動叫車完全同一支** RideService.CreateByCustomer——
// 派單、審計、車種驗證、「同時只能有一張進行中訂單」的規則因此全部自動沿用。
// 若在這裡另外寫一份建單邏輯，兩條路徑遲早會長歪（其中一條漏掉某個規則，
// 而且是預約那條漏——因為它沒有人在旁邊看著）。
type ScheduledRideDispatcher struct {
	repo   *repository.ScheduledRideRepository
	rides  scheduledRideCreator
	lead   time.Duration
	ticker time.Duration
	// expiryGrace 過了約定時間多久就不再轉單（見 dispatchOne 的說明）。
	expiryGrace time.Duration
	// now 可注入，供測試控制時間。
	now func() time.Time
}

func NewScheduledRideDispatcher(
	repo *repository.ScheduledRideRepository,
	rides scheduledRideCreator,
) *ScheduledRideDispatcher {
	return &ScheduledRideDispatcher{
		repo:        repo,
		rides:       rides,
		lead:        constants.ScheduledRideLeadMinutes * time.Minute,
		ticker:      30 * time.Second,
		expiryGrace: constants.ScheduledRideExpiryGraceMinutes * time.Minute,
		now:         time.Now,
	}
}

// SetExpiryGrace 覆寫過期作廢的寬限時間（測試用）。
func (d *ScheduledRideDispatcher) SetExpiryGrace(g time.Duration) { d.expiryGrace = g }

// SetLead 覆寫提前發動時間（測試與未來的後台可調用）。
func (d *ScheduledRideDispatcher) SetLead(lead time.Duration) { d.lead = lead }

// SetInterval 覆寫掃描週期。
func (d *ScheduledRideDispatcher) SetInterval(interval time.Duration) { d.ticker = interval }

// SetNow 覆寫時間來源（測試用）。
func (d *ScheduledRideDispatcher) SetNow(now func() time.Time) { d.now = now }

// Run 常駐掃描，直到 ctx 結束。
//
// 掃描週期（30 秒）遠小於提前量（15 分鐘）是刻意的：即使某一輪整個被跳過
// （重啟、DB 短暫不可用），下一輪仍遠在約定時間之前，預約不會因此遲到。
func (d *ScheduledRideDispatcher) Run(ctx context.Context) {
	t := time.NewTicker(d.ticker)
	defer t.Stop()
	log.Info().
		Dur("interval", d.ticker).
		Dur("lead", d.lead).
		Msg("預約行程排程器已啟動")
	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("預約行程排程器已停止")
			return
		case <-t.C:
			if n, err := d.Tick(ctx); err != nil {
				log.Error().Err(err).Msg("預約行程轉單失敗")
			} else if n > 0 {
				log.Info().Int("dispatched", n).Msg("預約行程已轉為訂單")
			}
		}
	}
}

// Tick 跑一輪：找出到點的預約並逐筆轉單，回傳成功轉單的筆數。
//
// 逐筆獨立處理、逐筆記錯——一筆失敗不該讓同一輪的其他預約跟著沒轉成。
func (d *ScheduledRideDispatcher) Tick(ctx context.Context) (int, error) {
	due, err := d.repo.FindDue(d.now(), d.lead, scheduledDispatchBatch)
	if err != nil {
		return 0, err
	}
	dispatched := 0
	for i := range due {
		ok, err := d.dispatchOne(ctx, &due[i])
		if err != nil {
			log.Error().Err(err).Int64("scheduled_ride_id", due[i].ID).
				Msg("預約轉單發生錯誤")
			continue
		}
		if ok {
			dispatched++
		}
	}
	return dispatched, nil
}

// dispatchOne 轉一筆預約；回傳是否真的轉成訂單。
func (d *ScheduledRideDispatcher) dispatchOne(ctx context.Context, s *model.ScheduledRide) (bool, error) {
	// 早就過了約定時間的不再轉單，直接作廢。
	//
	// 這條在正常運作下永遠不會成立（提前量 15 分鐘，早轉完了），它防的是
	// **排程器停過一段時間**：重啟時若把積壓的過期預約全部轉成真訂單，
	// 乘客昨天預約的車今天會突然開到他家樓下，而且他還得為那趟付錢。
	// 標 failed 而不是留在 pending，是為了讓乘客在清單上看得到「這筆沒成立」與原因——
	// 留著不動的話它會永遠掛在「即將到來」，而那台車永遠不會來。
	if s.ScheduledAt.Before(d.now().Add(-d.expiryGrace)) {
		if _, err := d.repo.MarkFailed(s.ID, "預約時間已過，系統未能及時派車"); err != nil {
			return false, err
		}
		log.Warn().Int64("scheduled_ride_id", s.ID).
			Time("scheduled_at", s.ScheduledAt).
			Msg("預約已過期太久，不再轉單")
		return false, nil
	}

	// 先認領。認領不到 ＝ 別的排程器（或上一輪的殘留）已經在處理這筆，直接跳過。
	claimed, err := d.repo.ClaimForDispatch(s.ID, s.AttemptCount)
	if err != nil {
		return false, err
	}
	if !claimed {
		return false, nil
	}
	attempt := s.AttemptCount + 1

	req := CustomerCreateRequest{
		PickupLat:           s.PickupPoint.Lat,
		PickupLng:           s.PickupPoint.Lng,
		PickupAddress:       s.PickupAddress,
		DropoffAddress:      s.DropoffAddress,
		RequiredVehicleType: s.RequiredVehicleType,
	}
	if s.DropoffPoint != nil {
		lat, lng := s.DropoffPoint.Lat, s.DropoffPoint.Lng
		req.DropoffLat, req.DropoffLng = &lat, &lng
	}

	ride, err := d.rides.CreateByCustomer(ctx, s.CustomerID, req)
	if err != nil {
		d.recordFailure(s.ID, attempt, err)
		return false, nil
	}

	ok, err := d.repo.MarkDispatched(s.ID, ride.ID)
	if err != nil {
		return false, err
	}
	if !ok {
		// 建單成功了，但這筆預約已不在 pending（乘客剛好在這一瞬間取消）。
		// 訂單已經進了派單池，收不回來——記下來讓人查得到，不要靜靜吞掉。
		log.Warn().Int64("scheduled_ride_id", s.ID).Int64("ride_id", ride.ID).
			Msg("預約已建出訂單，但預約本身已非 pending（可能同時被取消）")
	}
	return ok, nil
}

// recordFailure 依錯誤性質決定「等下一輪重試」還是「直接判死」。
//
// 這個分類是這支排程器最容易做錯的地方：把永久錯誤（座標無效、車種無效）
// 當成可重試，那筆預約會在池子裡重試到上限才失敗，乘客整整等到約定時間才知道沒車；
// 反過來把暫時錯誤（乘客當下還在另一趟行程上）當成永久失敗，
// 則會把一筆本來只要晚幾分鐘就能成立的預約直接砍掉。
func (d *ScheduledRideDispatcher) recordFailure(id int64, attempt int, cause error) {
	reason := cause.Error()
	if isPermanentScheduleError(cause) {
		if _, err := d.repo.MarkFailed(id, reason); err != nil {
			log.Error().Err(err).Int64("scheduled_ride_id", id).Msg("標記預約失敗時出錯")
		}
		return
	}
	if attempt >= constants.ScheduledRideMaxAttempts {
		if _, err := d.repo.MarkFailed(id, reason); err != nil {
			log.Error().Err(err).Int64("scheduled_ride_id", id).Msg("標記預約失敗時出錯")
		}
		return
	}
	if err := d.repo.RecordAttemptError(id, reason); err != nil {
		log.Error().Err(err).Int64("scheduled_ride_id", id).Msg("記錄預約重試原因時出錯")
	}
}

// isPermanentScheduleError 這個錯誤重試幾次都不會變好嗎？
//
// 只列**確定**不會自己好的：資料本身就不合法。其餘（乘客還在行程中、
// 叫車頻率限制、DB 暫時不可用）一律當成可重試——下一輪只差 30 秒，代價很低，
// 而誤判成永久失敗的代價是乘客的車沒了。
func isPermanentScheduleError(err error) bool {
	switch {
	case errors.Is(err, ErrInvalidCoords),
		errors.Is(err, ErrInvalidVehicleType),
		errors.Is(err, ErrStopsUnavailable):
		return true
	}
	return false
}
