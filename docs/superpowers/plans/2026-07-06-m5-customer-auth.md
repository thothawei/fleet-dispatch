# M5-CUSTOMER-AUTH：乘客認證 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task。Steps 用 checkbox（`- [ ]`）。

**Goal:** 讓乘客能以 `line_user_id + 密碼` 註冊/登入取得 JWT，解鎖乘客端 WebSocket 訂閱（收自己這趟的司機位置/訂單事件）與 M7 乘客 App。鏡射既有司機（`DriverRegistry`）與後台（`AdminRegistry`）樣板。

**Architecture:** `customers` 表加 `password_hash`（鏡射 `drivers` 的 000004）。新增 `CustomerRegistry`（bcrypt 註冊/登入）、`middleware.CustomerAuth`（驗 role=customer）、`CustomerHandler`（Register/Login）。JWT 用已完成的 `auth.GenerateToken("customer", customerID, ...)`——`customerID` 即 `customers.id`，與 dispatch/tracking 發佈事件的 `Recipient{RoleCustomer, ride.CustomerID}` 對齊，故乘客登入後即可用該 token 連 WS 收自己的事件。

**Tech Stack:** Go 1.25 / Gin / GORM / golang-jwt v5 / bcrypt / go test（testcontainers）。

## Global Constraints

- Go module `line-fleet-dispatch`，Go 1.25.0。分層不破壞。
- 註解/log/使用者訊息一律**繁體中文**；識別字英文。
- JWT 沿用 `internal/auth`；乘客 token 用 `auth.GenerateToken("customer", customer.ID, secret, ttl)`。
- **前向相容 LINE Login**：乘客身分鍵為 `line_user_id`（與全系統一致）；日後接 LINE Login 只換「驗證方式」，不動身分模型。
- migration 編號接續既有到 000005，下一個是 `000006`（勿重用編號）。
- cgo 壞：`CGO_ENABLED=0`；整合測試需 Docker。整合測試需完整 schema 者，用既有 `newMigratedTestDB`（跑真 migration）。
- 直接在 `main` 開發；每 Task `git commit`；全部完成 `git push`。fleet 的 git 指令一律用 `git -C /Users/mac/Documents/line-fleet-dispatch` 絕對定位（跨回合 cwd 會重設）。
- TDD：先寫失敗測試 → 紅 → 最小實作 → 綠 → commit。

---

### Task 1：customers 加 password_hash（migration + model + repo 方法）

**Files:**
- Create: `db/migrations/000006_add_customer_password.up.sql`
- Create: `db/migrations/000006_add_customer_password.down.sql`
- Modify: `internal/model/models.go`（`Customer` 加 `PasswordHash`）
- Modify: `internal/repository/repository.go`（`CustomerRepository` 加 `SetPassword`、`FindByID`）
- Test: `internal/repository/customer_password_integration_test.go`

**Interfaces:**
- Produces:
  - `Customer.PasswordHash string`
  - `func (r *CustomerRepository) SetPassword(id int64, passwordHash string) error`
  - `func (r *CustomerRepository) FindByID(id int64) (*model.Customer, error)`

- [ ] **Step 1：寫 migration**

Create `db/migrations/000006_add_customer_password.up.sql`：
```sql
-- 乘客登入密碼（bcrypt hash）。既有乘客預設空字串（無法登入，需設定密碼）。
ALTER TABLE customers ADD COLUMN IF NOT EXISTS password_hash text NOT NULL DEFAULT '';
```
Create `db/migrations/000006_add_customer_password.down.sql`：
```sql
ALTER TABLE customers DROP COLUMN IF EXISTS password_hash;
```

- [ ] **Step 2：Customer model 加欄位**

在 `internal/model/models.go` 的 `Customer` struct，`Phone` 之後加：
```go
	PasswordHash string    `gorm:"column:password_hash;not null;default:''"`
```

- [ ] **Step 3：寫失敗整合測試**

Create `internal/repository/customer_password_integration_test.go`：
```go
package repository

import (
	"testing"
)

func TestCustomerRepository_設密碼與依ID查詢(t *testing.T) {
	db := newMigratedTestDB(t)
	repo := NewCustomerRepository(db)

	cust, err := repo.FindOrCreateByLineUserID("U_cust_pw", "乘客A")
	if err != nil {
		t.Fatalf("建立乘客失敗: %v", err)
	}
	if err := repo.SetPassword(cust.ID, "hashed"); err != nil {
		t.Fatalf("SetPassword 失敗: %v", err)
	}
	got, err := repo.FindByID(cust.ID)
	if err != nil {
		t.Fatalf("FindByID 失敗: %v", err)
	}
	if got.PasswordHash != "hashed" {
		t.Fatalf("密碼未寫入: %q", got.PasswordHash)
	}
}
```

