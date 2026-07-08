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
	dispatch    *service.DispatchService
	tracking    *service.TrackingService
	rides       *service.RideQueryService
	rideService *service.RideService
}

func NewRideHandler(
	dispatch *service.DispatchService,
	tracking *service.TrackingService,
	rides *service.RideQueryService,
	rideService *service.RideService,
) *RideHandler {
	return &RideHandler{dispatch: dispatch, tracking: tracking, rides: rides, rideService: rideService}
}

// statusForErr 擁有權錯誤回 403，其餘狀態衝突回 409
func statusForErr(err error) int {
	if errors.Is(err, service.ErrForbidden) {
		return http.StatusForbidden
	}
	return http.StatusConflict
}

// readStatusForErr 乘客查詢/取消類錯誤對應 HTTP 狀態碼：無權限回 403、找不到回 404、其餘回 500
func readStatusForErr(err error) int {
	switch {
	case errors.Is(err, service.ErrForbidden):
		return http.StatusForbidden
	case errors.Is(err, service.ErrNotFound):
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
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
	dropoff, err := h.tracking.PickUp(c.Request.Context(), rideID, driverID)
	if err != nil {
		c.JSON(statusForErr(err), gin.H{"error": err.Error()})
		return
	}
	resp := gin.H{"ok": true}
	if dropoff != "" {
		resp["dropoff_address"] = dropoff
	}
	c.JSON(http.StatusOK, resp)
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

// Decline POST /api/rides/:id/decline — 司機明確拒接派單邀請（App 版，取代 LINE Flex 按鈕）。
// 記錄拒接後重派會跳過此司機；司機仍為待命狀態，訂單留在可派狀態。
func (h *RideHandler) Decline(c *gin.Context) {
	rideID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id 格式錯誤"})
		return
	}
	driverID := middleware.DriverIDFromCtx(c)
	if err := h.dispatch.DeclineOffer(c.Request.Context(), rideID, driverID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Track GET /api/rides/:id/track — GeoJSON 軌跡回放。
// 受 MultiAuth 保護，僅限本趟乘客／被指派司機／admin 存取。
func (h *RideHandler) Track(c *gin.Context) {
	rideID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id 格式錯誤"})
		return
	}
	role := middleware.RoleFromCtx(c)
	subjectID := middleware.SubjectIDFromCtx(c)
	if err := h.rides.AuthorizeTrackAccess(role, subjectID, rideID); err != nil {
		c.JSON(readStatusForErr(err), gin.H{"error": err.Error()})
		return
	}
	geojson, err := h.rides.TrackGeoJSON(rideID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json", []byte(geojson))
}

// Create POST /api/rides — 乘客 App 直接叫車（customer_id 取自 JWT）
func (h *RideHandler) Create(c *gin.Context) {
	customerID := middleware.CustomerIDFromCtx(c)
	var req struct {
		PickupLat      float64  `json:"pickup_lat"`
		PickupLng      float64  `json:"pickup_lng"`
		PickupAddress  string   `json:"pickup_address"`
		DropoffAddress string   `json:"dropoff_address"`
		DropoffLat     *float64 `json:"dropoff_lat"`
		DropoffLng     *float64 `json:"dropoff_lng"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "參數錯誤"})
		return
	}
	ride, err := h.rideService.CreateByCustomer(
		c.Request.Context(), customerID, service.CustomerCreateRequest{
			PickupLat:      req.PickupLat,
			PickupLng:      req.PickupLng,
			PickupAddress:  req.PickupAddress,
			DropoffAddress: req.DropoffAddress,
			DropoffLat:     req.DropoffLat,
			DropoffLng:     req.DropoffLng,
		},
	)
	if err != nil {
		c.JSON(createStatusForErr(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ride_id": ride.ID, "status": ride.Status})
}

// createStatusForErr 將下單錯誤對應到 HTTP 狀態碼。
func createStatusForErr(err error) int {
	switch {
	case errors.Is(err, service.ErrInvalidCoords):
		return http.StatusBadRequest
	case errors.Is(err, service.ErrActiveRideExists):
		return http.StatusConflict
	case errors.Is(err, service.ErrRateLimited):
		return http.StatusTooManyRequests
	default:
		return http.StatusInternalServerError
	}
}

// ActiveByCustomer GET /api/customer/rides/active — 乘客當前進行中訂單（App 啟動/重連取 ride_id 用）
// 無進行中訂單時回 {"ride": null}，非錯誤。
func (h *RideHandler) ActiveByCustomer(c *gin.Context) {
	customerID := middleware.CustomerIDFromCtx(c)
	ride, err := h.rides.GetActiveRideByCustomer(customerID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ride": ride})
}

// GetByCustomer GET /api/customer/rides/:id — 乘客查自己單一訂單狀態/司機/ETA，非本人訂單回 403/404
func (h *RideHandler) GetByCustomer(c *gin.Context) {
	rideID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id 格式錯誤"})
		return
	}
	customerID := middleware.CustomerIDFromCtx(c)
	ride, err := h.rides.GetRideForCustomer(customerID, rideID)
	if err != nil {
		c.JSON(readStatusForErr(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ride": ride})
}

// CancelByCustomer POST /api/rides/:id/cancel-by-customer — 乘客 App 端取消，
// 複用 DispatchService 的取消核心，釋放搶單鎖、司機回待命、通知雙方。
func (h *RideHandler) CancelByCustomer(c *gin.Context) {
	rideID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id 格式錯誤"})
		return
	}
	customerID := middleware.CustomerIDFromCtx(c)
	msg, err := h.dispatch.CancelByCustomerID(c.Request.Context(), customerID, rideID)
	if err != nil {
		c.JSON(readStatusForErr(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": msg})
}
