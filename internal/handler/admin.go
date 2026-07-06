package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"line-fleet-dispatch/internal/auth"
	redisstore "line-fleet-dispatch/internal/redis"
	"line-fleet-dispatch/internal/repository"
	"line-fleet-dispatch/internal/service"
)

// AdminHandler 後台唯讀 API + 登入
type AdminHandler struct {
	admins         *service.AdminRegistry
	drivers        *repository.DriverRepository
	rides          *repository.RideRepository
	tracks         *repository.TrackRepository
	reports        *repository.ReportRepository
	redis          *redisstore.Store
	jwtSecret      string
	jwtExpiryHours int
}

func NewAdminHandler(
	admins *service.AdminRegistry,
	drivers *repository.DriverRepository,
	rides *repository.RideRepository,
	tracks *repository.TrackRepository,
	reports *repository.ReportRepository,
	redis *redisstore.Store,
	jwtSecret string,
	jwtExpiryHours int,
) *AdminHandler {
	return &AdminHandler{
		admins: admins, drivers: drivers, rides: rides, tracks: tracks,
		reports: reports, redis: redis, jwtSecret: jwtSecret, jwtExpiryHours: jwtExpiryHours,
	}
}

// Login POST /api/admin/login
func (h *AdminHandler) Login(c *gin.Context) {
	var req struct {
		Email    string `json:"email" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "參數錯誤"})
		return
	}
	admin, err := h.admins.Login(c.Request.Context(), req.Email, req.Password)
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
	c.JSON(http.StatusOK, gin.H{"ride": ride, "track_geojson": geojson})
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
