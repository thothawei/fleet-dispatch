package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"line-fleet-dispatch/internal/auth"
	"line-fleet-dispatch/internal/middleware"
)

// 確保 AdminHandler 型別存在（資料正確性由 repo/redis 整合測試涵蓋，此處聚焦授權邊界）
var _ = NewAdminHandler

func TestAdminFleet_授權邊界(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/admin/fleet", middleware.AdminAuth("s"), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	// 無 token → 401
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/admin/fleet", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("預期 401，得到 %d", w.Code)
	}

	// admin token → 200
	tok, _ := auth.GenerateToken("admin", 1, "s", time.Hour)
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("GET", "/api/admin/fleet", nil)
	req2.Header.Set("Authorization", "Bearer "+tok)
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("admin 應通過授權，得到 %d", w2.Code)
	}
}
