package handler

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
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
	feeSettings      *service.FeeSettings
	redis            *redisstore.Store
	jwtSecret        string
	jwtExpiryHours   int
}

// SetFeeSettings 注入費率設定服務（供 /settings/fees 讀寫）；可選。
func (h *AdminHandler) SetFeeSettings(fs *service.FeeSettings) {
	h.feeSettings = fs
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

const rideListDateLayout = "2006-01-02"

// parseRideListFilter 解析 /api/admin/rides 的 query string。
// 抽成純函式（不吃 *gin.Context）以便單元測試，不必起 DB。
func parseRideListFilter(q url.Values) (repository.RideListFilter, error) {
	f := repository.RideListFilter{
		Q:    strings.TrimSpace(q.Get("q")),
		From: q.Get("from"),
		To:   q.Get("to"),
	}

	if s := q.Get("status"); s != "" {
		n, err := strconv.ParseInt(s, 10, 16)
		if err != nil {
			return f, fmt.Errorf("status 需為整數：%q", s)
		}
		v := int16(n)
		f.Status = &v
	}

	if l := q.Get("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil {
			return f, fmt.Errorf("limit 需為整數：%q", l)
		}
		if n <= 0 || n > repository.RideListMaxLimit {
			return f, fmt.Errorf("limit 需介於 1 與 %d 之間", repository.RideListMaxLimit)
		}
		f.Limit = n
	}

	if o := q.Get("offset"); o != "" {
		n, err := strconv.Atoi(o)
		if err != nil {
			return f, fmt.Errorf("offset 需為整數：%q", o)
		}
		if n < 0 {
			return f, fmt.Errorf("offset 不可為負數")
		}
		f.Offset = n
	}

	if f.From != "" {
		if _, err := time.Parse(rideListDateLayout, f.From); err != nil {
			return f, fmt.Errorf("from 需為 YYYY-MM-DD 格式：%q", f.From)
		}
	}
	if f.To != "" {
		if _, err := time.Parse(rideListDateLayout, f.To); err != nil {
			return f, fmt.Errorf("to 需為 YYYY-MM-DD 格式：%q", f.To)
		}
	}
	// 兩者都是 YYYY-MM-DD 定長格式，字串比較等同日期比較
	if f.From != "" && f.To != "" && f.From > f.To {
		return f, fmt.Errorf("from 不可晚於 to")
	}

	return f, nil
}

// Rides GET /api/admin/rides?status=0&limit=100&offset=0&from=2026-07-01&to=2026-07-10&q=台北
// 訂單列表：可依狀態、requested_at 日期區間、上車點/訂單 ID 關鍵字篩選並分頁。
// 回應的 total 是符合條件的總筆數（不受 limit/offset 影響）。
func (h *AdminHandler) Rides(c *gin.Context) {
	f, err := parseRideListFilter(c.Request.URL.Query())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	rows, total, err := h.rides.List(f)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	limit := f.Limit
	if limit <= 0 {
		limit = repository.RideListDefaultLimit
	}
	if rows == nil {
		rows = []repository.AdminRideRow{} // 空結果回 []，不要讓 nil slice 序列化成 null
	}
	c.JSON(http.StatusOK, gin.H{
		"rides":  rows,
		"total":  total,
		"limit":  limit,
		"offset": f.Offset,
	})
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

const reportMonthLayout = "2006-01"

// MonthlyReport GET /api/admin/reports/monthly?month=2026-07
// 月營運報表：每司機營業額/手續費/司機實得，並補上月會費與「應付總公司」。
func (h *AdminHandler) MonthlyReport(c *gin.Context) {
	month := c.Query("month")
	if month == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "請提供 month 參數（YYYY-MM）"})
		return
	}
	if _, err := time.Parse(reportMonthLayout, month); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "month 需為 YYYY-MM 格式"})
		return
	}
	rows, err := h.reports.MonthlyDriverStats(month)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// 會費：當月有完成行程的司機各計一筆月會費；應付總公司 = 手續費 + 月會費。
	var membership int64
	if h.feeSettings != nil {
		membership = h.feeSettings.MonthlyMembershipFeeCents()
	}
	for i := range rows {
		rows[i].MembershipFeeCents = membership
		rows[i].OwedToHqCents = rows[i].TotalCommissionCents + membership
	}
	if rows == nil {
		rows = []repository.MonthlyDriverReport{}
	}
	c.JSON(http.StatusOK, gin.H{"month": month, "drivers": rows})
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

// GetFeeSettings GET /api/admin/settings/fees（superadmin）
func (h *AdminHandler) GetFeeSettings(c *gin.Context) {
	if h.feeSettings == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "費率設定未啟用"})
		return
	}
	c.JSON(http.StatusOK, h.feeSettings.JSON())
}

// PutFeeSettings PUT /api/admin/settings/fees（superadmin）
func (h *AdminHandler) PutFeeSettings(c *gin.Context) {
	if h.feeSettings == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "費率設定未啟用"})
		return
	}
	var req struct {
		BaseFareCents             *int64 `json:"base_fare_cents"`
		PerKmFareCents            *int64 `json:"per_km_fare_cents"`
		MinFareCents              *int64 `json:"min_fare_cents"`
		CommissionBps             *int64 `json:"commission_bps"`
		MonthlyMembershipFeeCents *int64 `json:"monthly_membership_fee_cents"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "參數錯誤"})
		return
	}
	actorID := middleware.AdminIDFromCtx(c)
	if err := h.feeSettings.Update(req.BaseFareCents, req.PerKmFareCents, req.MinFareCents,
		req.CommissionBps, req.MonthlyMembershipFeeCents, &actorID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, h.feeSettings.JSON())
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
