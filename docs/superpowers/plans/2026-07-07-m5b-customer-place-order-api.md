# 乘客 App 下單 API（POST /api/rides）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 新增 `POST /api/rides`（乘客 JWT），讓 App 不經 LINE 直接叫車，複用既有派單核心；解鎖乘客端 App 端到端（gap 清單 P0 #1）。

**Architecture:** 沿用 `Handler → Service → Repository` 三層。新增 `RideService.CreateByCustomer`（鏡射既有 `CreateFromLocation`，但身分改吃 JWT 的 `customer_id` 而非 LINE `line_user_id`），下單前擋「同一乘客已有進行中訂單」，建立後沿用既有非同步 `DispatchService.Dispatch`。Handler 掛在 `CustomerAuth` 群組下。

**Tech Stack:** Go 1.x、Gin、GORM、PostGIS、Redis、testcontainers-go、既有 JWT（`internal/auth` role=customer）。

## Global Constraints

- 分層鐵律：Handler 不碰 DB，只呼叫 Service；Service 只透過 Repository/Redis。
- `driver_id`/`customer_id` 一律取自 JWT（`middleware.CustomerIDFromCtx`），**不信任 request body 的身分欄位**。
- log / 錯誤訊息用繁體中文（與 repo 一致）。
- 座標順序 PostGIS 為 `lng, lat`；`model.GeoPoint{Lat, Lng}` 的 `Value()` 已處理，勿自行拼 WKT。
- 每個 Task 結束都要 `go build ./... && go vet ./...` 綠燈才 commit。
- commit 後 push 到 `main`（fleet 慣例，push 用 repo 內 `core.sshCommand` 的 thothawei 金鑰）。

## 既有可複用資產（實測，勿重造）

- `middleware.CustomerAuth(secret)` → 驗 role=customer，`c.Set("customer_id", id)`；`middleware.CustomerIDFromCtx(c) int64` 取出。無 token→401、非 customer→403。
- `repository.CustomerRepository.FindByID(id int64) (*model.Customer, error)`（not found 回 `gorm.ErrRecordNotFound`）。`model.Customer` 有 `LineUserID` 欄位。
- `repository.RideRepository.FindActiveByCustomer(customerID int64) (*model.Ride, error)`：有進行中訂單回該筆，**無則回 `(nil, nil)`**。
- `repository.RideRepository.Create(ride *model.Ride) error`。
- `redis.Store.AllowRateLimit(ctx, lineUserID string, maxPerMin int) (bool, error)`。
- `constants.RideStatusRequested int16 = 0`。
- `service.RideService`（欄位：`customers, rides, redis, dispatch`）與其既有方法 `CreateFromLocation`（作為鏡射範本）。
- `service/errors.go` 既有 sentinel：`ErrForbidden / ErrNotFound / ErrInvalidCredentials`。
- 測試搭台：`repository` 套件有 `newMigratedTestDB(t)`（起真 PostGIS + 跑 migration；Docker 不可用時 skip）。handler 測試樣式見 `internal/handler/customer_test.go`（純 httptest，只測授權邊界）。

---

### Task 1: Service 錯誤常數 + 座標驗證（純單元 TDD）

**Files:**
- Modify: `internal/service/errors.go`
- Create: `internal/service/ride_validate.go`
- Test: `internal/service/ride_validate_test.go`

**Interfaces:**
- Produces:
  - `service.ErrInvalidCoords error`、`service.ErrActiveRideExists error`、`service.ErrRateLimited error`
  - `service.validatePickupCoords(lat, lng float64) error`（回 `ErrInvalidCoords` 或 `nil`）

- [ ] **Step 1: 寫失敗測試**

`internal/service/ride_validate_test.go`：
```go
package service

import (
	"errors"
	"testing"
)

func TestValidatePickupCoords(t *testing.T) {
	cases := []struct {
		name    string
		lat     float64
		lng     float64
		wantErr bool
	}{
		{"台北合法座標", 25.0330, 121.5654, false},
		{"全為零視為無效", 0, 0, true},
		{"緯度超界", 91, 121, true},
		{"經度超界", 25, 181, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePickupCoords(tc.lat, tc.lng)
			if tc.wantErr && !errors.Is(err, ErrInvalidCoords) {
				t.Fatalf("預期 ErrInvalidCoords，得到 %v", err)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("預期 nil，得到 %v", err)
			}
		})
	}
}
```

- [ ] **Step 2: 跑測試確認失敗**

