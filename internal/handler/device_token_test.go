package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"line-fleet-dispatch/internal/auth"
	"line-fleet-dispatch/internal/middleware"
	"line-fleet-dispatch/internal/service"
)

func setupDeviceTokenRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := NewDeviceTokenHandler(service.NewDeviceTokenService(nil))
	r := gin.New()
	driverG := r.Group("/api")
	driverG.Use(middleware.DriverAuth("s"))
	{
		driverG.POST("/driver/device-token", h.RegisterByDriver)
		driverG.DELETE("/driver/device-token", h.UnregisterByDriver)
	}
	customerG := r.Group("/api")
	customerG.Use(middleware.CustomerAuth("s"))
	{
		customerG.POST("/customer/device-token", h.RegisterByCustomer)
	}
	return r
}

func TestDeviceToken_授權邊界(t *testing.T) {
	r := setupDeviceTokenRouter()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/driver/device-token", strings.NewReader(`{"platform":"fcm","token":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("無 token 預期 401，得到 %d", w.Code)
	}

	ctok, _ := auth.GenerateToken("customer", 9, "s", time.Hour)
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("POST", "/api/driver/device-token", strings.NewReader(`{"platform":"fcm","token":"x"}`))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer "+ctok)
	r.ServeHTTP(w2, req2)
	// DriverAuth 用 ParseDriverToken：非司機 token 視為無效 → 401
	if w2.Code != http.StatusUnauthorized {
		t.Fatalf("customer token 打 driver 端點預期 401，得到 %d", w2.Code)
	}

	dtok, _ := auth.GenerateDriverToken(7, "s", time.Hour)
	w3 := httptest.NewRecorder()
	req3, _ := http.NewRequest("POST", "/api/driver/device-token", strings.NewReader(`{bad`))
	req3.Header.Set("Content-Type", "application/json")
	req3.Header.Set("Authorization", "Bearer "+dtok)
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusBadRequest {
		t.Fatalf("壞 JSON 預期 400，得到 %d", w3.Code)
	}
}
