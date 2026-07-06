package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"line-fleet-dispatch/internal/auth"
)

// CtxDriverID 存放經 JWT 驗證後的司機 id
const CtxDriverID = "driver_id"

// DriverAuth 驗證 Authorization: Bearer <jwt>，成功則把 driver_id 放進 context
func DriverAuth(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "缺少或格式錯誤的授權標頭"})
			return
		}
		driverID, err := auth.ParseDriverToken(strings.TrimPrefix(header, "Bearer "), secret)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "token 無效或已過期"})
			return
		}
		c.Set(CtxDriverID, driverID)
		c.Next()
	}
}

// DriverIDFromCtx 取出中介層放入的 driver_id
func DriverIDFromCtx(c *gin.Context) int64 {
	if v, ok := c.Get(CtxDriverID); ok {
		if id, ok := v.(int64); ok {
			return id
		}
	}
	return 0
}