- [ ] **Step 4：跑測試確認失敗**

Run: `CGO_ENABLED=0 go test ./internal/repository/ -run TestCustomerRepository_設密碼 -v`（先 `cd /Users/mac/Documents/line-fleet-dispatch`）
Expected: FAIL（`SetPassword`/`FindByID` undefined）。

- [ ] **Step 5：實作 repo 方法**

在 `internal/repository/repository.go` 的 `CustomerRepository` 區塊（`FindOrCreateByLineUserID` 之後）新增：
```go
func (r *CustomerRepository) FindByID(id int64) (*model.Customer, error) {
	var customer model.Customer
	if err := r.db.First(&customer, id).Error; err != nil {
		return nil, err
	}
	return &customer, nil
}

func (r *CustomerRepository) SetPassword(id int64, passwordHash string) error {
	return r.db.Model(&model.Customer{}).Where("id = ?", id).Updates(map[string]interface{}{
		"password_hash": passwordHash,
		"updated_at":    time.Now(),
	}).Error
}
```
（`time` 已於 repository.go import。）

- [ ] **Step 6：跑測試確認通過**

Run: `CGO_ENABLED=0 go test ./internal/repository/ -run TestCustomerRepository_設密碼 -v`
Expected: PASS（需 Docker）。

- [ ] **Step 7：Commit**

```bash
git -C /Users/mac/Documents/line-fleet-dispatch add db/migrations/000006_add_customer_password.up.sql db/migrations/000006_add_customer_password.down.sql internal/model/models.go internal/repository/repository.go internal/repository/customer_password_integration_test.go
git -C /Users/mac/Documents/line-fleet-dispatch commit -m "feat(customer): customers 加 password_hash + repo SetPassword/FindByID

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2：CustomerRegistry 服務（註冊/登入）

**Files:**
- Create: `internal/service/customer_registry.go`
- Test: `internal/service/customer_registry_test.go`

**Interfaces:**
- Consumes: `model.Customer`、既有 `ErrInvalidCredentials`、`ErrNotFound`
- Produces:
  - `type CustomerRegistry struct{...}`、`func NewCustomerRegistry(customers customerStore) *CustomerRegistry`
  - `func (s *CustomerRegistry) Register(ctx, lineUserID, name, password string) (*model.Customer, error)`
  - `func (s *CustomerRegistry) Login(ctx, lineUserID, password string) (*model.Customer, error)`
  - `type customerStore interface { FindByLineUserID(string) (*model.Customer, error); FindOrCreateByLineUserID(string, string) (*model.Customer, error); SetPassword(int64, string) error }`

**設計要點：** 鏡射 `DriverRegistry`；用介面 `customerStore` 便於用假替身做純邏輯單元測試（`*repository.CustomerRepository` 直接滿足）。

- [ ] **Step 1：寫失敗測試（假 store，純邏輯）**

Create `internal/service/customer_registry_test.go`：
```go
package service

import (
	"context"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"line-fleet-dispatch/internal/model"
)

type fakeCustomerStore struct {
	byLine map[string]*model.Customer
	nextID int64
}

