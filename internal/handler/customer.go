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
	jwtSecret      string
	jwtExpiryHours int
}

func NewCustomerHandler(customers *service.CustomerRegistry, jwtSecret string, jwtExpiryHours int) *CustomerHandler {
	return &CustomerHandler{customers: customers, jwtSecret: jwtSecret, jwtExpiryHours: jwtExpiryHours}
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