Run: `go test ./internal/service/ -run TestValidatePickupCoords -v`
Expected: 編譯失敗（`undefined: validatePickupCoords` / `ErrInvalidCoords`）。

- [ ] **Step 3: 加 sentinel 錯誤**

`internal/service/errors.go` 的 `var (...)` 區塊內補三行：
```go
	ErrInvalidCoords    = errors.New("無效的上車座標")
	ErrActiveRideExists = errors.New("已有進行中的訂單")
	ErrRateLimited      = errors.New("叫車太頻繁，請稍後再試")
```

- [ ] **Step 4: 實作驗證函式**

`internal/service/ride_validate.go`：
```go
package service

// validatePickupCoords 檢查上車座標落在合法經緯度範圍且非 (0,0)。
func validatePickupCoords(lat, lng float64) error {
	if lat < -90 || lat > 90 || lng < -180 || lng > 180 {
		return ErrInvalidCoords
	}
	if lat == 0 && lng == 0 {
		return ErrInvalidCoords
	}
	return nil
}
```

- [ ] **Step 5: 跑測試確認通過**

Run: `go test ./internal/service/ -run TestValidatePickupCoords -v`
Expected: PASS（4 個子測試全綠）。

- [ ] **Step 6: build/vet + commit**

```bash
go build ./... && go vet ./...
git add internal/service/errors.go internal/service/ride_validate.go internal/service/ride_validate_test.go
git commit -m "feat(ride): 乘客下單用錯誤常數與座標驗證"
```

---

### Task 2: RideService.CreateByCustomer（服務層）+ 進行中訂單守門整合測試

**Files:**
- Modify: `internal/service/ride.go`
- Test: `internal/repository/ride_active_integration_test.go`

**Interfaces:**
- Consumes: Task 1 的 `validatePickupCoords`、`ErrActiveRideExists`、`ErrRateLimited`；既有 `CustomerRepository.FindByID`、`RideRepository.FindActiveByCustomer`、`RideRepository.Create`、`redis.Store.AllowRateLimit`。
- Produces:
  - `func (s *RideService) CreateByCustomer(ctx context.Context, customerID int64, pickupLat, pickupLng float64, pickupAddress string) (*model.Ride, error)`

> 說明：本服務方法把 redis + 多個 repo + 非同步派單串起來，鏡射既有未做 Go 測的 `CreateFromLocation`（該方法由 docker e2e 驗證）。**單元可測的座標驗證已在 Task 1 覆蓋**；**「有進行中訂單就擋」的 DB 保證用 repository 整合測試坐實**（本 Task）；**完整下單→派單→重複擋** 由 Task 4 的 e2e smoke 驗證。

- [ ] **Step 1: 寫失敗的整合測試（守門查詢）**

`internal/repository/ride_active_integration_test.go`：
```go
package repository

import (
	"testing"
	"time"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/model"
)

func TestFindActiveByCustomer_守門(t *testing.T) {
	db := newMigratedTestDB(t) // Docker 不可用時內部 t.Skip
	customers := NewCustomerRepository(db)
	rides := NewRideRepository(db)

	cust, err := customers.FindOrCreateByLineUserID("U_active_guard", "測試乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}

	// 尚無訂單 → 回 (nil, nil)
	got, err := rides.FindActiveByCustomer(cust.ID)
	if err != nil {
		t.Fatalf("查詢失敗：%v", err)
	}
	if got != nil {
		t.Fatalf("預期無進行中訂單，卻得到 ride id=%d", got.ID)
	}

	// 建一筆 Requested → 應被視為進行中
	now := time.Now()
	ride := &model.Ride{
		CustomerID:    cust.ID,
		Status:        constants.RideStatusRequested,
		PickupPoint:   model.GeoPoint{Lat: 25.03, Lng: 121.56},
		PickupAddress: "台北車站",
		RequestedAt:   now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := rides.Create(ride); err != nil {
		t.Fatalf("建立訂單失敗：%v", err)
	}
	got, err = rides.FindActiveByCustomer(cust.ID)
	if err != nil {
		t.Fatalf("查詢失敗：%v", err)
	}
	if got == nil || got.ID != ride.ID {
		t.Fatalf("預期回進行中訂單 id=%d，得到 %v", ride.ID, got)
	}
}
```

- [ ] **Step 2: 跑測試確認會執行（或在無 Docker 環境 skip）**

Run: `go test ./internal/repository/ -run TestFindActiveByCustomer_守門 -v`
Expected: 有 Docker → PASS；無 Docker → `SKIP`（`newMigratedTestDB` 內建跳過）。**若 PASS/SKIP 以外的失敗（如 migration 欄位不符）才需修。**