func newFakeCustomerStore() *fakeCustomerStore {
	return &fakeCustomerStore{byLine: map[string]*model.Customer{}, nextID: 1}
}
func (f *fakeCustomerStore) FindByLineUserID(lineUserID string) (*model.Customer, error) {
	c, ok := f.byLine[lineUserID]
	if !ok {
		return nil, ErrNotFound
	}
	return c, nil
}
func (f *fakeCustomerStore) FindOrCreateByLineUserID(lineUserID, name string) (*model.Customer, error) {
	if c, ok := f.byLine[lineUserID]; ok {
		return c, nil
	}
	c := &model.Customer{ID: f.nextID, LineUserID: lineUserID, Name: name, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	f.nextID++
	f.byLine[lineUserID] = c
	return c, nil
}
func (f *fakeCustomerStore) SetPassword(id int64, hash string) error {
	for _, c := range f.byLine {
		if c.ID == id {
			c.PasswordHash = hash
			return nil
		}
	}
	return ErrNotFound
}

func TestCustomerRegistry_註冊與登入(t *testing.T) {
	reg := NewCustomerRegistry(newFakeCustomerStore())
	ctx := context.Background()

	cust, err := reg.Register(ctx, "U_c1", "小明", "pw123")
	if err != nil {
		t.Fatalf("註冊失敗: %v", err)
	}
	if cust.ID == 0 {
		t.Fatal("註冊後無 ID")
	}
	if bcrypt.CompareHashAndPassword([]byte(cust.PasswordHash), []byte("pw123")) != nil {
		t.Fatal("密碼未以 bcrypt 儲存")
	}

	logged, err := reg.Login(ctx, "U_c1", "pw123")
	if err != nil {
		t.Fatalf("登入失敗: %v", err)
	}
	if logged.ID != cust.ID {
		t.Fatalf("登入回傳錯誤乘客: %+v", logged)
	}

	if _, err := reg.Login(ctx, "U_c1", "wrong"); err == nil {
		t.Fatal("錯誤密碼應登入失敗")
	}
	if _, err := reg.Login(ctx, "U_unknown", "pw"); err == nil {
		t.Fatal("不存在乘客應登入失敗")
	}
}
```

- [ ] **Step 2：跑測試確認失敗**

Run: `CGO_ENABLED=0 go test ./internal/service/ -run TestCustomerRegistry -v`
Expected: FAIL（`undefined: NewCustomerRegistry`）。

- [ ] **Step 3：實作**

Create `internal/service/customer_registry.go`：
```go
package service

import (
	"context"

	"golang.org/x/crypto/bcrypt"

	"line-fleet-dispatch/internal/model"
)

// customerStore 抽象出 CustomerRegistry 需要的 repository 行為（方便測試替身）
type customerStore interface {
	FindByLineUserID(lineUserID string) (*model.Customer, error)
	FindOrCreateByLineUserID(lineUserID, name string) (*model.Customer, error)
	SetPassword(id int64, passwordHash string) error
}

// CustomerRegistry 乘客註冊與登入
type CustomerRegistry struct {
	customers customerStore
}

func NewCustomerRegistry(customers customerStore) *CustomerRegistry {
	return &CustomerRegistry{customers: customers}
}

// Register 建立或取回乘客並設定登入密碼（bcrypt）
func (s *CustomerRegistry) Register(ctx context.Context, lineUserID, name, password string) (*model.Customer, error) {
	customer, err := s.customers.FindOrCreateByLineUserID(lineUserID, name)
	if err != nil {
		return nil, err
	}
	if password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return nil, err
		}
		if err := s.customers.SetPassword(customer.ID, string(hash)); err != nil {
			return nil, err
		}
		customer.PasswordHash = string(hash)
	}
	return customer, nil
}

// Login 以 line_user_id + 密碼驗證
func (s *CustomerRegistry) Login(ctx context.Context, lineUserID, password string) (*model.Customer, error) {
	customer, err := s.customers.FindByLineUserID(lineUserID)
	if err != nil {
		return nil, ErrInvalidCredentials
	}
	if customer.PasswordHash == "" ||
		bcrypt.CompareHashAndPassword([]byte(customer.PasswordHash), []byte(password)) != nil {
		return nil, ErrInvalidCredentials
	}
	return customer, nil
}
```

- [ ] **Step 4：跑測試確認通過**

Run: `CGO_ENABLED=0 go test ./internal/service/ -run TestCustomerRegistry -v`
Expected: PASS。

- [ ] **Step 5：Commit**

```bash
git -C /Users/mac/Documents/line-fleet-dispatch add internal/service/customer_registry.go internal/service/customer_registry_test.go
git -C /Users/mac/Documents/line-fleet-dispatch commit -m "feat(customer): CustomerRegistry 註冊與登入（bcrypt）

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 3：CustomerAuth 中介層

**Files:**
- Modify: `internal/middleware/auth.go`（新增 `CustomerAuth`、`CustomerIDFromCtx`、`CtxCustomerID`）
- Test: `internal/middleware/customer_auth_test.go`

**Interfaces:**
- Consumes: `auth.ParseToken`
- Produces:
  - `const CtxCustomerID = "customer_id"`
  - `func CustomerAuth(secret string) gin.HandlerFunc`（Bearer JWT 且 role=customer；非 customer 回 403，無效回 401）
  - `func CustomerIDFromCtx(c *gin.Context) int64`

- [ ] **Step 1：寫失敗測試**

Create `internal/middleware/customer_auth_test.go`：
```go
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

func TestCustomerAuth_admin token被拒403(t *testing.T) {
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
```

- [ ] **Step 2：跑測試確認失敗**

Run: `CGO_ENABLED=0 go test ./internal/middleware/ -run TestCustomerAuth -v`
Expected: FAIL（`undefined: CustomerAuth`）。

- [ ] **Step 3：實作**

