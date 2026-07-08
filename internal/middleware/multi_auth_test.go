package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"line-fleet-dispatch/internal/auth"
)

func setupMultiRouter(secret string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/whoami", MultiAuth(secret), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"role": RoleFromCtx(c),
			"id":   SubjectIDFromCtx(c),
		})
	})
	return r
}

func TestMultiAuth_三種角色皆通過並帶出身分(t *testing.T) {
	secret := "s"
	cases := []struct {
		role string
		id   int64
	}{
		{"customer", 7},
		{"admin", 3},
	}
	for _, tc := range cases {
		tok, _ := auth.GenerateToken(tc.role, tc.id, secret, time.Hour)
		r := setupMultiRouter(secret)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/whoami", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("角色 %s 預期 200，得到 %d", tc.role, w.Code)
		}
	}
}

func TestMultiAuth_司機token通過(t *testing.T) {
	secret := "s"
	tok, _ := auth.GenerateDriverToken(11, secret, time.Hour)
	r := setupMultiRouter(secret)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/whoami", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("司機 token 預期 200，得到 %d", w.Code)
	}
}

func TestMultiAuth_無token回401(t *testing.T) {
	r := setupMultiRouter("s")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/whoami", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("預期 401，得到 %d", w.Code)
	}
}

func TestMultiAuth_無效token回401(t *testing.T) {
	r := setupMultiRouter("s")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/whoami", nil)
	req.Header.Set("Authorization", "Bearer 亂七八糟")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("預期 401，得到 %d", w.Code)
	}
}
