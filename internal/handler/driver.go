package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"line-fleet-dispatch/internal/service"
)

// DriverHandler 司機 API
type DriverHandler struct {
	tracking *service.TrackingService
	drivers  *service.DriverRegistry
}

func NewDriverHandler(tracking *service.TrackingService, drivers *service.DriverRegistry) *DriverHandler {
	return &DriverHandler{tracking: tracking, drivers: drivers}
}

type locationRequest struct {
	DriverID int64   `json:"driver_id" binding:"required"`
	Lat      float64 `json:"lat" binding:"required"`
	Lng      float64 `json:"lng" binding:"required"`
}

// ReportLocation POST /api/driver/location
func (h *DriverHandler) ReportLocation(c *gin.Context) {
	var req locationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "參數錯誤"})
		return
	}

	if err := h.tracking.ReportDriverLocation(c.Request.Context(), req.DriverID, req.Lat, req.Lng); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Register POST /api/driver/register（模擬器/開發用）
func (h *DriverHandler) Register(c *gin.Context) {
	var req struct {
		Name       string `json:"name" binding:"required"`
		LineUserID string `json:"line_user_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "參數錯誤"})
		return
	}
	driver, err := h.drivers.Register(c.Request.Context(), req.LineUserID, req.Name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"driver_id": driver.ID, "name": driver.Name})
}