在 `internal/middleware/auth.go` 末尾新增：
```go
// CtxCustomerID 存放經 JWT 驗證後的乘客 id
const CtxCustomerID = "customer_id"

// CustomerAuth 驗證 Bearer JWT 且角色為 customer；非 customer 回 403，無效 token 回 401
func CustomerAuth(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "缺少或格式錯誤的授權標頭"})
			return
		}
		role, id, err := auth.ParseToken(strings.TrimPrefix(header, "Bearer "), secret)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "token 無效或已過期"})
			return
		}
		if role != "customer" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "需要乘客身分"})
			return
		}
		c.Set(CtxCustomerID, id)
		c.Next()
	}
}

// CustomerIDFromCtx 取出中介層放入的 customer_id
func CustomerIDFromCtx(c *gin.Context) int64 {
	if v, ok := c.Get(CtxCustomerID); ok {
		if id, ok := v.(int64); ok {
			return id
		}
	}
	return 0
}
```

- [ ] **Step 4：跑測試確認通過（含既有不回歸）**

Run: `CGO_ENABLED=0 go test ./internal/middleware/ -v`
Expected: PASS。

- [ ] **Step 5：Commit**

```bash
git -C /Users/mac/Documents/line-fleet-dispatch add internal/middleware/auth.go internal/middleware/customer_auth_test.go
git -C /Users/mac/Documents/line-fleet-dispatch commit -m "feat(customer): CustomerAuth 中介層（role=customer）

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 4：CustomerHandler + 路由接線

**Files:**
- Create: `internal/handler/customer.go`
- Modify: `cmd/server/main.go`（建立 customer repo 已存在→建 registry/handler、註冊路由）
- Test: `internal/handler/customer_test.go`

**Interfaces:**
- Consumes: `service.CustomerRegistry`、`auth.GenerateToken`、`middleware.CustomerAuth`
- Produces:
  - `type CustomerHandler struct{...}` + `func NewCustomerHandler(reg *service.CustomerRegistry, jwtSecret string, jwtExpiryHours int) *CustomerHandler`
  - `Register`（POST /api/customer/register）、`Login`（POST /api/customer/login）

- [ ] **Step 1：寫失敗測試（授權邊界 + 型別存在）**

Create `internal/handler/customer_test.go`：
```go
package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"line-fleet-dispatch/internal/auth"
	"line-fleet-dispatch/internal/middleware"
)

// 確保 CustomerHandler 型別存在（註冊/登入資料流由 service 單元測試涵蓋）
var _ = NewCustomerHandler

func TestCustomerProtected_授權邊界(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/customer/me", middleware.CustomerAuth("s"), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"id": middleware.CustomerIDFromCtx(c)})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/customer/me", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("預期 401，得到 %d", w.Code)
	}

	tok, _ := auth.GenerateToken("customer", 3, "s", time.Hour)
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("GET", "/api/customer/me", nil)
	req2.Header.Set("Authorization", "Bearer "+tok)
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("customer 應通過授權，得到 %d", w2.Code)
	}
}
```

- [ ] **Step 2：跑測試確認失敗**

Run: `CGO_ENABLED=0 go test ./internal/handler/ -run TestCustomerProtected -v`
Expected: FAIL（`undefined: NewCustomerHandler`）。

- [ ] **Step 3：實作 CustomerHandler**

Create `internal/handler/customer.go`：
```go
package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"line-fleet-dispatch/internal/auth"
	"line-fleet-dispatch/internal/service"
)

// CustomerHandler 乘客註冊與登入
type CustomerHandler struct {
	customers      *service.CustomerRegistry
	jwtSecret      string
	jwtExpiryHours int
}

func NewCustomerHandler(customers *service.CustomerRegistry, jwtSecret string, jwtExpiryHours int) *CustomerHandler {
	return &CustomerHandler{customers: customers, jwtSecret: jwtSecret, jwtExpiryHours: jwtExpiryHours}
}

func (h *CustomerHandler) issueToken(id int64) (string, error) {
	return auth.GenerateToken("customer", id, h.jwtSecret, time.Duration(h.jwtExpiryHours)*time.Hour)
}

// Register POST /api/customer/register
func (h *CustomerHandler) Register(c *gin.Context) {
	var req struct {
		LineUserID string `json:"line_user_id" binding:"required"`
		Name       string `json:"name"`
		Password   string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "參數錯誤"})
		return
	}
	customer, err := h.customers.Register(c.Request.Context(), req.LineUserID, req.Name, req.Password)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	token, err := h.issueToken(customer.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "簽發 token 失敗"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"customer_id": customer.ID, "name": customer.Name, "token": token})
}

