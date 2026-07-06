package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"line-fleet-dispatch/internal/auth"
)

func setupAdminRouter(secret string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/admin/ping", AdminAuth(secret), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"admin_id": AdminIDFromCtx(c)})
	})
	return r
}

func TestAdminAuth_合法admin通過(t *testing.T) {
	secret := "s"
	tok, _ := auth.GenerateToken("admin", 5, secret, time.Hour)
	r := setupAdminRouter(secret)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/admin/ping", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("預期 200，得到 %d", w.Code)
	}
}

func TestAdminAuth_無token回401(t *testing.T) {
	r := setupAdminRouter("s")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/admin/ping", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("預期 401，得到 %d", w.Code)
	}
}

func TestAdminAuth_司機token被拒403(t *testing.T) {
	secret := "s"
	tok, _ := auth.GenerateToken("driver", 9, secret, time.Hour)
	r := setupAdminRouter(secret)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/admin/ping", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("預期 403，得到 %d", w.Code)
	}
}
