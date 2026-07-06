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

// 確保 CustomerHandler 型別存在（註冊/登入資料流由 service 單元測試涵蓋）
var _ = NewCustomerHandler

func TestCustomerProtected_授權邊界(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/customer/me", middleware.CustomerAuth("s"), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"id": middleware.CustomerIDFromCtx(c)})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/customer/me", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("預期 401，得到 %d", w.Code)
	}

	tok, _ := auth.GenerateToken("customer", 3, "s", time.Hour)
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("GET", "/api/customer/me", nil)
	req2.Header.Set("Authorization", "Bearer "+tok)
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("customer 應通過授權，得到 %d", w2.Code)
	}
}