// Login POST /api/customer/login
func (h *CustomerHandler) Login(c *gin.Context) {
	var req struct {
		LineUserID string `json:"line_user_id" binding:"required"`
		Password   string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "參數錯誤"})
		return
	}
	customer, err := h.customers.Login(c.Request.Context(), req.LineUserID, req.Password)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}
	token, err := h.issueToken(customer.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "簽發 token 失敗"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"customer_id": customer.ID, "token": token})
}
```

- [ ] **Step 4：接線 main.go**

4a. 建立 registry/handler（`customerRepo` 已存在於 main.go）。在 admin handler 建立處附近加：
```go
	customerRegistry := service.NewCustomerRegistry(customerRepo)
	customerHandler := handler.NewCustomerHandler(customerRegistry, cfg.JWTSecret, cfg.JWTExpiryHours)
```

4b. 在 `api := r.Group("/api")` 區塊內，司機註冊/登入附近加：
```go
		// 乘客：註冊 / 登入（公開）
		api.POST("/customer/register", customerHandler.Register)
		api.POST("/customer/login", customerHandler.Login)
```

- [ ] **Step 5：編譯、vet、相關測試**

Run:
```bash
cd /Users/mac/Documents/line-fleet-dispatch
CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go vet ./... && CGO_ENABLED=0 go test ./internal/handler/ ./internal/middleware/ ./internal/service/ 2>&1 | tail -10
```
Expected: 皆綠。

- [ ] **Step 6：Commit**

```bash
git -C /Users/mac/Documents/line-fleet-dispatch add internal/handler/customer.go internal/handler/customer_test.go cmd/server/main.go
git -C /Users/mac/Documents/line-fleet-dispatch commit -m "feat(customer): CustomerHandler 註冊/登入 API 與路由

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 5：文件、全量測試與 push

**Files:**
- Modify: `docs/decisions.md`

- [ ] **Step 1：補決策紀錄**

在 `docs/decisions.md` 末尾加：
```markdown
## 2026-07-06 · M5-CUSTOMER-AUTH 乘客認證

- **line_user_id + 密碼 JWT**：鏡射司機（DriverRegistry）；乘客 token 帶 `customers.id`，
  與 dispatch/tracking 發佈的 `Recipient{RoleCustomer, ride.CustomerID}` 對齊，
  登入後即可用該 token 連 WS 收自己這趟的事件。
- **前向相容 LINE Login**：身分鍵仍是 line_user_id；日後接 LINE Login 只換驗證方式，
  不動身分模型（此為設計 §12.4「可改決策」的踏腳石實作）。
- **測試**：CustomerRegistry 用假 store 做純邏輯單元測試；repo 層用 newMigratedTestDB。
```

- [ ] **Step 2：全量測試**

Run: `cd /Users/mac/Documents/line-fleet-dispatch && CGO_ENABLED=0 go test ./... 2>&1 | tail -18`
Expected: 全綠（含 testcontainers 整合測試）。

- [ ] **Step 3：Commit 與 push**

```bash
git -C /Users/mac/Documents/line-fleet-dispatch add docs/decisions.md
git -C /Users/mac/Documents/line-fleet-dispatch commit -m "docs(customer): 補乘客認證決策紀錄

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
git -C /Users/mac/Documents/line-fleet-dispatch push origin main
```

---

## Self-Review

**1. Coverage：** 乘客註冊/登入（Task 2/4）、密碼儲存（Task 1）、乘客 JWT + 中介層（Task 3）、WS 訂閱解鎖（乘客 token 帶 customers.id，既有 WS handler 已支援 role=customer，無需改）✅
**2. Placeholder scan：** 無 TBD/TODO；每個 code step 為完整可貼程式碼。
**3. Type consistency：**
- `auth.GenerateToken("customer", id, ...)` / `ParseToken`（M5-WS 已實作）於 Task 3/4 一致 ✅
- `customerStore` 介面（Task 2）方法與 `CustomerRepository`（Task 1 新增 SetPassword/FindByID + 既有 FindByLineUserID/FindOrCreateByLineUserID）一致——`*repository.CustomerRepository` 滿足介面 ✅
- `CustomerRegistry` 建構子接受介面，main.go 傳入 `*repository.CustomerRepository`（Task 4）✅
**4. 依賴順序：** 1→2→3→4→5。Task 4 依賴 2/3；Task 5 最後。正確。
**5. 前向相容檢查：** 乘客事件在 dispatch.go/tracking.go 已以 `ride.CustomerID`（=customers.id）發佈；乘客 token 的 sub_id 也是 customers.id，故 WS 路由自動對上，本 plan 不需改 WS/dispatch/tracking。✅
