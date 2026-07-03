package handler

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

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

	for _, event := range payload.Events {
		switch event.Type {
		case "message":
			h.handleMessage(c, event)
		case "postback":
			h.handlePostback(c, event)
		}
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *LineWebhookHandler) handleMessage(c *gin.Context, event lineEvent) {
	if event.Message.Type != "location" || event.Source.UserID == "" {
		return
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
		return
	}

	log.Info().
		Int64("ride_id", ride.ID).
		Float64("lat", event.Message.Latitude).
		Float64("lng", event.Message.Longitude).
		Msg("收到叫車位置")

	_ = h.lineClient.ReplyText(c.Request.Context(), event.ReplyToken, rideReceivedMessage)
}

func (h *LineWebhookHandler) handlePostback(c *gin.Context, event lineEvent) {
	if event.Source.UserID == "" || event.Postback.Data == "" {
		return
	}

	rideID, ok := lineclient.ParsePostbackRideID(event.Postback.Data)
	if !ok {
		// 也嘗試解析 URI 格式
		if vals, err := url.ParseQuery(strings.ReplaceAll(event.Postback.Data, "&", "&")); err == nil {
			if idStr := vals.Get("ride_id"); idStr != "" {
				if id, err := parseInt64(idStr); err == nil {
					rideID = id
					ok = true
				}
			}
		}
	}
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

func parseInt64(s string) (int64, error) {
	var n int64
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0, fmt.Errorf("invalid")
		}
		n = n*10 + int64(ch-'0')
	}
	return n, nil
}
