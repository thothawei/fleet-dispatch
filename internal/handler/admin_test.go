package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"golang.org/x/crypto/bcrypt"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"

	"line-fleet-dispatch/internal/auth"
	"line-fleet-dispatch/internal/database"
	"line-fleet-dispatch/internal/middleware"
	"line-fleet-dispatch/internal/model"
	"line-fleet-dispatch/internal/repository"
	"line-fleet-dispatch/internal/service"
)

// newMigratedTestDB 起真 PostGIS 容器並跑「全部 db/migrations」，得到與正式一致的完整 schema。
// Docker 不可用時跳過。（沿用 internal/repository 既有的整合測試手法）
func newMigratedTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	ctx := context.Background()
	container, err := tcpostgres.Run(ctx, "postgis/postgis:16-3.4",
		tcpostgres.WithDatabase("test"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Skipf("略過整合測試（Docker/testcontainers 不可用）: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("取得連線字串失敗: %v", err)
	}
	if err := database.RunMigrations(connStr, "../../db/migrations"); err != nil {
		t.Fatalf("跑 migration 失敗: %v", err)
	}
	db, err := gorm.Open(gormpostgres.Open(connStr), &gorm.Config{})
	if err != nil {
		t.Fatalf("連線失敗: %v", err)
	}
	return db
}

// fakeAdminStore 以記憶體模擬 AdminRepository（僅供 Login 測試使用）
type fakeAdminStore struct {
	byUsername map[string]*model.Admin
}

func (f *fakeAdminStore) FindByUsername(username string) (*model.Admin, error) {
	a, ok := f.byUsername[username]
	if !ok {
		return nil, service.ErrNotFound
	}
	return a, nil
}
func (f *fakeAdminStore) Create(a *model.Admin) error { return nil }
func (f *fakeAdminStore) CountAll() (int64, error)    { return int64(len(f.byUsername)), nil }

// 確保 AdminHandler 型別存在（資料正確性由 repo/redis 整合測試涵蓋，此處聚焦授權邊界）
var _ = NewAdminHandler

func TestAdminFleet_授權邊界(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/admin/fleet", middleware.AdminAuth("s", func(id int64) (string, bool, error) { return "superadmin", true, nil }), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	// 無 token → 401
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/admin/fleet", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("預期 401，得到 %d", w.Code)
	}

	// admin token → 200
	tok, _ := auth.GenerateToken("admin", 1, "s", time.Hour)
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("GET", "/api/admin/fleet", nil)
	req2.Header.Set("Authorization", "Bearer "+tok)
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("admin 應通過授權，得到 %d", w2.Code)
	}
}

func TestAdminLogin_停用帳號擋登入(t *testing.T) {
	gin.SetMode(gin.TestMode)

	hash, err := bcrypt.GenerateFromPassword([]byte("s3cret"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("產生密碼雜湊失敗: %v", err)
	}
	store := &fakeAdminStore{byUsername: map[string]*model.Admin{
		"disabled": {ID: 1, Username: "disabled", PasswordHash: string(hash), Name: "停用管理員", Role: "operator", IsActive: false},
	}}
	admins := service.NewAdminRegistry(store)
	h := NewAdminHandler(admins, nil, nil, nil, nil, nil, nil, nil, nil, nil, "s", 1)

	r := gin.New()
	r.POST("/api/admin/login", h.Login)

	body, _ := json.Marshal(map[string]string{"username": "disabled", "password": "s3cret"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/admin/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("停用帳號應回 403，得到 %d，body=%s", w.Code, w.Body.String())
	}
}

func TestAdminMe(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newMigratedTestDB(t)
	adminRepo := repository.NewAdminRepository(db)

	now := time.Now()
	seed := &model.Admin{Username: "ops1", PasswordHash: "hash", Name: "值班", Role: "dispatcher", IsActive: true, CreatedAt: now, UpdatedAt: now}
	if err := adminRepo.Create(seed); err != nil {
		t.Fatalf("種子 admin 建立失敗: %v", err)
	}

	h := NewAdminHandler(nil, nil, nil, nil, nil, nil, nil, nil, adminRepo, nil, "s", 1)

	r := gin.New()
	r.GET("/api/admin/me", func(c *gin.Context) {
		c.Set(middleware.CtxAdminID, seed.ID)
		c.Set(middleware.CtxAdminRole, seed.Role)
		c.Next()
	}, h.Me)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/admin/me", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("預期 200，得到 %d，body=%s", w.Code, w.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("回應非合法 JSON: %v", err)
	}
	if body["role"] != "dispatcher" {
		t.Fatalf("預期 role=dispatcher，得到 %v，body=%s", body["role"], w.Body.String())
	}
}
