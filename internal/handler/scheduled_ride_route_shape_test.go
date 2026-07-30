package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// 預約與常用地點在同一層同時有靜態段與參數段（/customer/scheduled-rides 與
// /customer/scheduled-rides/:id、/customer/places 與 /customer/places/:id），
// 而且它們跟既有的 /customer/rides、/customer/lost-items 共用同一個 group。
//
// gin 的路由衝突是**註冊當下 panic**：服務起不來，但單元測試照樣全綠——
// 是那種一路撐到部署才爆的失敗。這裡把乘客 group 的完整路由形狀重建一次，
// 讓衝突在測試階段就炸出來。
//
// **形狀變了要同步改這裡**（與 TestCustomerRideRoutesStaticAndParamCoexist 同一個約定）。
func TestCustomerPlaceAndScheduleRoutesCoexist(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("/api")

	// 既有的乘客路由（一起註冊才驗得到跨路由衝突）
	g.POST("/rides", func(c *gin.Context) { c.String(200, "create") })
	g.POST("/customer/rides/estimate", func(c *gin.Context) { c.String(200, "estimate") })
	g.GET("/customer/rides", func(c *gin.Context) { c.String(200, "history") })
	g.GET("/customer/rides/active", func(c *gin.Context) { c.String(200, "active") })
	g.GET("/customer/rides/:id", func(c *gin.Context) { c.String(200, "detail") })
	g.GET("/customer/lost-items", func(c *gin.Context) { c.String(200, "lost-items") })

	// 常用地點
	g.GET("/customer/places", func(c *gin.Context) { c.String(200, "places") })
	g.POST("/customer/places", func(c *gin.Context) { c.String(200, "place-create") })
	g.PUT("/customer/places/:id", func(c *gin.Context) { c.String(200, "place-update:"+c.Param("id")) })
	g.DELETE("/customer/places/:id", func(c *gin.Context) { c.String(200, "place-delete:"+c.Param("id")) })

	// 預約行程
	g.GET("/customer/scheduled-rides", func(c *gin.Context) { c.String(200, "schedules") })
	g.POST("/customer/scheduled-rides", func(c *gin.Context) { c.String(200, "schedule-create") })
	g.GET("/customer/scheduled-rides/:id", func(c *gin.Context) { c.String(200, "schedule:"+c.Param("id")) })
	g.POST("/customer/scheduled-rides/:id/cancel", func(c *gin.Context) {
		c.String(200, "schedule-cancel:"+c.Param("id"))
	})

	for _, tc := range []struct{ method, path, want string }{
		{"GET", "/api/customer/places", "places"},
		{"POST", "/api/customer/places", "place-create"},
		{"PUT", "/api/customer/places/7", "place-update:7"},
		{"DELETE", "/api/customer/places/7", "place-delete:7"},
		{"GET", "/api/customer/scheduled-rides", "schedules"},
		{"POST", "/api/customer/scheduled-rides", "schedule-create"},
		{"GET", "/api/customer/scheduled-rides/12", "schedule:12"},
		{"POST", "/api/customer/scheduled-rides/12/cancel", "schedule-cancel:12"},
		// 既有路由不能被新的蓋掉——/customer/rides 與 /customer/scheduled-rides
		// 是兄弟節點，前綴相同的那幾個字元最容易讓人以為可以共用一段。
		{"GET", "/api/customer/rides/active", "active"},
		{"GET", "/api/customer/rides/42", "detail"},
		{"GET", "/api/customer/lost-items", "lost-items"},
	} {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(tc.method, tc.path, nil)
		r.ServeHTTP(w, req)
		if w.Body.String() != tc.want {
			t.Fatalf("%s %s → %q，預期 %q（code=%d）", tc.method, tc.path, w.Body.String(), tc.want, w.Code)
		}
	}
}
