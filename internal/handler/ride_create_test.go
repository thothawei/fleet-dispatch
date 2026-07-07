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

// 驗「真」RideHandler.Create 的授權/綁定邊界（401/403/400）。這三條路徑都在
// 呼叫 rideService 之前返回（401/403 由中介層攔截、400 由綁定失敗即返回），
// 故可用 nil 依賴的 RideService 建構、不需 DB/redis。
// 成功下單(200)與重複擋(409)的語意由 Task 4 的 docker e2e smoke 覆蓋。
func setupCreateRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := NewRideHandler(nil, nil, nil, service.NewRideService(nil, nil, nil, nil))
	r := gin.New()
	r.POST("/api/rides", middleware.CustomerAuth("s"), h.Create)
	return r
}

func TestCreateRide_授權與綁定邊界(t *testing.T) {
	r := setupCreateRouter()

	// 無 token → 401（中介層攔截，未進 handler）
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/rides", strings.NewReader(`{"pickup_lat":25,"pickup_lng":121}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("無 token 預期 401，得到 %d", w.Code)
	}

	// driver token → 403（角色不符，中介層攔截）
	dtok, _ := auth.GenerateToken("driver", 7, "s", time.Hour)
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("POST", "/api/rides", strings.NewReader(`{"pickup_lat":25,"pickup_lng":121}`))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer "+dtok)
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusForbidden {
		t.Fatalf("driver token 預期 403，得到 %d", w2.Code)
	}

	// customer token + 壞 JSON → 400（Create 綁定失敗即返回，未觸及 rideService）
	ctok, _ := auth.GenerateToken("customer", 9, "s", time.Hour)
	w3 := httptest.NewRecorder()
	req3, _ := http.NewRequest("POST", "/api/rides", strings.NewReader(`{bad`))
	req3.Header.Set("Content-Type", "application/json")
	req3.Header.Set("Authorization", "Bearer "+ctok)
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusBadRequest {
		t.Fatalf("壞 JSON 預期 400，得到 %d", w3.Code)
	}
}
