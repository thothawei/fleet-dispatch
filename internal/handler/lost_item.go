package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"line-fleet-dispatch/internal/events"
	"line-fleet-dispatch/internal/middleware"
	"line-fleet-dispatch/internal/model"
	"line-fleet-dispatch/internal/service"
)

// LostItemHandler 遺失物協尋。
type LostItemHandler struct {
	items *service.LostItemService
}

func NewLostItemHandler(items *service.LostItemService) *LostItemHandler {
	return &LostItemHandler{items: items}
}

// CreateByCustomer POST /api/rides/:id/lost-items（乘客 JWT）
// 對自己的已完成行程建立協尋單；回應含依當下處理費%快照的 fee_cents。
func (h *LostItemHandler) CreateByCustomer(c *gin.Context) {
	rideID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id 格式錯誤"})
		return
	}
	var req struct {
		Description string `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "參數錯誤"})
		return
	}
	item, err := h.items.CreateByCustomer(middleware.CustomerIDFromCtx(c), rideID, req.Description)
	if err != nil {
		c.JSON(lostItemStatusForErr(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"lost_item": item})
}

// GetByRide GET /api/rides/:id/lost-items（MultiAuth：本趟乘客/司機、admin）
// 回傳該行程最新一張協尋單；從未建立過則 lost_item 為 null。
func (h *LostItemHandler) GetByRide(c *gin.Context) {
	rideID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id 格式錯誤"})
		return
	}
	item, err := h.items.GetForRide(middleware.RoleFromCtx(c), middleware.SubjectIDFromCtx(c), rideID)
	if err != nil {
		c.JSON(readStatusForErr(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"lost_item": item})
}

// ListByDriver GET /api/driver/lost-items（司機 JWT）：未結案協尋工作清單。
func (h *LostItemHandler) ListByDriver(c *gin.Context) {
	items, err := h.items.ListByDriver(middleware.DriverIDFromCtx(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"lost_items": items})
}

// ListByCustomer GET /api/customer/lost-items（乘客 JWT）：未結案協尋清單。
func (h *LostItemHandler) ListByCustomer(c *gin.Context) {
	items, err := h.items.ListByCustomer(middleware.CustomerIDFromCtx(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"lost_items": items})
}

// MarkFound POST /api/lost-items/:id/found（司機 JWT）：標記已尋獲。
func (h *LostItemHandler) MarkFound(c *gin.Context) {
	h.transitionByDriver(c, h.items.MarkFound)
}

// MarkReturned POST /api/lost-items/:id/return（司機 JWT）：付訖後標記已歸還。
func (h *LostItemHandler) MarkReturned(c *gin.Context) {
	h.transitionByDriver(c, h.items.MarkReturned)
}

// Pay POST /api/lost-items/:id/pay（乘客 JWT）：支付處理費（記帳式確認）。
func (h *LostItemHandler) Pay(c *gin.Context) {
	itemID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id 格式錯誤"})
		return
	}
	item, err := h.items.Pay(middleware.CustomerIDFromCtx(c), itemID)
	if err != nil {
		c.JSON(lostItemStatusForErr(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"lost_item": item})
}

// Close POST /api/lost-items/:id/close（MultiAuth：本單乘客/司機）：未尋獲或取消結案。
func (h *LostItemHandler) Close(c *gin.Context) {
	itemID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id 格式錯誤"})
		return
	}
	role := middleware.RoleFromCtx(c)
	if role != events.RoleCustomer && role != events.RoleDriver {
		c.JSON(http.StatusForbidden, gin.H{"error": "僅協尋單的乘客或司機可結案"})
		return
	}
	item, err := h.items.Close(role, middleware.SubjectIDFromCtx(c), itemID)
	if err != nil {
		c.JSON(lostItemStatusForErr(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"lost_item": item})
}

func (h *LostItemHandler) transitionByDriver(c *gin.Context, fn func(driverID, itemID int64) (*model.LostItemRequest, error)) {
	itemID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id 格式錯誤"})
		return
	}
	item, err := fn(middleware.DriverIDFromCtx(c), itemID)
	if err != nil {
		c.JSON(lostItemStatusForErr(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"lost_item": item})
}

// lostItemStatusForErr 協尋錯誤對應 HTTP 狀態碼。
func lostItemStatusForErr(err error) int {
	switch {
	case errors.Is(err, service.ErrEmptyDescription),
		errors.Is(err, service.ErrDescriptionTooLong),
		errors.Is(err, service.ErrRideNotCompleted):
		return http.StatusBadRequest
	case errors.Is(err, service.ErrLostItemExists),
		errors.Is(err, service.ErrBadLostItemState):
		return http.StatusConflict
	default:
		return readStatusForErr(err)
	}
}
