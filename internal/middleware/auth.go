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

// CtxAdminID 存放經 JWT 驗證後的管理員 id
const CtxAdminID = "admin_id"

// AdminAuth 驗證 Bearer JWT 且角色為 admin；非 admin 回 403，無效 token 回 401
func AdminAuth(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "缺少或格式錯誤的授權標頭"})
			return
		}
		role, id, err := auth.ParseToken(strings.TrimPrefix(header, "Bearer "), secret)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "token 無效或已過期"})
			return
		}
		if role != "admin" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "需要管理員權限"})
			return
		}
		c.Set(CtxAdminID, id)
		c.Next()
	}
}

// AdminIDFromCtx 取出中介層放入的 admin_id
func AdminIDFromCtx(c *gin.Context) int64 {
	if v, ok := c.Get(CtxAdminID); ok {
		if id, ok := v.(int64); ok {
			return id
		}
	}
	return 0
}

// CtxCustomerID 存放經 JWT 驗證後的乘客 id
const CtxCustomerID = "customer_id"

// CustomerAuth 驗證 Bearer JWT 且角色為 customer；非 customer 回 403，無效 token 回 401
func CustomerAuth(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "缺少或格式錯誤的授權標頭"})
			return
		}
		role, id, err := auth.ParseToken(strings.TrimPrefix(header, "Bearer "), secret)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "token 無效或已過期"})
			return
		}
		if role != "customer" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "需要乘客身分"})
			return
		}
		c.Set(CtxCustomerID, id)
		c.Next()
	}
}

// CustomerIDFromCtx 取出中介層放入的 customer_id
func CustomerIDFromCtx(c *gin.Context) int64 {
	if v, ok := c.Get(CtxCustomerID); ok {
		if id, ok := v.(int64); ok {
			return id
		}
	}
	return 0
}

// CtxRole 存放經 MultiAuth 驗證後的角色（driver/customer/admin）
const CtxRole = "auth_role"

// CtxSubjectID 存放經 MultiAuth 驗證後的角色主體 id
const CtxSubjectID = "auth_subject_id"

// MultiAuth 接受任一合法角色（driver/customer/admin）的 JWT，成功則把 role 與 id 放進
// context，不因角色而拒絕；由後續 handler 依資源擁有權自行授權。無效/缺 token 回 401。
// 用於需「本趟乘客／司機或 admin 皆可存取」的端點（例如軌跡回放）。
func MultiAuth(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "缺少或格式錯誤的授權標頭"})
			return
		}
		role, id, err := auth.ParseToken(strings.TrimPrefix(header, "Bearer "), secret)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "token 無效或已過期"})
			return
		}
		c.Set(CtxRole, role)
		c.Set(CtxSubjectID, id)
		c.Next()
	}
}

// RoleFromCtx 取出 MultiAuth 放入的角色
func RoleFromCtx(c *gin.Context) string {
	if v, ok := c.Get(CtxRole); ok {
		if role, ok := v.(string); ok {
			return role
		}
	}
	return ""
}

// SubjectIDFromCtx 取出 MultiAuth 放入的角色主體 id
func SubjectIDFromCtx(c *gin.Context) int64 {
	if v, ok := c.Get(CtxSubjectID); ok {
		if id, ok := v.(int64); ok {
			return id
		}
	}
	return 0
}
