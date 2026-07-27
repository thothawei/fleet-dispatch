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

// 驗 RideHandler.RateByCustomer 的授權／綁定／值域邊界（401/403/400/503）。
// 擁有權、行程狀態、一趟一評等需要 DB 的守門由 service 層整合測試
// （internal/service/rating_test.go，真 PostGIS）覆蓋——這裡只確認 HTTP 邊界，
// 故用 repo=nil 的 RatingService：星等值域在碰到 repo **之前**就返回，不會 panic。
func setupRatingRouter(withRating bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := NewRideHandler(nil, nil, nil, service.NewRideService(nil, nil, nil, nil))
	if withRating {
		h.SetRating(service.NewRatingService(nil, nil))
	}
	r := gin.New()
	r.POST("/api/customer/rides/:id/rating", middleware.CustomerAuth("s"), h.RateByCustomer)
	return r
}

func postRating(t *testing.T, r *gin.Engine, token, id, body string) int {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/customer/rides/"+id+"/rating", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	r.ServeHTTP(w, req)
	return w.Code
}

func TestRateByCustomer_授權與綁定邊界(t *testing.T) {
	r := setupRatingRouter(true)
	ctok, _ := auth.GenerateToken("customer", 9, "s", time.Hour)
	dtok, _ := auth.GenerateToken("driver", 7, "s", time.Hour)

	// 無 token → 401（中介層攔截）
	if code := postRating(t, r, "", "1", `{"score":5}`); code != http.StatusUnauthorized {
		t.Fatalf("無 token 預期 401，得到 %d", code)
	}
	// driver token → 403（角色不符；司機不能替乘客評自己）
	if code := postRating(t, r, dtok, "1", `{"score":5}`); code != http.StatusForbidden {
		t.Fatalf("driver token 預期 403，得到 %d", code)
	}
	// 非數字的 ride id → 400
	if code := postRating(t, r, ctok, "abc", `{"score":5}`); code != http.StatusBadRequest {
		t.Fatalf("壞 id 預期 400，得到 %d", code)
	}
	// 壞 JSON → 400（綁定失敗即返回）
	if code := postRating(t, r, ctok, "1", `{bad`); code != http.StatusBadRequest {
		t.Fatalf("壞 JSON 預期 400，得到 %d", code)
	}
	// 星等越界 → 400。**沒帶 score 等同 0**，同樣被擋——
	// 少一個欄位不該被當成「給 0 分」寫進去。
	for _, body := range []string{`{"score":0}`, `{"score":6}`, `{}`} {
		if code := postRating(t, r, ctok, "1", body); code != http.StatusBadRequest {
			t.Fatalf("星等越界 %s 預期 400，得到 %d", body, code)
		}
	}
}

// 未呼叫 SetRating（rating==nil）→ handler 早退 503，不 panic。
func TestRateByCustomer_未啟用回503(t *testing.T) {
	r := setupRatingRouter(false)
	ctok, _ := auth.GenerateToken("customer", 9, "s", time.Hour)
	if code := postRating(t, r, ctok, "1", `{"score":5}`); code != http.StatusServiceUnavailable {
		t.Fatalf("未啟用預期 503，得到 %d", code)
	}
}
