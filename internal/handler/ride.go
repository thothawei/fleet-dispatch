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
	stops       *service.RideStopService
}

func NewRideHandler(
	dispatch *service.DispatchService,
	tracking *service.TrackingService,
	rides *service.RideQueryService,
	rideService *service.RideService,
) *RideHandler {
	return &RideHandler{dispatch: dispatch, tracking: tracking, rides: rides, rideService: rideService}
}

// SetStops 注入停靠點服務（供 N7 的到達／跳過標記）；可選。
func (h *RideHandler) SetStops(stops *service.RideStopService) {
	h.stops = stops
}

// stopStatusForErr 停靠點標記的錯誤對應。
func stopStatusForErr(err error) int {
	switch {
	case errors.Is(err, service.ErrForbidden):
		return http.StatusForbidden
	case errors.Is(err, service.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, service.ErrStopAlreadyHandled), errors.Is(err, service.ErrBadStopState):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

// ArriveStop POST /api/rides/:id/stops/:stop_id/arrive — 司機標記已到達該停靠點（N7）。
// 重複標記或該站已跳過回 409（到達時間是計費與稽核的原始資料，不覆寫）。
func (h *RideHandler) ArriveStop(c *gin.Context) {
	h.markStop(c, func(driverID, rideID, stopID int64) error {
		return h.stops.MarkArrived(driverID, rideID, stopID)
	})
}

// SkipStop POST /api/rides/:id/stops/:stop_id/skip — 乘客未出現，司機跳過該停靠點（N7）。
// 被跳過的站不計入 N5 的計費路線——沒去就沒開那段路。
func (h *RideHandler) SkipStop(c *gin.Context) {
	h.markStop(c, func(driverID, rideID, stopID int64) error {
		return h.stops.MarkSkipped(driverID, rideID, stopID)
	})
}

func (h *RideHandler) markStop(c *gin.Context, do func(driverID, rideID, stopID int64) error) {
	if h.stops == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "多停靠點行程未啟用"})
		return
	}
	rideID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id 格式錯誤"})
		return
	}
	stopID, err := strconv.ParseInt(c.Param("stop_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "stop_id 格式錯誤"})
		return
	}
	driverID := middleware.DriverIDFromCtx(c)
	if err := do(driverID, rideID, stopID); err != nil {
		c.JSON(stopStatusForErr(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
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
	result, err := h.tracking.PickUp(c.Request.Context(), rideID, driverID)
	if err != nil {
		c.JSON(statusForErr(err), gin.H{"error": err.Error()})
		return
	}
	resp := gin.H{"ok": true}
	if result.DropoffAddress != "" {
		resp["dropoff_address"] = result.DropoffAddress
	}
	if result.HasDropoffPoint {
		resp["dropoff_lat"] = result.DropoffLat
		resp["dropoff_lng"] = result.DropoffLng
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
		// 選填（P2）：未帶＝不指定車種，維持現行行為，既有 App／LINE 建單不受影響。
		RequiredVehicleType string `json:"required_vehicle_type"`
		// 選填（N3）：多乘客／多停靠點行程。未帶＝傳統單點訂單。
		// 有帶時 pickup/dropoff 由 stops 推導（第一個 pickup／最終 dropoff），
		// 此時上面的 pickup_lat/lng 等欄位會被忽略。
		Stops []struct {
			Seq            int     `json:"seq"`
			Kind           string  `json:"kind"` // pickup / dropoff
			Lat            float64 `json:"lat"`
			Lng            float64 `json:"lng"`
			Address        string  `json:"address"`
			PassengerLabel string  `json:"passenger_label"`
		} `json:"stops"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "參數錯誤"})
		return
	}
	stops := make([]service.StopInput, 0, len(req.Stops))
	for _, s := range req.Stops {
		stops = append(stops, service.StopInput{
			Seq:            s.Seq,
			Kind:           s.Kind,
			Lat:            s.Lat,
			Lng:            s.Lng,
			Address:        s.Address,
			PassengerLabel: s.PassengerLabel,
		})
	}
	ride, err := h.rideService.CreateByCustomer(
		c.Request.Context(), customerID, service.CustomerCreateRequest{
			PickupLat:           req.PickupLat,
			PickupLng:           req.PickupLng,
			PickupAddress:       req.PickupAddress,
			DropoffAddress:      req.DropoffAddress,
			DropoffLat:          req.DropoffLat,
			DropoffLng:          req.DropoffLng,
			RequiredVehicleType: req.RequiredVehicleType,
			Stops:               stops,
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
	case errors.Is(err, service.ErrInvalidCoords), errors.Is(err, service.ErrInvalidVehicleType),
		// 停靠點填錯是乘客的輸入問題（N2）→ 400，錯誤訊息本身已說明錯在哪。
		errors.Is(err, service.ErrTooManyStops), errors.Is(err, service.ErrTooManyPassengers),
		errors.Is(err, service.ErrInvalidStopKind), errors.Is(err, service.ErrDuplicateStopSeq),
		errors.Is(err, service.ErrUnpairedStop), errors.Is(err, service.ErrDropoffBeforePickup),
		errors.Is(err, service.ErrMissingPassenger):
		return http.StatusBadRequest
	case errors.Is(err, service.ErrStopsUnavailable):
		// 這是伺服器沒接好，不是乘客填錯——不可回 400 讓乘客以為是自己的問題。
		return http.StatusServiceUnavailable
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
