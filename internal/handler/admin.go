package handler

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"line-fleet-dispatch/internal/auth"
	"line-fleet-dispatch/internal/middleware"
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
	adminRepo        *repository.AdminRepository
	adminUsers       *service.AdminUsers
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
	adminRepo *repository.AdminRepository,
	adminUsers *service.AdminUsers,
	redis *redisstore.Store,
	jwtSecret string,
	jwtExpiryHours int,
) *AdminHandler {
	return &AdminHandler{
		admins: admins, adminOps: adminOps, dispatchSettings: dispatchSettings,
		drivers: drivers, rides: rides, tracks: tracks, rideEvents: rideEvents,
		reports: reports, adminRepo: adminRepo, adminUsers: adminUsers,
		redis: redis, jwtSecret: jwtSecret, jwtExpiryHours: jwtExpiryHours,
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
	if !admin.IsActive {
		c.JSON(http.StatusForbidden, gin.H{"error": "帳號已停用"})
		return
	}
	token, err := auth.GenerateToken("admin", admin.ID, h.jwtSecret, time.Duration(h.jwtExpiryHours)*time.Hour)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "簽發 token 失敗"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"admin_id": admin.ID, "name": admin.Name, "role": admin.Role, "token": token})
}

// Me GET /api/admin/me：回登入者自己的身分與角色
func (h *AdminHandler) Me(c *gin.Context) {
	id := middleware.AdminIDFromCtx(c)
	a, err := h.adminRepo.FindByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "帳號不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": a.ID, "username": a.Username, "role": a.Role})
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

// ListAdmins GET /api/admin/admins
func (h *AdminHandler) ListAdmins(c *gin.Context) {
	list, err := h.adminUsers.List()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查詢失敗"})
		return
	}
	out := make([]gin.H, 0, len(list))
	for _, a := range list {
		out = append(out, gin.H{"id": a.ID, "username": a.Username, "role": a.Role,
			"is_active": a.IsActive, "created_at": a.CreatedAt})
	}
	c.JSON(http.StatusOK, gin.H{"admins": out})
}

// CreateAdmin POST /api/admin/admins
func (h *AdminHandler) CreateAdmin(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required,min=6"`
		Role     string `json:"role" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "參數錯誤"})
		return
	}
	a, err := h.adminUsers.Create(req.Username, req.Password, req.Role)
	if err != nil {
		if errors.Is(err, service.ErrBadRole) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		// 避免把底層 Postgres 錯誤（如 unique violation 原始文字）洩漏給前端
		c.JSON(http.StatusBadRequest, gin.H{"error": "建立失敗（帳號可能已存在）"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": a.ID, "username": a.Username, "role": a.Role})
}

// UpdateAdmin PATCH /api/admin/admins/:id
func (h *AdminHandler) UpdateAdmin(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id 錯誤"})
		return
	}
	var req struct {
		Role     *string `json:"role"`
		Password *string `json:"password"`
		IsActive *bool   `json:"is_active"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "參數錯誤"})
		return
	}
	actorID := middleware.AdminIDFromCtx(c)
	if err := h.adminUsers.Update(actorID, id, req.Role, req.Password, req.IsActive); err != nil {
		switch {
		case errors.Is(err, service.ErrNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "帳號不存在"})
		case errors.Is(err, service.ErrSelfLockout), errors.Is(err, service.ErrLastSuperadmin), errors.Is(err, service.ErrBadRole):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "更新失敗"})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
