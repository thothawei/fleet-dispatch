package service

import (
	"github.com/rs/zerolog/log"

	"line-fleet-dispatch/internal/model"
	"line-fleet-dispatch/internal/repository"
)

// rideAuditor 訂單狀態審計（D4）。nil repo 時靜默略過，方便單元測試不接 DB。
type rideAuditor struct {
	events *repository.RideEventRepository
}

func (a *rideAuditor) record(
	rideID int64,
	fromStatus *int16,
	toStatus int16,
	eventType, actorRole string,
	actorID *int64,
	note string,
) {
	if a == nil || a.events == nil {
		return
	}
	e := &model.RideEvent{
		RideID:     rideID,
		FromStatus: fromStatus,
		ToStatus:   toStatus,
		EventType:  eventType,
		ActorRole:  actorRole,
		ActorID:    actorID,
		Note:       note,
	}
	if err := a.events.Append(e); err != nil {
		log.Error().Err(err).Int64("ride_id", rideID).Str("event_type", eventType).Msg("寫入 ride_events 失敗")
	}
}

func statusPtr(s int16) *int16 { return &s }
func idPtr(id int64) *int64    { return &id }