- [ ] **Step 3: 實作 CreateByCustomer**

在 `internal/service/ride.go` 的 `CreateFromLocation` 之後插入：
```go
// CreateByCustomer 供已登入乘客（App）直接叫車：身分取自 JWT 的 customer_id。
// 下單前擋「同一乘客已有進行中訂單」，建立後沿用非同步派單。
func (s *RideService) CreateByCustomer(
	ctx context.Context,
	customerID int64,
	pickupLat, pickupLng float64,
	pickupAddress string,
) (*model.Ride, error) {
	if err := validatePickupCoords(pickupLat, pickupLng); err != nil {
		return nil, err
	}

	customer, err := s.customers.FindByID(customerID)
	if err != nil {
		return nil, err
	}

	allowed, err := s.redis.AllowRateLimit(ctx, customer.LineUserID, 5)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, ErrRateLimited
	}

	active, err := s.rides.FindActiveByCustomer(customerID)
	if err != nil {
		return nil, err
	}
	if active != nil {
		return nil, ErrActiveRideExists
	}

	now := time.Now()
	ride := &model.Ride{
		CustomerID:    customer.ID,
		Status:        constants.RideStatusRequested,
		PickupPoint:   model.GeoPoint{Lat: pickupLat, Lng: pickupLng},
		PickupAddress: pickupAddress,
		RequestedAt:   now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.rides.Create(ride); err != nil {
		return nil, err
	}

	// 非同步派單（與 CreateFromLocation 一致）
	go func(rideID int64) {
		_ = s.dispatch.Dispatch(context.Background(), rideID)
	}(ride.ID)

	return ride, nil
}
```

- [ ] **Step 4: build/vet + 跑整合測試**

Run:
```bash
go build ./... && go vet ./...
go test ./internal/repository/ -run TestFindActiveByCustomer_守門 -v
```
Expected: build/vet 綠；測試 PASS 或 SKIP。

- [ ] **Step 5: Commit**

```bash
git add internal/service/ride.go internal/repository/ride_active_integration_test.go
git commit -m "feat(ride): RideService.CreateByCustomer 乘客下單（含進行中訂單守門）"
```

---

### Task 3: Handler + 路由 + 授權/綁定測試（httptest）

**Files:**
- Modify: `internal/handler/ride.go`（`RideHandler` 加 `rideService` 欄位與 `Create` 方法、`createStatusForErr`）
- Modify: `cmd/server/main.go`（`NewRideHandler` 多傳 `rideService`；註冊 `POST /api/rides` 於 CustomerAuth 群組）
- Test: `internal/handler/ride_create_test.go`

**Interfaces:**
- Consumes: Task 2 的 `RideService.CreateByCustomer`；`service.ErrInvalidCoords/ErrActiveRideExists/ErrRateLimited`；`middleware.CustomerAuth`、`middleware.CustomerIDFromCtx`。
- Produces:
  - `func (h *RideHandler) Create(c *gin.Context)` → 路由 `POST /api/rides`
  - 成功回 `200 {"ride_id": <int64>, "status": <int16>}`

- [ ] **Step 1: 寫失敗測試（授權邊界 + 綁定 400）**

