package handler

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"line-fleet-dispatch/internal/auth"
	"line-fleet-dispatch/internal/model"
	redisstore "line-fleet-dispatch/internal/redis"
	"line-fleet-dispatch/internal/repository"
	"line-fleet-dispatch/internal/service"
)

// AdminHandler 後台 API + 登入
type AdminHandler struct {
	admins           *service.AdminRegistry
	adminOps         *service.AdminOperations
	dispatchSettings *service.DispatchSettings
	drivers          *repository.DriverRepository
	rides            *repository.RideRepository
	tracks           *repository.TrackRepository
	rideEvents       *repository.RideEventRepository
	reports          *repository.ReportRepository
	redis            *redisstore.Store
	jwtSecret        string
	jwtExpiryHours   int
}

func NewAdminHandler(
	admins *service.AdminRegistry,
	adminOps *service.AdminOperations,
	dispatchSettings *service.DispatchSettings,
	drivers *repository.DriverRepository,
	rides *repository.RideRepository,
	tracks *repository.TrackRepository,
	rideEvents *repository.RideEventRepository,
	reports *repository.ReportRepository,
	redis *redisstore.Store,
	jwtSecret string,
	jwtExpiryHours int,
) *AdminHandler {
	return &AdminHandler{
		admins: admins, adminOps: adminOps, dispatchSettings: dispatchSettings,
		drivers: drivers, rides: rides, tracks: tracks, rideEvents: rideEvents,
		reports: reports, redis: redis, jwtSecret: jwtSecret, jwtExpiryHours: jwtExpiryHours,
	}
}

// Login POST /api/admin/login
func (h *AdminHandler) Login(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "參數錯誤"})
		return
	}
	admin, err := h.admins.Login(c.Request.Context(), req.Username, req.Password)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}
	token, err := auth.GenerateToken("admin", admin.ID, h.jwtSecret, time.Duration(h.jwtExpiryHours)*time.Hour)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "簽發 token 失敗"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"admin_id": admin.ID, "name": admin.Name, "token": token})
}

// Fleet GET /api/admin/fleet：即時在線司機座標快照
func (h *AdminHandler) Fleet(c *gin.Context) {
	locs, err := h.redis.OnlineDriverLocations(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"drivers": locs})
}

// Drivers GET /api/admin/drivers：司機列表
func (h *AdminHandler) Drivers(c *gin.Context) {
	drivers, err := h.drivers.ListAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"drivers": drivers})
}

// Rides GET /api/admin/rides?status=0&limit=100：訂單列表
func (h *AdminHandler) Rides(c *gin.Context) {
	var statusPtr *int16
	if s := c.Query("status"); s != "" {
		if n, err := strconv.ParseInt(s, 10, 16); err == nil {
			v := int16(n)
			statusPtr = &v
		}
	}
	limit := 100
	if l := c.Query("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil {
			limit = n
		}
	}
	rows, err := h.rides.ListRecent(statusPtr, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"rides": rows})
}

// RideDetail GET /api/admin/rides/:id：單筆訂單 + 軌跡 GeoJSON
func (h *AdminHandler) RideDetail(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id 格式錯誤"})
		return
	}
	ride, err := h.rides.GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "找不到訂單"})
		return
	}
	geojson, err := h.tracks.GeoJSON(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var evts []model.RideEvent
	if h.rideEvents != nil {
		evts, err = h.rideEvents.ListByRideID(id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	if evts == nil {
		evts = []model.RideEvent{}
	}
	c.JSON(http.StatusOK, gin.H{"ride": ride, "track_geojson": geojson, "events": evts})
}

// DailyReport GET /api/admin/reports/daily?date=2026-07-06
func (h *AdminHandler) DailyReport(c *gin.Context) {
	date := c.Query("date")
	if date == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "請提供 date 參數"})
		return
	}
	rows, err := h.reports.DailyDriverStats(date)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"date": date, "drivers": rows})
}

// PatchDriverStatus PATCH /api/admin/drivers/:id/status — 啟用/停用司機
func (h *AdminHandler) PatchDriverStatus(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id 格式錯誤"})
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "參數錯誤"})
		return
	}
	driver, err := h.adminOps.SetDriverEnabled(c.Request.Context(), id, req.Enabled)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "找不到司機"})
		case errors.Is(err, service.ErrDriverOnTrip):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"driver": driver})
}

// GetDispatchSettings GET /api/admin/settings/dispatch
func (h *AdminHandler) GetDispatchSettings(c *gin.Context) {
	c.JSON(http.StatusOK, h.dispatchSettings.JSON())
}

// PutDispatchSettings PUT /api/admin/settings/dispatch
func (h *AdminHandler) PutDispatchSettings(c *gin.Context) {
	var req struct {
		RadiusM         *int `json:"radius_m"`
		MaxDrivers      *int `json:"max_drivers"`
		OfferTimeoutSec *int `json:"offer_timeout_sec"`
		MaxAttempts     *int `json:"max_attempts"`
		RateLimitPerMin *int `json:"rate_limit_per_min"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "參數錯誤"})
		return
	}
	if err := h.dispatchSettings.Update(req.RadiusM, req.MaxDrivers, req.OfferTimeoutSec, req.MaxAttempts, req.RateLimitPerMin); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, h.dispatchSettings.JSON())
}

// CancelRide POST /api/admin/rides/:id/cancel — 後台強制取消
func (h *AdminHandler) CancelRide(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id 格式錯誤"})
		return
	}
	msg, err := h.adminOps.CancelRideByAdmin(c.Request.Context(), id)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "找不到訂單"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": msg})
}
