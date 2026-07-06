package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"line-fleet-dispatch/internal/middleware"
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

// statusForErr 擁有權錯誤回 403，其餘狀態衝突回 409
func statusForErr(err error) int {
	if errors.Is(err, service.ErrForbidden) {
		return http.StatusForbidden
	}
	return http.StatusConflict
}

// Accept POST /api/rides/:id/accept（需 JWT，driver_id 取自 token）
func (h *RideHandler) Accept(c *gin.Context) {
	rideID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	driverID := middleware.DriverIDFromCtx(c)
	msg, err := h.dispatch.AcceptRide(c.Request.Context(), rideID, driverID, "")
	if err != nil {
		c.JSON(statusForErr(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": msg})
}

// PickUp POST /api/rides/:id/pickup
func (h *RideHandler) PickUp(c *gin.Context) {
	rideID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	driverID := middleware.DriverIDFromCtx(c)
	if err := h.tracking.PickUp(c.Request.Context(), rideID, driverID); err != nil {
		c.JSON(statusForErr(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Complete POST /api/rides/:id/complete
func (h *RideHandler) Complete(c *gin.Context) {
	rideID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	driverID := middleware.DriverIDFromCtx(c)
	if err := h.tracking.Complete(c.Request.Context(), rideID, driverID); err != nil {
		c.JSON(statusForErr(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Cancel POST /api/rides/:id/cancel — 司機放棄已接的訂單（觸發重新派單）
func (h *RideHandler) Cancel(c *gin.Context) {
	rideID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	driverID := middleware.DriverIDFromCtx(c)
	msg, err := h.dispatch.CancelByDriver(c.Request.Context(), rideID, driverID)
	if err != nil {
		c.JSON(statusForErr(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": msg})
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
