package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"line-fleet-dispatch/internal/service"
)

// RideHandler 訂單 API
type RideHandler struct {
	dispatch *service.DispatchService
	tracking *service.TrackingService
	rides    *service.RideQueryService
}

func NewRideHandler(
	dispatch *service.DispatchService,
	tracking *service.TrackingService,
	rides *service.RideQueryService,
) *RideHandler {
	return &RideHandler{dispatch: dispatch, tracking: tracking, rides: rides}
}

// Accept POST /api/rides/:id/accept
func (h *RideHandler) Accept(c *gin.Context) {
	rideID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var req struct {
		DriverID int64 `json:"driver_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "參數錯誤"})
		return
	}
	msg, err := h.dispatch.AcceptRide(c.Request.Context(), rideID, req.DriverID, "")
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": msg})
}

// PickUp POST /api/rides/:id/pickup
func (h *RideHandler) PickUp(c *gin.Context) {
	rideID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var req struct {
		DriverID int64 `json:"driver_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "參數錯誤"})
		return
	}
	if err := h.tracking.PickUp(c.Request.Context(), rideID, req.DriverID); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Complete POST /api/rides/:id/complete
func (h *RideHandler) Complete(c *gin.Context) {
	rideID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var req struct {
		DriverID int64 `json:"driver_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "參數錯誤"})
		return
	}
	if err := h.tracking.Complete(c.Request.Context(), rideID, req.DriverID); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Track GET /api/rides/:id/track — GeoJSON 軌跡回放
func (h *RideHandler) Track(c *gin.Context) {
	rideID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	geojson, err := h.rides.TrackGeoJSON(rideID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", []byte(geojson))
}
