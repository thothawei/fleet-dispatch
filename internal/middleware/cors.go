package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// CORS 允許瀏覽器端（Flutter Web、後台前端）跨網域呼叫 API。
// 認證採 Authorization: Bearer <jwt>，不依賴 cookie，故不需要 credentials，
// 允許任意 Origin 不會帶來 CSRF 風險。
func CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Vary", "Origin")
		}
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		c.Header("Access-Control-Max-Age", "600")

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
