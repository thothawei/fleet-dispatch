package handler

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/middleware"
	"line-fleet-dispatch/internal/service"
)

// ScheduledRideHandler 預約行程（乘客端）。
type ScheduledRideHandler struct {
	schedules *service.ScheduledRideService
}

func NewScheduledRideHandler(schedules *service.ScheduledRideService) *ScheduledRideHandler {
	return &ScheduledRideHandler{schedules: schedules}
}

// List GET /api/customer/scheduled-rides?upcoming=1（乘客 JWT）。
// upcoming=1 只回還沒轉單的預約（首頁那張卡用）；預設回全部（預約紀錄頁用）。
func (h *ScheduledRideHandler) List(c *gin.Context) {
	upcoming := c.Query("upcoming") == "1"
	rows, err := h.schedules.List(middleware.CustomerIDFromCtx(c), upcoming)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"scheduled_rides": rows,
		// 把兩個時間參數一起回給 App。
		//
		// lead_minutes：「我們會提前這麼久幫你找車」這句話要跟後端的實際行為一致。
		// min_lead_minutes：App 的時間選擇器要用它擋下太近的預約——**寫死在 App 端的話**，
		// 後端把門檻調高之後，App 仍會讓乘客選一個註定被 400 拒絕的時間，
		// 而他要填完整張表才會知道。
		"lead_minutes":     constants.ScheduledRideLeadMinutes,
		"min_lead_minutes": constants.ScheduledRideMinLeadMinutes,
	})
}

// Get GET /api/customer/scheduled-rides/:id（乘客 JWT）。
func (h *ScheduledRideHandler) Get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id 格式錯誤"})
		return
	}
	row, err := h.schedules.Get(middleware.CustomerIDFromCtx(c), id)
	if err != nil {
		c.JSON(scheduledRideStatusForErr(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"scheduled_ride": row})
}

// Create POST /api/customer/scheduled-rides（乘客 JWT）：建立預約。
func (h *ScheduledRideHandler) Create(c *gin.Context) {
	var body struct {
		ScheduledAt         time.Time `json:"scheduled_at"`
		PickupLat           float64   `json:"pickup_lat"`
		PickupLng           float64   `json:"pickup_lng"`
		PickupAddress       string    `json:"pickup_address"`
		DropoffAddress      string    `json:"dropoff_address"`
		DropoffLat          *float64  `json:"dropoff_lat"`
		DropoffLng          *float64  `json:"dropoff_lng"`
		RequiredVehicleType string    `json:"required_vehicle_type"`
		Note                string    `json:"note"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "參數錯誤"})
		return
	}
	row, err := h.schedules.Create(middleware.CustomerIDFromCtx(c), service.ScheduleRideInput{
		ScheduledAt:         body.ScheduledAt,
		PickupLat:           body.PickupLat,
		PickupLng:           body.PickupLng,
		PickupAddress:       body.PickupAddress,
		DropoffAddress:      body.DropoffAddress,
		DropoffLat:          body.DropoffLat,
		DropoffLng:          body.DropoffLng,
		RequiredVehicleType: body.RequiredVehicleType,
		Note:                body.Note,
	})
	if err != nil {
		c.JSON(scheduledRideStatusForErr(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"scheduled_ride":   row,
		"lead_minutes":     constants.ScheduledRideLeadMinutes,
		"min_lead_minutes": constants.ScheduledRideMinLeadMinutes,
	})
}

// Cancel POST /api/customer/scheduled-rides/:id/cancel（乘客 JWT）。
//
// 已轉單的回 409＋該筆預約現況——App 據此把畫面換成「已轉為訂單」並引導去取消訂單，
// 而不是顯示「取消失敗，請稍後再試」（那句話會讓乘客一直按，訂單卻照樣派出去）。
func (h *ScheduledRideHandler) Cancel(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id 格式錯誤"})
		return
	}
	customerID := middleware.CustomerIDFromCtx(c)
	row, err := h.schedules.Cancel(customerID, id)
	if err != nil {
		status := scheduledRideStatusForErr(err)
		payload := gin.H{"error": err.Error()}
		if errors.Is(err, service.ErrScheduleNotCancellable) {
			if latest, gerr := h.schedules.Get(customerID, id); gerr == nil {
				payload["scheduled_ride"] = latest
			}
		}
		c.JSON(status, payload)
		return
	}
	c.JSON(http.StatusOK, gin.H{"scheduled_ride": row})
}

func scheduledRideStatusForErr(err error) int {
	switch {
	case errors.Is(err, service.ErrScheduleTooSoon),
		errors.Is(err, service.ErrScheduleTooFar),
		errors.Is(err, service.ErrScheduleNoteTooLong),
		errors.Is(err, service.ErrInvalidCoords),
		errors.Is(err, service.ErrInvalidVehicleType):
		return http.StatusBadRequest
	case errors.Is(err, service.ErrTooManySchedules),
		errors.Is(err, service.ErrScheduleNotCancellable):
		return http.StatusConflict
	case errors.Is(err, service.ErrNotFound):
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}