`internal/handler/ride_create_test.go`：
```go
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

// 驗「真」RideHandler.Create 的授權/綁定邊界（401/403/400）。這三條路徑都在
// 呼叫 rideService 之前返回（401/403 由中介層攔截、400 由綁定失敗即返回），
// 故可用 nil 依賴的 RideService 建構、不需 DB/redis。
// 成功下單(200)與重複擋(409)的語意由 Task 4 的 docker e2e smoke 覆蓋。
func setupCreateRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := NewRideHandler(nil, nil, nil, service.NewRideService(nil, nil, nil, nil))
	r := gin.New()
	r.POST("/api/rides", middleware.CustomerAuth("s"), h.Create)
	return r
}

func TestCreateRide_授權與綁定邊界(t *testing.T) {
	r := setupCreateRouter()

	// 無 token → 401（中介層攔截，未進 handler）
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/rides", strings.NewReader(`{"pickup_lat":25,"pickup_lng":121}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("無 token 預期 401，得到 %d", w.Code)
	}

	// driver token → 403（角色不符，中介層攔截）
	dtok, _ := auth.GenerateToken("driver", 7, "s", time.Hour)
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("POST", "/api/rides", strings.NewReader(`{"pickup_lat":25,"pickup_lng":121}`))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer "+dtok)
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusForbidden {
		t.Fatalf("driver token 預期 403，得到 %d", w2.Code)
	}

	// customer token + 壞 JSON → 400（Create 綁定失敗即返回，未觸及 rideService）
	ctok, _ := auth.GenerateToken("customer", 9, "s", time.Hour)
	w3 := httptest.NewRecorder()
	req3, _ := http.NewRequest("POST", "/api/rides", strings.NewReader(`{bad`))
	req3.Header.Set("Content-Type", "application/json")
	req3.Header.Set("Authorization", "Bearer "+ctok)
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusBadRequest {
		t.Fatalf("壞 JSON 預期 400，得到 %d", w3.Code)
	}
}
```

- [ ] **Step 2: 跑測試確認失敗（RED）**

Run: `go test ./internal/handler/ -run TestCreateRide_授權與綁定邊界 -v`
Expected: **編譯失敗**——`too many arguments in call to NewRideHandler` 與 `h.Create undefined`（因 4 參數建構子與 `Create` 方法尚未實作）。這就是 TDD 紅燈；Step 3~5 實作後轉綠。

- [ ] **Step 3: 改 RideHandler 加欄位與建構子參數**

`internal/handler/ride.go` 內：
```go
type RideHandler struct {
	dispatch    *service.DispatchService
	tracking    *service.TrackingService
	rides       *service.RideQueryService
	rideService *service.RideService
}

