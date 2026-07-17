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

// setupVehicleRouter 以 nil repo 的 DriverRegistry 建路由：驗證失敗的路徑在碰 DB 前就回，
// 故不需要真連線。成功寫入／車牌衝突（409）由 repository 整合測試覆蓋。
func setupVehicleRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := NewDriverHandler(nil, service.NewDriverRegistry(nil), nil, "s", 1)
	r := gin.New()
	g := r.Group("/api")
	g.Use(middleware.DriverAuth("s"))
	{
		g.GET("/driver/vehicle", h.Vehicle)
		g.PUT("/driver/vehicle", h.UpdateVehicle)
	}
	return r
}

func putVehicle(t *testing.T, r *gin.Engine, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/driver/vehicle", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	r.ServeHTTP(w, req)
	return w
}

func TestDriverVehicle_授權邊界(t *testing.T) {
	r := setupVehicleRouter()

	if w := putVehicle(t, r, "", `{"vehicle_type":"sedan","plate_number":"ABC-1234"}`); w.Code != http.StatusUnauthorized {
		t.Fatalf("無 token 預期 401，得到 %d", w.Code)
	}

	// 車輛資訊只有司機本人能改；customer token 走 DriverAuth 視為無效。
	ctok, _ := auth.GenerateToken("customer", 9, "s", time.Hour)
	if w := putVehicle(t, r, ctok, `{"vehicle_type":"sedan","plate_number":"ABC-1234"}`); w.Code != http.StatusUnauthorized {
		t.Fatalf("customer token 打 driver 端點預期 401，得到 %d", w.Code)
	}
}

func TestDriverVehicle_參數驗證(t *testing.T) {
	r := setupVehicleRouter()
	dtok, _ := auth.GenerateDriverToken(7, "s", time.Hour)

	cases := []struct {
		name string
		body string
	}{
		{"壞 JSON", `{bad`},
		{"缺車種", `{"plate_number":"ABC-1234"}`},
		{"缺車牌", `{"vehicle_type":"sedan"}`},
		// 空值等同「未設定」：若被當成合法輸入寫入，司機就繞過了 O3 的接單 gate。
		{"車種空字串", `{"vehicle_type":"","plate_number":"ABC-1234"}`},
		{"車牌空字串", `{"vehicle_type":"sedan","plate_number":""}`},
		// 車種是清潔費（O6）與派單過濾（P3）的判斷依據，不得收白名單外的值。
		{"車種非白名單", `{"vehicle_type":"spaceship","plate_number":"ABC-1234"}`},
		{"車牌含非法字元", `{"vehicle_type":"sedan","plate_number":"ABC_1234"}`},
		{"車牌過長", `{"vehicle_type":"sedan","plate_number":"ABCD-123456"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if w := putVehicle(t, r, dtok, tc.body); w.Code != http.StatusBadRequest {
				t.Fatalf("預期 400，得到 %d（body: %s）", w.Code, w.Body.String())
			}
		})
	}
}
