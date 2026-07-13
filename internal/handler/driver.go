package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"line-fleet-dispatch/internal/auth"
	"line-fleet-dispatch/internal/middleware"
	"line-fleet-dispatch/internal/model"
	"line-fleet-dispatch/internal/repository"
	"line-fleet-dispatch/internal/service"
)

// DriverHandler 司機 API
type DriverHandler struct {
	tracking       *service.TrackingService
	drivers        *service.DriverRegistry
	rides          *service.RideQueryService
	reports        *repository.ReportRepository
	feeSettings    *service.FeeSettings
	jwtSecret      string
	jwtExpiryHours int
}

// SetEarnings 注入收入查詢所需的報表 repo 與費率設定（供 /driver/earnings）；可選。
func (h *DriverHandler) SetEarnings(reports *repository.ReportRepository, fees *service.FeeSettings) {
	h.reports = reports
	h.feeSettings = fees
}

func NewDriverHandler(
	tracking *service.TrackingService,
	drivers *service.DriverRegistry,
	rides *service.RideQueryService,
	jwtSecret string,
	jwtExpiryHours int,
) *DriverHandler {
	return &DriverHandler{
		tracking:       tracking,
		drivers:        drivers,
		rides:          rides,
		jwtSecret:      jwtSecret,
		jwtExpiryHours: jwtExpiryHours,
	}
}

// driverPublic 回傳司機可公開的個資（不含密碼雜湊）
func driverPublic(d *model.Driver) gin.H {
	return gin.H{
		"driver_id": d.ID,
		"name":      d.Name,
		"phone":     d.Phone,
		"status":    d.Status,
	}
}

// Me GET /api/driver/me — 司機個資與目前狀態（App 首頁顯示，取代信任本地）
func (h *DriverHandler) Me(c *gin.Context) {
	driverID := middleware.DriverIDFromCtx(c)
	d, err := h.drivers.Me(driverID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "找不到司機"})
		return
	}
	c.JSON(http.StatusOK, driverPublic(d))
}

// Earnings GET /api/driver/earnings?month=2026-07 — 司機當月收入（F7）
// 回傳趟數、營業額、手續費、司機實得、月會費、應付總公司（皆為「分」）。
func (h *DriverHandler) Earnings(c *gin.Context) {
	if h.reports == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "收入查詢未啟用"})
		return
	}
	month := c.Query("month")
	if month == "" {
		month = time.Now().Format("2006-01")
	}
	if _, err := time.Parse("2006-01", month); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "month 需為 YYYY-MM 格式"})
		return
	}
	driverID := middleware.DriverIDFromCtx(c)
	e, err := h.reports.DriverMonthlyEarnings(driverID, month)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// 會費以 membership_invoices 帳本快照為單一真源（repo 讀出），不再用即時費率——
	// 未產生帳單者為 0；應付總公司 = 手續費 + 帳本會費。與月報表 F6 同源、不會分歧。
	c.JSON(http.StatusOK, gin.H{
		"month":                  month,
		"trip_count":             e.TripCount,
		"total_revenue_cents":    e.TotalRevenueCents,
		"total_commission_cents": e.TotalCommissionCents,
		"driver_net_cents":       e.DriverNetCents,
		"membership_fee_cents":   e.MembershipFeeCents,
		"owed_to_hq_cents":       e.TotalCommissionCents + e.MembershipFeeCents,
	})
}

// Online POST /api/driver/online — 顯式上線（設為待命，重新進入派單池）
func (h *DriverHandler) Online(c *gin.Context) {
	driverID := middleware.DriverIDFromCtx(c)
	d, err := h.drivers.GoOnline(driverID)
	if err != nil {
		if errors.Is(err, service.ErrDriverDisabled) {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, driverPublic(d))
}

// Offline POST /api/driver/offline — 顯式下線（乾淨移出派單池）；載客中回 409
func (h *DriverHandler) Offline(c *gin.Context) {
	driverID := middleware.DriverIDFromCtx(c)
	d, err := h.drivers.GoOffline(driverID)
	if err != nil {
		if errors.Is(err, service.ErrDriverOnTrip) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, driverPublic(d))
}

// ActiveRide GET /api/driver/rides/active — 司機當前進行中訂單（App 中途重啟恢復行程）。
// 無進行中訂單時回 {"ride": null}，非錯誤。
func (h *DriverHandler) ActiveRide(c *gin.Context) {
	driverID := middleware.DriverIDFromCtx(c)
	ride, err := h.rides.GetActiveRideByDriver(driverID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ride": ride})
}

func (h *DriverHandler) issueToken(driverID int64) (string, error) {
	return auth.GenerateDriverToken(driverID, h.jwtSecret, time.Duration(h.jwtExpiryHours)*time.Hour)
}

type locationRequest struct {
	Lat float64 `json:"lat" binding:"required"`
	Lng float64 `json:"lng" binding:"required"`
}

// ReportLocation POST /api/driver/location（需 JWT，driver_id 取自 token）
func (h *DriverHandler) ReportLocation(c *gin.Context) {
	driverID := middleware.DriverIDFromCtx(c)
	var req locationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "參數錯誤"})
		return
	}

	if err := h.tracking.ReportDriverLocation(c.Request.Context(), driverID, req.Lat, req.Lng); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Register POST /api/driver/register（設定密碼並回傳 token）
func (h *DriverHandler) Register(c *gin.Context) {
	var req struct {
		Name       string `json:"name" binding:"required"`
		LineUserID string `json:"line_user_id" binding:"required"`
		Password   string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "參數錯誤"})
		return
	}
	driver, err := h.drivers.Register(c.Request.Context(), req.LineUserID, req.Name, req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	token, err := h.issueToken(driver.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "簽發 token 失敗"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"driver_id": driver.ID, "name": driver.Name, "token": token})
}

// Login POST /api/driver/login（line_user_id + 密碼換 token）
func (h *DriverHandler) Login(c *gin.Context) {
	var req struct {
		LineUserID string `json:"line_user_id" binding:"required"`
		Password   string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "參數錯誤"})
		return
	}
	driver, err := h.drivers.Login(c.Request.Context(), req.LineUserID, req.Password)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}
	token, err := h.issueToken(driver.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "簽發 token 失敗"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"driver_id": driver.ID, "token": token})
}
