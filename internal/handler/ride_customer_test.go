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

// setupCustomerRideRouter 比照 ride_create_test.go 的作法：這三條路徑的授權檢查
// （401/403）都在進入 handler 前由中介層攔截，故可用 nil 依賴建構、不需 DB/redis。
// 資料正確性（owner 檢查、空結果、取消釋放鎖）由 internal/service 的整合測試涵蓋。
func setupCustomerRideRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := NewRideHandler(nil, nil, nil, service.NewRideService(nil, nil, nil, nil))
	r := gin.New()
	authed := r.Group("/api")
	authed.Use(middleware.CustomerAuth("s"))
	{
		authed.GET("/customer/rides/active", h.ActiveByCustomer)
		authed.GET("/customer/rides/:id", h.GetByCustomer)
		authed.POST("/rides/:id/cancel-by-customer", h.CancelByCustomer)
	}
	return r
}

// TestActiveByCustomer_授權邊界 驗收條件：三端點都受 customer JWT 保護
func TestActiveByCustomer_授權邊界(t *testing.T) {
	r := setupCustomerRideRouter()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/customer/rides/active", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("無 token 預期 401，得到 %d", w.Code)
	}

	dtok, _ := auth.GenerateToken("driver", 7, "s", time.Hour)
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("GET", "/api/customer/rides/active", nil)
	req2.Header.Set("Authorization", "Bearer "+dtok)
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusForbidden {
		t.Fatalf("driver token 預期 403，得到 %d", w2.Code)
	}
}

// TestGetByCustomer_授權邊界與參數格式
func TestGetByCustomer_授權邊界與參數格式(t *testing.T) {
	r := setupCustomerRideRouter()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/customer/rides/1", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("無 token 預期 401，得到 %d", w.Code)
	}

	dtok, _ := auth.GenerateToken("driver", 7, "s", time.Hour)
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("GET", "/api/customer/rides/1", nil)
	req2.Header.Set("Authorization", "Bearer "+dtok)
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusForbidden {
		t.Fatalf("driver token 預期 403，得到 %d", w2.Code)
	}

	// customer token + 非數字 id → 400（在觸及 service 前即返回，未接 DB）
	ctok, _ := auth.GenerateToken("customer", 9, "s", time.Hour)
	w3 := httptest.NewRecorder()
	req3, _ := http.NewRequest("GET", "/api/customer/rides/abc", nil)
	req3.Header.Set("Authorization", "Bearer "+ctok)
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusBadRequest {
		t.Fatalf("非數字 id 預期 400，得到 %d", w3.Code)
	}
}

// TestCancelByCustomer_授權邊界與參數格式
func TestCancelByCustomer_授權邊界與參數格式(t *testing.T) {
	r := setupCustomerRideRouter()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/rides/1/cancel-by-customer", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("無 token 預期 401，得到 %d", w.Code)
	}

	dtok, _ := auth.GenerateToken("driver", 7, "s", time.Hour)
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("POST", "/api/rides/1/cancel-by-customer", nil)
	req2.Header.Set("Authorization", "Bearer "+dtok)
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusForbidden {
		t.Fatalf("driver token 預期 403，得到 %d", w2.Code)
	}

	ctok, _ := auth.GenerateToken("customer", 9, "s", time.Hour)
	w3 := httptest.NewRecorder()
	req3, _ := http.NewRequest("POST", "/api/rides/abc/cancel-by-customer", strings.NewReader(""))
	req3.Header.Set("Authorization", "Bearer "+ctok)
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusBadRequest {
		t.Fatalf("非數字 id 預期 400，得到 %d", w3.Code)
	}
}
