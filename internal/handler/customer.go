package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"line-fleet-dispatch/internal/auth"
	"line-fleet-dispatch/internal/service"
)

// CustomerHandler 乘客註冊與登入
type CustomerHandler struct {
	customers      *service.CustomerRegistry
	feeSettings    *service.FeeSettings
	jwtSecret      string
	jwtExpiryHours int
}

func NewCustomerHandler(customers *service.CustomerRegistry, jwtSecret string, jwtExpiryHours int) *CustomerHandler {
	return &CustomerHandler{customers: customers, jwtSecret: jwtSecret, jwtExpiryHours: jwtExpiryHours}
}

// SetFeeSettings 注入費率設定（供 GET /api/customer/fees）；可選，比照 AdminHandler.SetFeeSettings。
func (h *CustomerHandler) SetFeeSettings(fees *service.FeeSettings) {
	h.feeSettings = fees
}

// Fees GET /api/customer/fees（customer JWT，唯讀）——乘客該知道的費率（P5）。
// 乘客在「選擇車種」UI 呼叫它，選寵物用車的當下就顯示「將加收清潔費 X%」，
// 而不是等行程完成才知道。
//
// **白名單輸出**：回 FeeSettings.CustomerJSON()，不是把 admin 的 JSON() 過濾——
// commission_bps（手續費）與 monthly_membership_fee_cents（月會費）是內部費率，絕不可外洩給乘客。
// 讀記憶體快取（與 F4 同源），無額外 DB 負擔。
func (h *CustomerHandler) Fees(c *gin.Context) {
	if h.feeSettings == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "費率查詢未啟用"})
		return
	}
	c.JSON(http.StatusOK, h.feeSettings.CustomerJSON())
}

func (h *CustomerHandler) issueToken(id int64) (string, error) {
	return auth.GenerateToken("customer", id, h.jwtSecret, time.Duration(h.jwtExpiryHours)*time.Hour)
}

// Register POST /api/customer/register
func (h *CustomerHandler) Register(c *gin.Context) {
	var req struct {
		LineUserID string `json:"line_user_id" binding:"required"`
		Name       string `json:"name"`
		Password   string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "參數錯誤"})
		return
	}
	customer, err := h.customers.Register(c.Request.Context(), req.LineUserID, req.Name, req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	token, err := h.issueToken(customer.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "簽發 token 失敗"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"customer_id": customer.ID, "name": customer.Name, "token": token})
}

// Login POST /api/customer/login
func (h *CustomerHandler) Login(c *gin.Context) {
	var req struct {
		LineUserID string `json:"line_user_id" binding:"required"`
		Password   string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "參數錯誤"})
		return
	}
	customer, err := h.customers.Login(c.Request.Context(), req.LineUserID, req.Password)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}
	token, err := h.issueToken(customer.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "簽發 token 失敗"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"customer_id": customer.ID, "token": token})
}
