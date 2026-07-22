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
	osrmclient "line-fleet-dispatch/internal/osrm"
	"line-fleet-dispatch/internal/service"
)

// 驗 RideHandler.EstimateFare 的授權/綁定/未就緒邊界（401/403/400/503）。
// 車資實算與 200 成功值由 service 層單元測試（estimate_test.go）與 docker e2e 覆蓋——
// 這裡只確認 HTTP 邊界，用 fees=nil 的 EstimateService（estimate 非 nil 但呼叫時回未就緒）。
func setupEstimateRouter(withEstimate bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := NewRideHandler(nil, nil, nil, service.NewRideService(nil, nil, nil, nil))
	if withEstimate {
		// fees=nil → Estimate() 回 ErrEstimateUnavailable（503）；足以測綁定/授權路徑。
		h.SetEstimate(service.NewEstimateService(osrmclient.NewClient("http://127.0.0.1:0"), nil))
	}
	r := gin.New()
	r.POST("/api/customer/rides/estimate", middleware.CustomerAuth("s"), h.EstimateFare)
	return r
}

func postEstimate(t *testing.T, r *gin.Engine, token, body string) int {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/customer/rides/estimate", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	r.ServeHTTP(w, req)
	return w.Code
}

func TestEstimateFare_授權與綁定邊界(t *testing.T) {
	r := setupEstimateRouter(true)
	ctok, _ := auth.GenerateToken("customer", 9, "s", time.Hour)
	dtok, _ := auth.GenerateToken("driver", 7, "s", time.Hour)

	// 無 token → 401（中介層攔截）
	if code := postEstimate(t, r, "", `{"pickup_lat":25,"pickup_lng":121}`); code != http.StatusUnauthorized {
		t.Fatalf("無 token 預期 401，得到 %d", code)
	}
	// driver token → 403（角色不符）
	if code := postEstimate(t, r, dtok, `{"pickup_lat":25,"pickup_lng":121}`); code != http.StatusForbidden {
		t.Fatalf("driver token 預期 403，得到 %d", code)
	}
	// customer token + 壞 JSON → 400（綁定失敗即返回，未觸及 Estimate）
	if code := postEstimate(t, r, ctok, `{bad`); code != http.StatusBadRequest {
		t.Fatalf("壞 JSON 預期 400，得到 %d", code)
	}
	// customer token + 合法 JSON、但服務未就緒（fees=nil）→ 503
	if code := postEstimate(t, r, ctok,
		`{"pickup_lat":25.03,"pickup_lng":121.56,"dropoff_lat":25.05,"dropoff_lng":121.52}`); code != http.StatusServiceUnavailable {
		t.Fatalf("服務未就緒預期 503，得到 %d", code)
	}
}

// 未呼叫 SetEstimate（estimate==nil）→ handler 早退 503，不 panic。
func TestEstimateFare_未啟用回503(t *testing.T) {
	r := setupEstimateRouter(false)
	ctok, _ := auth.GenerateToken("customer", 9, "s", time.Hour)
	if code := postEstimate(t, r, ctok,
		`{"pickup_lat":25.03,"pickup_lng":121.56,"dropoff_lat":25.05,"dropoff_lng":121.52}`); code != http.StatusServiceUnavailable {
		t.Fatalf("未啟用預期 503，得到 %d", code)
	}
}
