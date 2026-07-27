package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// 乘客訂單路由同一層同時有靜態段（estimate／active）與參數段（:id），
// **gin 舊版對此會在註冊當下 panic**——那是啟動即死、單元測試照樣全綠的失敗模式，
// 一路到部署才會發現。B5 新增 POST /customer/rides/:id/rating 讓 POST 這一側
// 首次出現同樣的混用，故補這道守門。
//
// 這裡重建的是**路由形狀**而非 main.go 的實際路由表（無法從 handler 套件取用），
// 形狀變了要同步改這裡。
func TestCustomerRideRoutesStaticAndParamCoexist(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("/api")
	g.POST("/rides", func(c *gin.Context) { c.String(200, "create") })
	g.POST("/customer/rides/estimate", func(c *gin.Context) { c.String(200, "estimate") })
	g.GET("/customer/rides", func(c *gin.Context) { c.String(200, "history") })
	g.GET("/customer/rides/active", func(c *gin.Context) { c.String(200, "active") })
	g.GET("/customer/rides/:id", func(c *gin.Context) { c.String(200, "detail") })
	g.POST("/customer/rides/:id/rating", func(c *gin.Context) { c.String(200, "rating:"+c.Param("id")) })

	for _, tc := range []struct{ method, path, want string }{
		{"POST", "/api/customer/rides/estimate", "estimate"},
		{"POST", "/api/customer/rides/42/rating", "rating:42"},
		{"GET", "/api/customer/rides/active", "active"},
		{"GET", "/api/customer/rides/42", "detail"},
	} {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(tc.method, tc.path, nil)
		r.ServeHTTP(w, req)
		if w.Body.String() != tc.want {
			t.Fatalf("%s %s → %q，預期 %q（code=%d）", tc.method, tc.path, w.Body.String(), tc.want, w.Code)
		}
	}
}
