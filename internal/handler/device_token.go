package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"line-fleet-dispatch/internal/middleware"
	"line-fleet-dispatch/internal/notify"
	"line-fleet-dispatch/internal/service"
)

// DeviceTokenHandler 司機／乘客註冊與註銷 FCM／APNs token。
type DeviceTokenHandler struct {
	tokens *service.DeviceTokenService
}

func NewDeviceTokenHandler(tokens *service.DeviceTokenService) *DeviceTokenHandler {
	return &DeviceTokenHandler{tokens: tokens}
}

type deviceTokenBody struct {
	Platform string `json:"platform"`
	Token    string `json:"token"`
}

// RegisterByDriver POST /api/driver/device-token
func (h *DeviceTokenHandler) RegisterByDriver(c *gin.Context) {
	h.register(c, notify.RoleDriver, middleware.DriverIDFromCtx(c))
}

// UnregisterByDriver DELETE /api/driver/device-token
func (h *DeviceTokenHandler) UnregisterByDriver(c *gin.Context) {
	h.unregister(c, notify.RoleDriver, middleware.DriverIDFromCtx(c))
}

// RegisterByCustomer POST /api/customer/device-token
func (h *DeviceTokenHandler) RegisterByCustomer(c *gin.Context) {
	h.register(c, notify.RoleCustomer, middleware.CustomerIDFromCtx(c))
}

// UnregisterByCustomer DELETE /api/customer/device-token
func (h *DeviceTokenHandler) UnregisterByCustomer(c *gin.Context) {
	h.unregister(c, notify.RoleCustomer, middleware.CustomerIDFromCtx(c))
}

func (h *DeviceTokenHandler) register(c *gin.Context, role string, subjectID int64) {
	var req deviceTokenBody
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "參數錯誤"})
		return
	}
	if err := h.tokens.Register(role, subjectID, req.Platform, req.Token); err != nil {
		if errors.Is(err, service.ErrInvalidDeviceToken) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *DeviceTokenHandler) unregister(c *gin.Context, role string, subjectID int64) {
	token := strings.TrimSpace(c.Query("token"))
	if token == "" {
		var req deviceTokenBody
		_ = c.ShouldBindJSON(&req)
		token = strings.TrimSpace(req.Token)
	}
	if err := h.tokens.Unregister(role, subjectID, token); err != nil {
		if errors.Is(err, service.ErrInvalidDeviceToken) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
