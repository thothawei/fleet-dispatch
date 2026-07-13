package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"line-fleet-dispatch/internal/events"
	"line-fleet-dispatch/internal/middleware"
	"line-fleet-dispatch/internal/service"
)

// ChatHandler 行程內對話（乘客↔司機）。
type ChatHandler struct {
	chat *service.ChatService
}

func NewChatHandler(chat *service.ChatService) *ChatHandler {
	return &ChatHandler{chat: chat}
}

// List GET /api/rides/:id/messages?after=<id>&limit=<n>（MultiAuth：本趟乘客/司機、admin）
// after 供增量補讀：只回傳 id 大於 after 的訊息（WS 斷線重連後補漏用）。
func (h *ChatHandler) List(c *gin.Context) {
	rideID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id 格式錯誤"})
		return
	}
	afterID, _ := strconv.ParseInt(c.DefaultQuery("after", "0"), 10, 64)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))

	role := middleware.RoleFromCtx(c)
	subjectID := middleware.SubjectIDFromCtx(c)
	msgs, err := h.chat.List(role, subjectID, rideID, afterID, limit)
	if err != nil {
		c.JSON(readStatusForErr(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"messages": msgs})
}

// Send POST /api/rides/:id/messages（MultiAuth：僅本趟乘客/司機可發話）
// 訊息持久化後透過 WebSocket 即時推播給行程雙方（chat.message 事件）。
func (h *ChatHandler) Send(c *gin.Context) {
	rideID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id 格式錯誤"})
		return
	}
	role := middleware.RoleFromCtx(c)
	if role != events.RoleCustomer && role != events.RoleDriver {
		c.JSON(http.StatusForbidden, gin.H{"error": "僅行程乘客或司機可發送訊息"})
		return
	}
	var req struct {
		Body string `json:"body"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "參數錯誤"})
		return
	}
	msg, err := h.chat.Send(role, middleware.SubjectIDFromCtx(c), rideID, req.Body)
	if err != nil {
		c.JSON(chatStatusForErr(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": msg})
}

// chatStatusForErr 對話錯誤對應 HTTP 狀態碼。
func chatStatusForErr(err error) int {
	switch {
	case errors.Is(err, service.ErrEmptyMessage), errors.Is(err, service.ErrMessageTooLong):
		return http.StatusBadRequest
	default:
		return readStatusForErr(err)
	}
}
