package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	lineclient "line-fleet-dispatch/internal/line"
	"line-fleet-dispatch/internal/repository"
	"line-fleet-dispatch/internal/service"
)

const rideReceivedMessage = "已收到您的叫車，正在為您派車"

// LineWebhookHandler 處理 LINE Messaging API webhook
type LineWebhookHandler struct {
	rideService *service.RideService
	dispatch    *service.DispatchService
	drivers     *repository.DriverRepository
	lineClient  *lineclient.Client
}

func NewLineWebhookHandler(
	rideService *service.RideService,
	dispatch *service.DispatchService,
	drivers *repository.DriverRepository,
	lineClient *lineclient.Client,
) *LineWebhookHandler {
	return &LineWebhookHandler{
		rideService: rideService,
		dispatch:    dispatch,
		drivers:     drivers,
		lineClient:  lineClient,
	}
}

type webhookPayload struct {
	Events []lineEvent `json:"events"`
}

type lineEvent struct {
	Type       string       `json:"type"`
	ReplyToken string       `json:"replyToken"`
	Source     lineSource   `json:"source"`
	Message    lineMessage  `json:"message"`
	Postback   linePostback `json:"postback"`
}

type lineSource struct {
	UserID string `json:"userId"`
	Type   string `json:"type"`
}

type lineMessage struct {
	Type      string  `json:"type"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Address   string  `json:"address"`
}

type linePostback struct {
	Data string `json:"data"`
}

func (h *LineWebhookHandler) Handle(c *gin.Context) {
	var payload webhookPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "無效的 JSON"})
		return
	}

	var rideIDs []int64
	for _, event := range payload.Events {
		switch event.Type {
		case "message":
			if id := h.handleMessage(c, event); id > 0 {
				rideIDs = append(rideIDs, id)
			}
		case "postback":
			h.handlePostback(c, event)
		}
	}

	// LINE 會忽略回應內容；附上 ride_ids 方便自動化測試/除錯取得建立的訂單編號
	resp := gin.H{"ok": true}
	if len(rideIDs) > 0 {
		resp["ride_ids"] = rideIDs
	}
	c.JSON(http.StatusOK, resp)
}

// handleMessage 處理位置訊息並建立訂單，回傳建立的 ride id（無則回 0）
func (h *LineWebhookHandler) handleMessage(c *gin.Context, event lineEvent) int64 {
	if event.Message.Type != "location" || event.Source.UserID == "" {
		return 0
	}

	ride, err := h.rideService.CreateFromLocation(c.Request.Context(), service.RideRequest{
		LineUserID:    event.Source.UserID,
		DisplayName:   "",
		PickupLat:     event.Message.Latitude,
		PickupLng:     event.Message.Longitude,
		PickupAddress: event.Message.Address,
	})
	if err != nil {
		log.Error().Err(err).Str("line_user_id", event.Source.UserID).Msg("建立叫車訂單失敗")
		_ = h.lineClient.ReplyText(c.Request.Context(), event.ReplyToken, err.Error())
		return 0
	}

	log.Info().
		Int64("ride_id", ride.ID).
		Float64("lat", event.Message.Latitude).
		Float64("lng", event.Message.Longitude).
		Msg("收到叫車位置")

	_ = h.lineClient.ReplyText(c.Request.Context(), event.ReplyToken, rideReceivedMessage)
	return ride.ID
}

func (h *LineWebhookHandler) handlePostback(c *gin.Context, event lineEvent) {
	if event.Source.UserID == "" || event.Postback.Data == "" {
		return
	}

	rideID, ok := lineclient.ParsePostbackRideID(event.Postback.Data)
	if !ok {
		return
	}

	driver, err := h.drivers.FindByLineUserID(event.Source.UserID)
	if err != nil {
		_ = h.lineClient.ReplyText(c.Request.Context(), event.ReplyToken, "請先註冊為司機")
		return
	}

	msg, err := h.dispatch.AcceptRide(c.Request.Context(), rideID, driver.ID, event.ReplyToken)
	if err != nil {
		_ = h.lineClient.ReplyText(c.Request.Context(), event.ReplyToken, err.Error())
		return
	}
	if msg != "接單成功" {
		_ = h.lineClient.ReplyText(c.Request.Context(), event.ReplyToken, msg)
	}
}