func NewRideHandler(
	dispatch *service.DispatchService,
	tracking *service.TrackingService,
	rides *service.RideQueryService,
	rideService *service.RideService,
) *RideHandler {
	return &RideHandler{dispatch: dispatch, tracking: tracking, rides: rides, rideService: rideService}
}
```

- [ ] **Step 4: 加 Create 方法與錯誤對應**

`internal/handler/ride.go` 檔尾（`Track` 之後）：
```go
// Create POST /api/rides — 乘客 App 直接叫車（customer_id 取自 JWT）
func (h *RideHandler) Create(c *gin.Context) {
	customerID := middleware.CustomerIDFromCtx(c)
	var req struct {
		PickupLat     float64 `json:"pickup_lat"`
		PickupLng     float64 `json:"pickup_lng"`
		PickupAddress string  `json:"pickup_address"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "參數錯誤"})
		return
	}
	ride, err := h.rideService.CreateByCustomer(
		c.Request.Context(), customerID, req.PickupLat, req.PickupLng, req.PickupAddress,
	)
	if err != nil {
		c.JSON(createStatusForErr(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ride_id": ride.ID, "status": ride.Status})
}

// createStatusForErr 將下單錯誤對應到 HTTP 狀態碼。
func createStatusForErr(err error) int {
	switch {
	case errors.Is(err, service.ErrInvalidCoords):
		return http.StatusBadRequest
	case errors.Is(err, service.ErrActiveRideExists):
		return http.StatusConflict
	case errors.Is(err, service.ErrRateLimited):
		return http.StatusTooManyRequests
	default:
		return http.StatusInternalServerError
	}
}
```

- [ ] **Step 5: main.go 傳入 rideService 並註冊路由**

`cmd/server/main.go`：
1. 找到 `rideHandler := handler.NewRideHandler(dispatchService, trackingService, rideQueryService)`，改為多傳既有的 `rideService` 變數（LINE webhook 已在用它，故該變數已存在於本檔）：
```go
	rideHandler := handler.NewRideHandler(dispatchService, trackingService, rideQueryService, rideService)
```
2. 在 `api := r.Group("/api")` 區塊內、公開註冊/登入之後，新增乘客 JWT 群組（與既有 driver `authed` 群組並列）：
```go
		// 受乘客 JWT 保護：App 下單
		customerAuthed := api.Group("")
		customerAuthed.Use(middleware.CustomerAuth(cfg.JWTSecret))
		{
			customerAuthed.POST("/rides", rideHandler.Create)
		}
```

- [ ] **Step 6: build/vet + 全套測試**

Run:
```bash
go build ./... && go vet ./...
go test ./internal/handler/ -run TestCreateRide_授權與綁定邊界 -v
```
Expected: build/vet 綠；測試 PASS。

- [ ] **Step 7: Commit**

```bash
git add internal/handler/ride.go internal/handler/ride_create_test.go cmd/server/main.go
git commit -m "feat(ride): POST /api/rides 乘客下單端點與路由"
```

---

### Task 4: Docker 端到端 smoke（成功下單 + 重複擋）

**Files:**
- Create: `scripts/smoke_place_order.sh`

**Interfaces:**
- Consumes: 完整服務（`docker compose up`）、`/api/customer/register`、`/api/customer/login`、`POST /api/rides`。

- [ ] **Step 1: 寫 smoke 腳本**

`scripts/smoke_place_order.sh`：
```bash
#!/usr/bin/env bash
# 乘客 App 下單端到端驗證：註冊→登入→下單(200)→重複下單(409)。
set -euo pipefail
BASE="${BASE:-http://localhost:8080}"
LID="U_smoke_$(date +%s)"

echo "== 註冊乘客 =="
curl -s -X POST "$BASE/api/customer/register" \
  -H 'Content-Type: application/json' \
  -d "{\"line_user_id\":\"$LID\",\"name\":\"smoke\",\"password\":\"pw123456\"}" | tee /dev/stderr

echo; echo "== 登入取 token =="
TOKEN=$(curl -s -X POST "$BASE/api/customer/login" \
  -H 'Content-Type: application/json' \
  -d "{\"line_user_id\":\"$LID\",\"password\":\"pw123456\"}" | sed -E 's/.*"token":"([^"]+)".*/\1/')
test -n "$TOKEN" || { echo "取 token 失敗"; exit 1; }

echo "== 第一次下單（預期 200 + ride_id）=="
CODE1=$(curl -s -o /tmp/ride1.json -w '%{http_code}' -X POST "$BASE/api/rides" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"pickup_lat":25.0330,"pickup_lng":121.5654,"pickup_address":"台北車站"}')
cat /tmp/ride1.json; echo
test "$CODE1" = "200" || { echo "第一次下單預期 200，得到 $CODE1"; exit 1; }

echo "== 第二次下單（已有進行中訂單，預期 409）=="
CODE2=$(curl -s -o /tmp/ride2.json -w '%{http_code}' -X POST "$BASE/api/rides" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"pickup_lat":25.0330,"pickup_lng":121.5654,"pickup_address":"台北車站"}')
cat /tmp/ride2.json; echo
test "$CODE2" = "409" || { echo "第二次下單預期 409，得到 $CODE2"; exit 1; }

echo "== smoke PASS =="
```

- [ ] **Step 2: 起服務並執行**

Run:
```bash
docker compose up --build -d
chmod +x scripts/smoke_place_order.sh
BASE=http://localhost:8080 bash scripts/smoke_place_order.sh
```
Expected 輸出結尾：`== smoke PASS ==`（第一次 200 回 `{"ride_id":...,"status":0}`；第二次 409 回 `{"error":"已有進行中的訂單"}`）。
（若無在線司機，訂單經重派逾時會自動取消；smoke 只驗下單與重複擋，不驗接單。）

- [ ] **Step 3: Commit**

```bash
git add scripts/smoke_place_order.sh
git commit -m "test(ride): 乘客下單端到端 smoke 腳本"
```

- [ ] **Step 4: 收尾**

- 回填本檔與 [backend-api-gaps.md](../../backend-api-gaps.md) P0 #1 的勾選框。
- `git push`（main）。

---

## Self-Review（對照 spec/gap 清單）

- **涵蓋度**：gap P0 #1「`POST /api/rides` App 下單」→ Task 1~4 完整覆蓋（驗證/服務/端點/e2e）。附帶「重複下單守門」為額外正確性保護，非 spec 明列但屬合理防呆。
- **未涵蓋（刻意，屬其他 gap 項）**：查自己訂單 `GET /api/customer/rides/active`（P0 #2）、乘客取消（P0 #4）另立計畫。本計畫不擴張。
- **型別一致性**：`CreateByCustomer(ctx, customerID int64, pickupLat, pickupLng float64, pickupAddress string) (*model.Ride, error)` 在 Task 2 定義、Task 3 handler 呼叫一致；`ErrInvalidCoords/ErrActiveRideExists/ErrRateLimited` Task 1 定義、Task 2/3 使用一致；`NewRideHandler` 新增第 4 參數在 handler 定義與 main.go 呼叫一致。
- **無佔位符**：所有步驟含實際程式碼與可執行指令、預期輸出。

## 相依與後續
- 本端點就緒後，乘客 App（M7）的「叫車」畫面即可對接；接著做 P0 #2（查進行中訂單以 WS 訂閱）→ #4（乘客取消）。
- 寫入端點的 `ride_events` 審計（gap D4）尚未納入；待審計表建立後可在 `CreateByCustomer` 補記一筆 `REQUESTED` 事件。
