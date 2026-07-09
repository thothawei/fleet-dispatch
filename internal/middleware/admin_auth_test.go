package middleware

import (
	"errors"
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
	lookup := func(id int64) (string, bool, error) { return "superadmin", true, nil }
	r.GET("/admin/ping", AdminAuth(secret, lookup), func(c *gin.Context) {
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
	// 有效但非 admin 角色的 token 屬已驗證但無權限，應回 403 而非 401
	if w.Code != http.StatusForbidden {
		t.Fatalf("預期 403，得到 %d", w.Code)
	}
}

func Test查無帳號_AdminAuth回401(t *testing.T) {
	secret := "s"
	tok, _ := auth.GenerateToken("admin", 9, secret, time.Hour)
	lookup := func(id int64) (string, bool, error) { return "", false, errors.New("db error") }
	r := gin.New()
	r.GET("/x", AdminAuth(secret, lookup), func(c *gin.Context) { c.Status(200) })
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/x", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("lookup 錯誤應 401，得 %d", w.Code)
	}
}

func Test停用帳號_AdminAuth回403(t *testing.T) {
	secret := "s"
	tok, _ := auth.GenerateToken("admin", 7, secret, time.Hour)
	lookup := func(id int64) (string, bool, error) { return "viewer", false, nil } // 停用
	r := gin.New()
	r.GET("/x", AdminAuth(secret, lookup), func(c *gin.Context) { c.Status(200) })
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/x", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("停用帳號應 403，得 %d", w.Code)
	}
}

func TestRequireAdminRole_不足回403(t *testing.T) {
	r := gin.New()
	r.GET("/x", func(c *gin.Context) { c.Set(CtxAdminRole, "viewer"); c.Next() },
		RequireAdminRole(auth.RoleDispatcher), func(c *gin.Context) { c.Status(200) })
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/x", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("viewer 打 dispatcher 路由應 403，得 %d", w.Code)
	}
}
