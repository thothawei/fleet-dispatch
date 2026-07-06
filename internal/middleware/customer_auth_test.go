package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"line-fleet-dispatch/internal/auth"
)

func setupCustomerRouter(secret string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/customer/me", CustomerAuth(secret), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"customer_id": CustomerIDFromCtx(c)})
	})
	return r
}

func TestCustomerAuth_合法customer通過(t *testing.T) {
	secret := "s"
	tok, _ := auth.GenerateToken("customer", 7, secret, time.Hour)
	r := setupCustomerRouter(secret)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/customer/me", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("預期 200，得到 %d", w.Code)
	}
}

func TestCustomerAuth_無token回401(t *testing.T) {
	r := setupCustomerRouter("s")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/customer/me", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("預期 401，得到 %d", w.Code)
	}
}

func TestCustomerAuth_adminToken被拒403(t *testing.T) {
	secret := "s"
	tok, _ := auth.GenerateToken("admin", 1, secret, time.Hour)
	r := setupCustomerRouter(secret)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/customer/me", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("預期 403，得到 %d", w.Code)
	}
}
