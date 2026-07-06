# M5-WS：WebSocket 即時通道 + 事件廣播 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 `line-fleet-dispatch` 加入 JWT 驗證的 WebSocket 即時通道，讓司機/乘客/後台能即時收到派單、訂單狀態轉換、司機位置事件——不改動既有派單/訂單業務邏輯，只在既有 LINE 推播點旁「多發一份結構化事件」。

**Architecture:** 新增 `internal/events`（Event/Recipient 型別 + Publisher 介面 + Hub 實作，Go 原生 goroutine+channel）與 `internal/handler/ws.go`（JWT 驗證後升級連線）。`DispatchService`/`TrackingService` 各注入一個 nil-safe `events.Publisher`，在既有 `s.line.Push*` 呼叫旁 `s.publisher.Publish(...)`。Hub 依 `Recipient{Role,ID}` 把事件路由到訂閱的連線；後台車隊事件用 `Recipient{Role:"admin", ID:0}` 廣播。

**Tech Stack:** Go 1.25 / Gin / gorilla/websocket / golang-jwt v5（沿用既有）/ zerolog / go test（含既有 testcontainers 慣例）。

## Global Constraints

- Go module：`line-fleet-dispatch`，Go 1.25.0（見 `go.mod`）。
- 既有分層 `Handler → Service → Repository → DB/Redis` 不破壞；業務邏輯（dispatch/ride/tracking）不重寫，只加事件發佈點。
- 註解、log、面向使用者訊息一律**繁體中文**；識別字用英文。
- JWT 沿用既有 `internal/auth`：HS256、secret 來自 `cfg.JWTSecret`，`RegisteredClaims.Subject` 已用來標角色（既有司機 token `Subject="driver"`）。
- 服務注入的 `events.Publisher` 必須 **nil-safe**：未接 hub 時（例如既有測試）服務照常運作。
- 每個 Task 結尾都要 `git commit`；訊息用中文、附既有 Co-Authored-By 慣例（見既有 git log）。
- 測試優先（TDD）：先寫失敗測試 → 跑到紅 → 最小實作 → 跑到綠 → commit。

---

### Task 1：新增 gorilla/websocket 依賴與 WS 設定

**Files:**
- Modify: `go.mod`（新增 `github.com/gorilla/websocket`）
- Modify: `internal/config/config.go:37-42`（新增 WS 相關設定欄位）
- Modify: `internal/config/config.go:66-71`（Load 讀取）

**Interfaces:**
- Consumes: 既有 `config.Config`、`getEnvInt`
- Produces: `cfg.WSWriteWaitSec int`、`cfg.WSPongWaitSec int`、`cfg.WSMaxMessageBytes int`

- [ ] **Step 1：加入依賴**

Run:
```bash
cd /Users/mac/Documents/line-fleet-dispatch && go get github.com/gorilla/websocket@v1.5.3
```
Expected: `go.mod` 出現 `github.com/gorilla/websocket v1.5.3`，`go.sum` 更新。

- [ ] **Step 2：新增設定欄位**

在 `internal/config/config.go` 的 `Config` struct（`TrackRetentionMonths int` 之後）加入：
```go
	WSWriteWaitSec    int
	WSPongWaitSec     int
	WSMaxMessageBytes int
```

在 `Load()` 的 `cfg := &Config{...}`（`TrackRetentionMonths` 那行之後）加入：
```go
		WSWriteWaitSec:    getEnvInt("WS_WRITE_WAIT_SEC", 10),
		WSPongWaitSec:     getEnvInt("WS_PONG_WAIT_SEC", 60),
		WSMaxMessageBytes: getEnvInt("WS_MAX_MESSAGE_BYTES", 4096),
```

- [ ] **Step 3：驗證編譯**

Run: `cd /Users/mac/Documents/line-fleet-dispatch && go build ./...`
Expected: 無錯誤。

- [ ] **Step 4：Commit**

```bash
git add go.mod go.sum internal/config/config.go
git commit -m "feat(ws): 新增 gorilla/websocket 依賴與 WS 連線設定

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2：事件型別與 Publisher 介面（`internal/events/event.go`）

**Files:**
- Create: `internal/events/event.go`
- Test: `internal/events/event_test.go`

**Interfaces:**
- Produces:
  - `type Recipient struct { Role string; ID int64 }`
  - 角色常數 `RoleDriver="driver"`、`RoleCustomer="customer"`、`RoleAdmin="admin"`
  - `type Event struct { Type string ...; RideID int64 ...; Payload map[string]any ... }`
  - 事件型別常數 `TypeRideAssigned`、`TypeRideAccepted`、`TypeDriverLocation`、`TypeDriverArrived`、`TypeRidePickedUp`、`TypeRideCompleted`、`TypeRideCancelled`
  - `type Publisher interface { Publish(rec Recipient, ev Event) }`
  - `func (e Event) JSON() ([]byte, error)`

- [ ] **Step 1：寫失敗測試**

Create `internal/events/event_test.go`：
```go
package events

import (
	"encoding/json"
	"testing"
)

func TestEventJSON_含型別與 payload(t *testing.T) {
	ev := Event{
		Type:    TypeRideAccepted,
		RideID:  42,
		Payload: map[string]any{"driver_name": "阿明", "eta_sec": 300},
	}
	raw, err := ev.JSON()
	if err != nil {
		t.Fatalf("序列化失敗: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("反序列化失敗: %v", err)
	}
	if got["type"] != TypeRideAccepted {
		t.Errorf("type 錯誤: %v", got["type"])
	}
	if got["ride_id"].(float64) != 42 {
		t.Errorf("ride_id 錯誤: %v", got["ride_id"])
	}
	payload := got["payload"].(map[string]any)
	if payload["driver_name"] != "阿明" {
		t.Errorf("payload.driver_name 錯誤: %v", payload["driver_name"])
	}
}

func TestRecipient_角色常數(t *testing.T) {
	if RoleDriver != "driver" || RoleCustomer != "customer" || RoleAdmin != "admin" {
		t.Fatal("角色常數值不符預期")
	}
}
```

- [ ] **Step 2：跑測試確認失敗**

Run: `cd /Users/mac/Documents/line-fleet-dispatch && go test ./internal/events/ -run TestEventJSON -v`
Expected: FAIL（`undefined: Event` 等，套件尚未建立）。

- [ ] **Step 3：最小實作**

Create `internal/events/event.go`：
```go
// Package events 定義即時事件的型別與發佈介面。
// Publisher 由 WebSocket Hub 實作；業務服務只依賴此介面，方便測試替身注入。
package events

import "encoding/json"

// 角色：對應 JWT Subject
const (
	RoleDriver   = "driver"
	RoleCustomer = "customer"
	RoleAdmin    = "admin"
)

// 事件型別
const (
	TypeRideAssigned   = "ride.assigned"    // 已派單給司機（待接）
	TypeRideAccepted   = "ride.accepted"    // 司機已接單
	TypeDriverLocation = "driver.location"  // 司機位置更新（後台車隊 / 乘客追蹤）
	TypeDriverArrived  = "driver.arrived"   // 司機進入上車圍籬
	TypeRidePickedUp   = "ride.picked_up"   // 乘客已上車
	TypeRideCompleted  = "ride.completed"   // 行程完成
	TypeRideCancelled  = "ride.cancelled"   // 行程取消
)

// Recipient 事件收件人。ID=0 代表該角色的廣播（例如後台車隊看板）。
type Recipient struct {
	Role string
	ID   int64
}

// Event 送往前端的即時事件。
type Event struct {
	Type    string         `json:"type"`
	RideID  int64          `json:"ride_id,omitempty"`
	Payload map[string]any `json:"payload,omitempty"`
}

// JSON 序列化為前端可解析的位元組。
func (e Event) JSON() ([]byte, error) {
	return json.Marshal(e)
}

// Publisher 發佈事件到對應收件人。Hub 為其唯一實作。
type Publisher interface {
	Publish(rec Recipient, ev Event)
}
```

- [ ] **Step 4：跑測試確認通過**

Run: `cd /Users/mac/Documents/line-fleet-dispatch && go test ./internal/events/ -v`
Expected: PASS。

- [ ] **Step 5：Commit**

```bash
git add internal/events/event.go internal/events/event_test.go
git commit -m "feat(ws): events 事件型別與 Publisher 介面

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 3：Hub 連線管理與事件路由（`internal/events/hub.go`）

**Files:**
- Create: `internal/events/hub.go`
- Test: `internal/events/hub_test.go`

**Interfaces:**
- Consumes: `Recipient`、`Event`、`Publisher`（Task 2）
- Produces:
  - `type Client struct { Rec Recipient; Send chan []byte }`
  - `type Hub struct{...}`
  - `func NewHub() *Hub`
  - `func (h *Hub) Run()`（阻塞式事件迴圈，於 goroutine 啟動）
  - `func (h *Hub) Register(c *Client)`
  - `func (h *Hub) Unregister(c *Client)`
  - `func (h *Hub) Publish(rec Recipient, ev Event)`（實作 `Publisher`）
  - `func (h *Hub) ClientCount() int`（測試/監控用）

**設計要點：** Hub 用單一 goroutine（`Run`）序列化 register/unregister/publish，避免 map 競態。`Publish` 把事件丟進 buffered channel；`Run` 迴圈取出後比對 `Recipient` 送到對應 `Client.Send`（非阻塞 select，滿了就丟棄該則，避免慢客戶拖垮 Hub）。後台廣播事件（`ID==0`）送給所有 `Role` 相符的 client。

- [ ] **Step 1：寫失敗測試**

Create `internal/events/hub_test.go`：
```go
package events

import (
	"testing"
	"time"
)

func waitMsg(t *testing.T, c *Client) []byte {
	t.Helper()
	select {
	case m := <-c.Send:
		return m
	case <-time.After(time.Second):
		t.Fatal("等待事件逾時")
		return nil
	}
}

func TestHub_定向送給指定收件人(t *testing.T) {
	h := NewHub()
	go h.Run()

	cust := &Client{Rec: Recipient{Role: RoleCustomer, ID: 7}, Send: make(chan []byte, 4)}
	other := &Client{Rec: Recipient{Role: RoleCustomer, ID: 99}, Send: make(chan []byte, 4)}
	h.Register(cust)
	h.Register(other)
	// 等 Run 消化 register
	time.Sleep(20 * time.Millisecond)

	h.Publish(Recipient{Role: RoleCustomer, ID: 7}, Event{Type: TypeRideAccepted, RideID: 1})

	msg := waitMsg(t, cust)
	if len(msg) == 0 {
		t.Fatal("指定收件人未收到事件")
	}
	select {
	case <-other.Send:
		t.Fatal("非收件人不應收到事件")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestHub_後台廣播送給所有 admin(t *testing.T) {
	h := NewHub()
	go h.Run()
	a1 := &Client{Rec: Recipient{Role: RoleAdmin, ID: 1}, Send: make(chan []byte, 4)}
	a2 := &Client{Rec: Recipient{Role: RoleAdmin, ID: 2}, Send: make(chan []byte, 4)}
	h.Register(a1)
	h.Register(a2)
	time.Sleep(20 * time.Millisecond)

	h.Publish(Recipient{Role: RoleAdmin, ID: 0}, Event{Type: TypeDriverLocation})

	waitMsg(t, a1)
	waitMsg(t, a2)
}

func TestHub_註銷後不再收到(t *testing.T) {
	h := NewHub()
	go h.Run()
	c := &Client{Rec: Recipient{Role: RoleDriver, ID: 5}, Send: make(chan []byte, 4)}
	h.Register(c)
	time.Sleep(20 * time.Millisecond)
	h.Unregister(c)
	time.Sleep(20 * time.Millisecond)

	h.Publish(Recipient{Role: RoleDriver, ID: 5}, Event{Type: TypeRideAssigned})
	select {
	case <-c.Send:
		t.Fatal("註銷後不應再收到事件")
	case <-time.After(100 * time.Millisecond):
	}
}
```

- [ ] **Step 2：跑測試確認失敗**

Run: `cd /Users/mac/Documents/line-fleet-dispatch && go test ./internal/events/ -run TestHub -v`
Expected: FAIL（`undefined: NewHub` / `Client`）。

- [ ] **Step 3：最小實作**

Create `internal/events/hub.go`：
```go
package events

import "github.com/rs/zerolog/log"

// Client 一條 WebSocket 連線在 Hub 中的代表。
type Client struct {
	Rec  Recipient
	Send chan []byte // Hub → 連線的出站佇列
}

type publishReq struct {
	rec Recipient
	ev  Event
}

// Hub 以單一 goroutine 序列化連線註冊與事件路由，避免 map 競態。
type Hub struct {
	clients    map[*Client]bool
	register   chan *Client
	unregister chan *Client
	publishCh  chan publishReq
}

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		register:   make(chan *Client, 32),
		unregister: make(chan *Client, 32),
		publishCh:  make(chan publishReq, 256),
	}
}

// Run 事件迴圈，需在 goroutine 中啟動並常駐。
func (h *Hub) Run() {
	for {
		select {
		case c := <-h.register:
			h.clients[c] = true
		case c := <-h.unregister:
			if _, ok := h.clients[c]; ok {
				delete(h.clients, c)
				close(c.Send)
			}
		case req := <-h.publishCh:
			h.route(req)
		}
	}
}

// route 比對收件人送出事件；ID==0 為該角色廣播。
func (h *Hub) route(req publishReq) {
	raw, err := req.ev.JSON()
	if err != nil {
		log.Error().Err(err).Msg("事件序列化失敗")
		return
	}
	for c := range h.clients {
		if c.Rec.Role != req.rec.Role {
			continue
		}
		if req.rec.ID != 0 && c.Rec.ID != req.rec.ID {
			continue
		}
		select {
		case c.Send <- raw:
		default:
			// 慢客戶：佇列已滿則丟棄該則，避免拖垮 Hub
			log.Warn().Str("role", c.Rec.Role).Int64("id", c.Rec.ID).Msg("客戶端佇列已滿，丟棄事件")
		}
	}
}

func (h *Hub) Register(c *Client)   { h.register <- c }
func (h *Hub) Unregister(c *Client) { h.unregister <- c }

// Publish 實作 Publisher 介面。
func (h *Hub) Publish(rec Recipient, ev Event) {
	select {
	case h.publishCh <- publishReq{rec: rec, ev: ev}:
	default:
		log.Warn().Msg("Hub 發佈佇列已滿，丟棄事件")
	}
}
```

> 註：`ClientCount()` 若測試未用可省略；本測試未依賴它，不實作以免 YAGNI。

- [ ] **Step 4：跑測試確認通過（含競態偵測）**

Run: `cd /Users/mac/Documents/line-fleet-dispatch && go test ./internal/events/ -race -v`
Expected: PASS，無 race 警告。

- [ ] **Step 5：Commit**

```bash
git add internal/events/hub.go internal/events/hub_test.go
git commit -m "feat(ws): Hub 連線管理與事件路由（單 goroutine 序列化）

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 4：WS 用的角色化 JWT 簽發與解析（`internal/auth`）

**Files:**
- Modify: `internal/auth/jwt.go`
- Test: `internal/auth/jwt_test.go`（新增測試函式）

**Interfaces:**
- Consumes: 既有 `DriverClaims`、`GenerateDriverToken`、`ParseDriverToken`（不改動，保持相容）
- Produces:
  - `type SubjectClaims struct { Role string; SubjectID int64; jwt.RegisteredClaims }`
  - `func GenerateToken(role string, id int64, secret string, ttl time.Duration) (string, error)`
  - `func ParseToken(tokenStr, secret string) (role string, id int64, err error)`

**設計要點：** 既有司機 token 用 `DriverClaims{DriverID}` + `Subject="driver"`。WS 要同時支援 driver/customer/admin，新增通用 `SubjectClaims{Role, SubjectID}`。**既有司機登入流程不改**（仍發 `DriverClaims`）；WS 解析時需能吃既有司機 token → 故 `ParseToken` 先試 `SubjectClaims`，失敗再退回 `ParseDriverToken` 當 `role="driver"`。

- [ ] **Step 1：寫失敗測試**

在 `internal/auth/jwt_test.go` 末尾新增：
```go
func Test通用Token_簽發與解析(t *testing.T) {
	secret := "test-secret"
	tok, err := GenerateToken("customer", 88, secret, time.Hour)
	if err != nil {
		t.Fatalf("簽發失敗: %v", err)
	}
	role, id, err := ParseToken(tok, secret)
	if err != nil {
		t.Fatalf("解析失敗: %v", err)
	}
	if role != "customer" || id != 88 {
		t.Fatalf("解析結果錯誤: role=%s id=%d", role, id)
	}
}

func Test通用ParseToken_相容既有司機Token(t *testing.T) {
	secret := "test-secret"
	// 用既有司機簽發函式產生的 token，ParseToken 也要能解析為 driver
	tok, err := GenerateDriverToken(3, secret, time.Hour)
	if err != nil {
		t.Fatalf("司機簽發失敗: %v", err)
	}
	role, id, err := ParseToken(tok, secret)
	if err != nil {
		t.Fatalf("解析司機 token 失敗: %v", err)
	}
	if role != "driver" || id != 3 {
		t.Fatalf("司機 token 解析錯誤: role=%s id=%d", role, id)
	}
}

func Test通用ParseToken_錯誤密鑰被拒(t *testing.T) {
	tok, _ := GenerateToken("admin", 1, "secret-a", time.Hour)
	if _, _, err := ParseToken(tok, "secret-b"); err == nil {
		t.Fatal("錯誤密鑰應被拒絕")
	}
}
```

需確認 `jwt_test.go` 已 import `"time"`（既有測試應已用到；若無則加）。

- [ ] **Step 2：跑測試確認失敗**

Run: `cd /Users/mac/Documents/line-fleet-dispatch && go test ./internal/auth/ -run 通用 -v`
Expected: FAIL（`undefined: GenerateToken`）。

- [ ] **Step 3：最小實作**

在 `internal/auth/jwt.go` 末尾新增：
```go
// SubjectClaims 通用角色身分 claims（driver/customer/admin 共用）
type SubjectClaims struct {
	Role      string `json:"role"`
	SubjectID int64  `json:"sub_id"`
	jwt.RegisteredClaims
}

// GenerateToken 簽發通用角色 JWT。
func GenerateToken(role string, id int64, secret string, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := SubjectClaims{
		Role:      role,
		SubjectID: id,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   role,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// ParseToken 解析通用角色 JWT；為相容既有司機 token（DriverClaims），
// 若通用解析取不到 role/sub_id，退回以 ParseDriverToken 視為 driver。
func ParseToken(tokenStr, secret string) (string, int64, error) {
	claims := &SubjectClaims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return []byte(secret), nil
	})
	if err == nil && token.Valid && claims.Role != "" && claims.SubjectID != 0 {
		return claims.Role, claims.SubjectID, nil
	}
	// 相容既有司機 token（同一把密鑰簽發；錯誤密鑰會在此一併被拒）
	if driverID, derr := ParseDriverToken(tokenStr, secret); derr == nil {
		return "driver", driverID, nil
	}
	return "", 0, ErrInvalidToken
}
```

- [ ] **Step 4：跑測試確認通過**

Run: `cd /Users/mac/Documents/line-fleet-dispatch && go test ./internal/auth/ -v`
Expected: PASS（含既有測試，未回歸）。

- [ ] **Step 5：Commit**

```bash
git add internal/auth/jwt.go internal/auth/jwt_test.go
git commit -m "feat(ws): 通用角色 JWT 簽發/解析（相容既有司機 token）

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 5：WebSocket 連線 handler（`internal/handler/ws.go`）

**Files:**
- Create: `internal/handler/ws.go`
- Test: `internal/handler/ws_test.go`

**Interfaces:**
- Consumes: `events.Hub`、`events.Client`、`events.Recipient`（Task 2/3）、`auth.ParseToken`（Task 4）、`config`（Task 1 的 WS 設定）
- Produces:
  - `type WSHandler struct{...}`
  - `func NewWSHandler(hub *events.Hub, jwtSecret string, writeWaitSec, pongWaitSec, maxMsgBytes int) *WSHandler`
  - `func (h *WSHandler) Connect(c *gin.Context)`（GET `/ws?token=<jwt>`）

**設計要點：** 用 gorilla `Upgrader` 升級連線。token 從 query（`?token=`）或 `Authorization: Bearer` 取得（App 的 WS client 用 query 較單純）。驗證失敗回 401 不升級。升級後建立 `events.Client`、`hub.Register`，啟動 writePump（含 ping/pong keepalive），readPump 只負責讀 close/pong；任一端結束就 `hub.Unregister`。

- [ ] **Step 1：寫失敗測試（整合式：起 httptest server + 真 WS client）**

Create `internal/handler/ws_test.go`：
```go
package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"line-fleet-dispatch/internal/auth"
	"line-fleet-dispatch/internal/events"
)

func newTestWSServer(t *testing.T, secret string) (*httptest.Server, *events.Hub) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	hub := events.NewHub()
	go hub.Run()
	h := NewWSHandler(hub, secret, 10, 60, 4096)
	r := gin.New()
	r.GET("/ws", h.Connect)
	return httptest.NewServer(r), hub
}

func dialWS(t *testing.T, srvURL, token string) *websocket.Conn {
	t.Helper()
	u := "ws" + strings.TrimPrefix(srvURL, "http") + "/ws?token=" + token
	conn, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		t.Fatalf("WS 連線失敗: %v", err)
	}
	return conn
}

func TestWS_合法Token可連線並收到事件(t *testing.T) {
	secret := "ws-secret"
	srv, hub := newTestWSServer(t, secret)
	defer srv.Close()

	tok, _ := auth.GenerateToken(events.RoleCustomer, 7, secret, time.Hour)
	conn := dialWS(t, srv.URL, tok)
	defer conn.Close()

	// 等 handler 完成 register
	time.Sleep(50 * time.Millisecond)
	hub.Publish(events.Recipient{Role: events.RoleCustomer, ID: 7},
		events.Event{Type: events.TypeRideAccepted, RideID: 1})

	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("讀取事件失敗: %v", err)
	}
	if !strings.Contains(string(msg), events.TypeRideAccepted) {
		t.Fatalf("事件內容錯誤: %s", msg)
	}
}

func TestWS_無Token回401(t *testing.T) {
	secret := "ws-secret"
	srv, _ := newTestWSServer(t, secret)
	defer srv.Close()

	u := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	_, resp, err := websocket.DefaultDialer.Dial(u, nil)
	if err == nil {
		t.Fatal("無 token 應連線失敗")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("預期 401，得到: %v", resp)
	}
}
```

- [ ] **Step 2：跑測試確認失敗**

Run: `cd /Users/mac/Documents/line-fleet-dispatch && go test ./internal/handler/ -run TestWS -v`
Expected: FAIL（`undefined: NewWSHandler`）。

- [ ] **Step 3：最小實作**

Create `internal/handler/ws.go`：
```go
package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"

	"line-fleet-dispatch/internal/auth"
	"line-fleet-dispatch/internal/events"
)

// WSHandler 處理 WebSocket 升級與連線生命週期。
type WSHandler struct {
	hub         *events.Hub
	jwtSecret   string
	writeWait   time.Duration
	pongWait    time.Duration
	pingPeriod  time.Duration
	maxMsgBytes int64
	upgrader    websocket.Upgrader
}

func NewWSHandler(hub *events.Hub, jwtSecret string, writeWaitSec, pongWaitSec, maxMsgBytes int) *WSHandler {
	pong := time.Duration(pongWaitSec) * time.Second
	return &WSHandler{
		hub:         hub,
		jwtSecret:   jwtSecret,
		writeWait:   time.Duration(writeWaitSec) * time.Second,
		pongWait:    pong,
		pingPeriod:  (pong * 9) / 10,
		maxMsgBytes: int64(maxMsgBytes),
		upgrader: websocket.Upgrader{
			// 作品階段允許跨來源（App/後台不同網域）；正式環境應收斂白名單
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

// Connect GET /ws?token=<jwt>：驗證後升級連線，訂閱屬於自己角色/ID 的事件。
func (h *WSHandler) Connect(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		if hdr := c.GetHeader("Authorization"); len(hdr) > 7 && hdr[:7] == "Bearer " {
			token = hdr[7:]
		}
	}
	role, id, err := auth.ParseToken(token, h.jwtSecret)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "token 無效或已過期"})
		return
	}

	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Error().Err(err).Msg("WebSocket 升級失敗")
		return
	}

	client := &events.Client{
		Rec:  events.Recipient{Role: role, ID: id},
		Send: make(chan []byte, 64),
	}
	h.hub.Register(client)
	log.Info().Str("role", role).Int64("id", id).Msg("WebSocket 已連線")

	go h.writePump(conn, client)
	h.readPump(conn, client)
}

// readPump 讀取（僅處理 pong / close），連線結束則註銷。
func (h *WSHandler) readPump(conn *websocket.Conn, client *events.Client) {
	defer func() {
		h.hub.Unregister(client)
		_ = conn.Close()
	}()
	conn.SetReadLimit(h.maxMsgBytes)
	_ = conn.SetReadDeadline(time.Now().Add(h.pongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(h.pongWait))
	})
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}

// writePump 送出 Hub 事件並定期 ping 保活。
func (h *WSHandler) writePump(conn *websocket.Conn, client *events.Client) {
	ticker := time.NewTicker(h.pingPeriod)
	defer ticker.Stop()
	for {
		select {
		case msg, ok := <-client.Send:
			_ = conn.SetWriteDeadline(time.Now().Add(h.writeWait))
			if !ok { // Hub 已關閉此連線
				_ = conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = conn.SetWriteDeadline(time.Now().Add(h.writeWait))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
```

- [ ] **Step 4：跑測試確認通過**

Run: `cd /Users/mac/Documents/line-fleet-dispatch && go test ./internal/handler/ -run TestWS -v`
Expected: PASS。

- [ ] **Step 5：Commit**

```bash
git add internal/handler/ws.go internal/handler/ws_test.go
git commit -m "feat(ws): WebSocket 連線 handler（JWT 驗證 + ping/pong 保活）

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 6：把 Publisher 注入 DispatchService 並在關鍵事件發佈

**Files:**
- Modify: `internal/service/dispatch.go`（struct 欄位、建構子、發佈點）
- Test: `internal/service/dispatch_publish_test.go`

**Interfaces:**
- Consumes: `events.Publisher`、`events.Event`、`events.Recipient`（Task 2）
- Produces: `NewDispatchService` 尾端新增參數 `publisher events.Publisher`；`DispatchService` 新增 `publisher events.Publisher` 欄位，並提供內部 `func (s *DispatchService) publish(rec events.Recipient, ev events.Event)`（nil-safe）

**設計要點：** 不改派單決策邏輯，只在既有通知點旁「多發事件」。發佈點：
- `AcceptRide` 成功後：發 `TypeRideAccepted` 給乘客（`Recipient{RoleCustomer, ride.CustomerID}`，payload 帶司機名、eta_sec）與司機（`Recipient{RoleDriver, driverID}`）。
- `pushOffer` 內：發 `TypeRideAssigned` 給該司機（`Recipient{RoleDriver, driver.ID}`，payload 帶 ride_id、address）。
- `giveUpIfUnaccepted`/`CancelByCustomer`：發 `TypeRideCancelled` 給乘客。

- [ ] **Step 1：寫失敗測試（用假 Publisher 驗證發佈）**

Create `internal/service/dispatch_publish_test.go`：
```go
package service

import (
	"sync"
	"testing"

	"line-fleet-dispatch/internal/events"
)

// fakePublisher 記錄收到的發佈，供斷言
type fakePublisher struct {
	mu   sync.Mutex
	recv []struct {
		Rec events.Recipient
		Ev  events.Event
	}
}

func (f *fakePublisher) Publish(rec events.Recipient, ev events.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recv = append(f.recv, struct {
		Rec events.Recipient
		Ev  events.Event
	}{rec, ev})
}

func (f *fakePublisher) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.recv)
}

func TestDispatch_publish為nil時不panic(t *testing.T) {
	s := &DispatchService{} // publisher 為 nil
	// 不應 panic
	s.publish(events.Recipient{Role: events.RoleCustomer, ID: 1}, events.Event{Type: events.TypeRideAccepted})
}

func TestDispatch_publish轉發給Publisher(t *testing.T) {
	fp := &fakePublisher{}
	s := &DispatchService{publisher: fp}
	s.publish(events.Recipient{Role: events.RoleDriver, ID: 9}, events.Event{Type: events.TypeRideAssigned, RideID: 5})
	if fp.count() != 1 {
		t.Fatalf("預期發佈 1 則，得到 %d", fp.count())
	}
	if fp.recv[0].Rec.ID != 9 || fp.recv[0].Ev.Type != events.TypeRideAssigned {
		t.Fatalf("發佈內容錯誤: %+v", fp.recv[0])
	}
}
```

- [ ] **Step 2：跑測試確認失敗**

Run: `cd /Users/mac/Documents/line-fleet-dispatch && go test ./internal/service/ -run TestDispatch_publish -v`
Expected: FAIL（`DispatchService` 無 `publisher` 欄位、無 `publish` 方法）。

- [ ] **Step 3：實作 — 加欄位、建構子參數、nil-safe helper 與發佈點**

3a. `internal/service/dispatch.go` import 區加入：
```go
	"line-fleet-dispatch/internal/events"
```

3b. `DispatchService` struct 末尾（`maxAttempts int` 之後）加欄位：
```go
	publisher    events.Publisher
```

3c. `NewDispatchService` 簽名末尾加參數並賦值。改簽名為：
```go
func NewDispatchService(
	drivers *repository.DriverRepository,
	rides *repository.RideRepository,
	customers *repository.CustomerRepository,
	redis *redisstore.Store,
	line *lineclient.Client,
	eta *ETAService,
	radiusM, maxCount, offerTimeoutSec, maxAttempts int,
	publisher events.Publisher,
) *DispatchService {
```
在 return 的 struct literal 末尾加：
```go
		publisher:    publisher,
```

3d. 檔案末尾新增 nil-safe helper：
```go
// publish nil-safe 事件發佈（未接 Hub 時靜默略過）
func (s *DispatchService) publish(rec events.Recipient, ev events.Event) {
	if s.publisher == nil {
		return
	}
	s.publisher.Publish(rec, ev)
}
```

3e. 在 `pushOffer` 內（`s.line.PushRideOffer(...)` 之後）加：
```go
	s.publish(events.Recipient{Role: events.RoleDriver, ID: driver.ID}, events.Event{
		Type:    events.TypeRideAssigned,
		RideID:  rideID,
		Payload: map[string]any{"address": address, "eta_sec": etaSec, "dist_m": distM},
	})
```

3f. 在 `AcceptRide` 成功後（`return "接單成功", nil` 之前）加：
```go
	s.publish(events.Recipient{Role: events.RoleCustomer, ID: ride.CustomerID}, events.Event{
		Type:    events.TypeRideAccepted,
		RideID:  rideID,
		Payload: map[string]any{"driver_name": driver.Name, "eta_sec": etaSec},
	})
	s.publish(events.Recipient{Role: events.RoleDriver, ID: driverID}, events.Event{
		Type:   events.TypeRideAccepted,
		RideID: rideID,
	})
```

3g. 在 `giveUpIfUnaccepted` 取消成功後（`log.Warn(...逾時無人接單...)` 之後）加：
```go
	s.publish(events.Recipient{Role: events.RoleCustomer, ID: ride.CustomerID}, events.Event{
		Type:   events.TypeRideCancelled,
		RideID: rideID,
	})
```

3h. 在 `CancelByCustomer` 的 `releaseAndReset` 之後加（`ride` 變數可用）：
```go
	s.publish(events.Recipient{Role: events.RoleCustomer, ID: ride.CustomerID}, events.Event{
		Type:   events.TypeRideCancelled,
		RideID: ride.ID,
	})
```

- [ ] **Step 4：跑單元測試確認通過**

Run: `cd /Users/mac/Documents/line-fleet-dispatch && go test ./internal/service/ -run TestDispatch_publish -v`
Expected: PASS。

- [ ] **Step 5：修正建構子呼叫端（main.go）避免編譯失敗**

在 `cmd/server/main.go` 的 `service.NewDispatchService(...)` 呼叫（約 90-94 行）末尾參數補上 `nil`（Task 8 會換成真 hub）：
```go
	dispatchService := service.NewDispatchService(
		driverRepo, rideRepo, customerRepo, redisStore, lineClient, etaService,
		cfg.DispatchRadiusM, cfg.DispatchMaxDrivers,
		cfg.DispatchOfferTimeoutSec, cfg.DispatchMaxAttempts,
		nil,
	)
```

Run: `cd /Users/mac/Documents/line-fleet-dispatch && go build ./... && go test ./internal/service/ -v`
Expected: 編譯成功；service 既有測試不回歸。

- [ ] **Step 6：Commit**

```bash
git add internal/service/dispatch.go internal/service/dispatch_publish_test.go cmd/server/main.go
git commit -m "feat(ws): DispatchService 注入 Publisher，派單/接單/取消發佈事件

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 7：把 Publisher 注入 TrackingService 並在位置/抵達/上車/完成發佈

**Files:**
- Modify: `internal/service/tracking.go`（struct 欄位、建構子、發佈點）
- Test: `internal/service/tracking_publish_test.go`

**Interfaces:**
- Consumes: `events.Publisher`、`events.Event`、`events.Recipient`；沿用 Task 6 的 `fakePublisher`（同 package，可直接用）
- Produces: `NewTrackingService` 尾端新增參數 `publisher events.Publisher`；`TrackingService` 新增 `publisher` 欄位與 nil-safe `publish` 方法

**設計要點：** 發佈點：
- `ReportDriverLocation` 更新 Redis 後：發 `TypeDriverLocation` 給後台廣播（`Recipient{RoleAdmin, 0}`，payload 帶 driver_id/lat/lng）。
- `checkGeofence` 觸發抵達後：發 `TypeDriverArrived` 給乘客（`ride.CustomerID`）。
- `PickUp`：發 `TypeRidePickedUp` 給乘客。
- `Complete`：發 `TypeRideCompleted` 給乘客與司機。

- [ ] **Step 1：寫失敗測試**

Create `internal/service/tracking_publish_test.go`：
```go
package service

import (
	"testing"

	"line-fleet-dispatch/internal/events"
)

func TestTracking_publish為nil時不panic(t *testing.T) {
	s := &TrackingService{}
	s.publish(events.Recipient{Role: events.RoleAdmin, ID: 0}, events.Event{Type: events.TypeDriverLocation})
}

func TestTracking_publish轉發給Publisher(t *testing.T) {
	fp := &fakePublisher{}
	s := &TrackingService{publisher: fp}
	s.publish(events.Recipient{Role: events.RoleAdmin, ID: 0}, events.Event{Type: events.TypeDriverLocation})
	if fp.count() != 1 {
		t.Fatalf("預期發佈 1 則，得到 %d", fp.count())
	}
	if fp.recv[0].Rec.Role != events.RoleAdmin || fp.recv[0].Ev.Type != events.TypeDriverLocation {
		t.Fatalf("發佈內容錯誤: %+v", fp.recv[0])
	}
}
```

- [ ] **Step 2：跑測試確認失敗**

Run: `cd /Users/mac/Documents/line-fleet-dispatch && go test ./internal/service/ -run TestTracking_publish -v`
Expected: FAIL（無 `publisher` 欄位/`publish` 方法）。

- [ ] **Step 3：實作**

3a. `internal/service/tracking.go` import 區加入：
```go
	"line-fleet-dispatch/internal/events"
```

3b. `TrackingService` struct 加欄位（`dispatch *DispatchService` 之後）：
```go
	publisher events.Publisher
```

3c. `NewTrackingService` 簽名末尾加參數 `publisher events.Publisher`，並在 return struct literal 末尾加 `publisher: publisher,`。新簽名：
```go
func NewTrackingService(
	drivers *repository.DriverRepository,
	rides *repository.RideRepository,
	tracks *repository.TrackRepository,
	redis *redisstore.Store,
	line *lineclient.Client,
	dispatch *DispatchService,
	etaMinIntervalSec, etaDistThresholdM int,
	publisher events.Publisher,
) *TrackingService {
```

3d. 檔案末尾新增：
```go
// publish nil-safe 事件發佈
func (s *TrackingService) publish(rec events.Recipient, ev events.Event) {
	if s.publisher == nil {
		return
	}
	s.publisher.Publish(rec, ev)
}
```

3e. `ReportDriverLocation` 中，`s.redis.UpdateDriverLocation` 成功後、`return s.handleActiveRide(...)` 之前加：
```go
	s.publish(events.Recipient{Role: events.RoleAdmin, ID: 0}, events.Event{
		Type:    events.TypeDriverLocation,
		Payload: map[string]any{"driver_id": driverID, "lat": lat, "lng": lng},
	})
```

3f. `checkGeofence` 中，設 `s.geofenced[ride.ID]=true` 並取得 customerLineID 後（`_ = s.line.PushText(...抵達...)` 之後）加：
```go
	s.publish(events.Recipient{Role: events.RoleCustomer, ID: ride.CustomerID}, events.Event{
		Type:   events.TypeDriverArrived,
		RideID: ride.ID,
	})
```

3g. `PickUp` 中，`s.rides.MarkPickedUp` 成功後加：
```go
	s.publish(events.Recipient{Role: events.RoleCustomer, ID: ride.CustomerID}, events.Event{
		Type:   events.TypeRidePickedUp,
		RideID: rideID,
	})
```

3h. `Complete` 中，`s.rides.CompleteRide` 成功後加：
```go
	s.publish(events.Recipient{Role: events.RoleCustomer, ID: ride.CustomerID}, events.Event{
		Type:    events.TypeRideCompleted,
		RideID:  rideID,
		Payload: map[string]any{"distance_m": distanceM},
	})
	s.publish(events.Recipient{Role: events.RoleDriver, ID: driverID}, events.Event{
		Type:   events.TypeRideCompleted,
		RideID: rideID,
	})
```

- [ ] **Step 4：跑測試確認通過**

Run: `cd /Users/mac/Documents/line-fleet-dispatch && go test ./internal/service/ -run TestTracking_publish -v`
Expected: PASS。

- [ ] **Step 5：修正建構子呼叫端（main.go）**

`cmd/server/main.go` 的 `service.NewTrackingService(...)` 呼叫末尾加 `nil`（Task 8 換真 hub）：
```go
	trackingService := service.NewTrackingService(
		driverRepo, rideRepo, trackRepo, redisStore, lineClient, dispatchService,
		cfg.ETAPushMinIntervalSec, cfg.ETAPushDistThresholdM,
		nil,
	)
```

Run: `cd /Users/mac/Documents/line-fleet-dispatch && go build ./... && go test ./internal/service/ -v`
Expected: 編譯成功、service 測試全綠。

- [ ] **Step 6：Commit**

```bash
git add internal/service/tracking.go internal/service/tracking_publish_test.go cmd/server/main.go
git commit -m "feat(ws): TrackingService 注入 Publisher，位置/抵達/上車/完成發佈事件

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 8：在 main.go 接上 Hub 與 /ws 路由（端到端串起）

**Files:**
- Modify: `cmd/server/main.go`（建立 Hub、注入服務、註冊路由）

**Interfaces:**
- Consumes: `events.NewHub`、`handler.NewWSHandler`、Task 6/7 的建構子新參數
- Produces:（無新對外介面，串接既有元件）

- [ ] **Step 1：建立 Hub 並啟動**

在 `cmd/server/main.go`，`redisStore := ...` 附近（服務建立前）加入：
```go
	hub := events.NewHub()
	go hub.Run()
```
並在 import 區加入 `"line-fleet-dispatch/internal/events"`。

- [ ] **Step 2：把 hub 傳入兩個服務建構子**

將 Task 6/7 暫填的 `nil` 換成 `hub`：
- `NewDispatchService(... cfg.DispatchMaxAttempts, hub)`
- `NewTrackingService(... cfg.ETAPushDistThresholdM, hub)`

- [ ] **Step 3：建立 WS handler 並註冊路由**

在其他 handler 建立處加：
```go
	wsHandler := handler.NewWSHandler(hub, cfg.JWTSecret, cfg.WSWriteWaitSec, cfg.WSPongWaitSec, cfg.WSMaxMessageBytes)
```
在路由區（`r.GET("/healthz", ...)` 附近）加：
```go
	r.GET("/ws", wsHandler.Connect)
```

- [ ] **Step 4：編譯與全量測試**

Run: `cd /Users/mac/Documents/line-fleet-dispatch && go build ./... && go vet ./... && go test ./... -short`
Expected: 編譯、vet、測試皆綠（`-short` 略過需 Docker 的整合測試；如環境有 Docker 可跑 `make test`）。

- [ ] **Step 5：手動端到端煙霧測試（docker）**

Run:
```bash
cd /Users/mac/Documents/line-fleet-dispatch && docker compose up -d --build
```
用 wscat 或瀏覽器 console 測（先取得一個司機 token：走既有 `/api/driver/register`）：
```bash
# 安裝 wscat：npm i -g wscat（若無）
wscat -c "ws://localhost:8080/ws?token=<driver_jwt>"
```
另一終端讓該司機回報位置或觸發派單，觀察 wscat 是否即時收到 `driver.location` / `ride.assigned` 事件。
Expected: WS 連線成功且收到對應 JSON 事件；無 token 連線被拒（401）。

- [ ] **Step 6：Commit**

```bash
git add cmd/server/main.go
git commit -m "feat(ws): main 接上 Hub 與 /ws 路由，事件即時通道端到端串通

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 9：更新 .env.example 與決策紀錄

**Files:**
- Modify: `.env.example`（新增 WS 設定）
- Modify: `docs/decisions.md`（記錄 WS 技術決策）

- [ ] **Step 1：補 .env.example**

在 `.env.example` 末尾加：
```dotenv
# WebSocket 即時通道
WS_WRITE_WAIT_SEC=10
WS_PONG_WAIT_SEC=60
WS_MAX_MESSAGE_BYTES=4096
```

- [ ] **Step 2：補決策紀錄**

在 `docs/decisions.md` 末尾追加一段（若檔案不存在則建立）：
```markdown
## WebSocket 即時通道（M5-WS，2026-07）

- **選 gorilla/websocket**：Go 生態最成熟穩定的 WS 函式庫；作品階段夠用。
- **Hub 單 goroutine 序列化**：register/unregister/publish 全走 channel 進單一迴圈，
  免鎖、無 map 競態（`-race` 驗證）。慢客戶端佇列滿則丟該則事件，不阻塞 Hub。
- **事件發佈與 LINE 推播並存**：在既有 `line.Push*` 呼叫旁「多發一份」結構化事件，
  派單/訂單業務邏輯零改動；未接 Hub 時 Publisher 為 nil，服務照常運作（既有測試不回歸）。
- **通用角色 JWT**：新增 `SubjectClaims{Role,SubjectID}` 同時支援 driver/customer/admin，
  並相容既有司機 `DriverClaims` token（`ParseToken` 退回機制）。
- **邊界**：WS 目前僅單向下推（伺服器 → 前端）；上行（如司機定位）仍走既有 REST。
```

- [ ] **Step 3：Commit**

```bash
git add .env.example docs/decisions.md
git commit -m "docs(ws): 補 WS 環境變數與技術決策紀錄

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Self-Review

**1. Spec coverage（對照設計 §5.2 WebSocket 即時通道）**
- 「開 `/ws`，JWT 驗證」→ Task 5 + Task 8 ✅
- 「hub 管理連線（goroutine+channel）」→ Task 3 ✅
- 「乘客訂閱自己這趟司機位置 + 訂單事件」→ Task 6/7 發佈 `RoleCustomer` 事件 ✅
- 「司機訂閱派給自己的邀請 + 自身訂單事件」→ Task 6 `TypeRideAssigned`/`TypeRideAccepted` ✅
- 「後台訂閱全域車隊位置 + 訂單事件」→ Task 7 `Recipient{RoleAdmin,0}` 廣播 ✅
- 「訂單狀態轉換廣播」→ Task 6/7 涵蓋 assigned/accepted/arrived/picked_up/completed/cancelled ✅
- 「斷線清理、重連可續訂」→ Task 5 readPump defer Unregister（重連＝重新 dial，續訂自動成立）✅
- **注意**：`ride_events` 審計表（設計 §5.2 / roadmap B2）**不在本 plan**，屬獨立資料表工作，留待後續 plan（不阻塞 WS 即時功能）。

**2. Placeholder scan：** 無 TBD/TODO；每個 code step 均為可貼上的完整程式碼。Task 6/7 的 `nil` 是刻意的暫接，Task 8 Step 2 明確替換為 `hub`——非佔位符。

**3. Type consistency：**
- `events.Recipient{Role, ID}`、`events.Event{Type, RideID, Payload}`、`events.Publisher.Publish(rec, ev)` 三處定義（Task 2）與所有使用點（Task 3/5/6/7）一致 ✅
- `auth.GenerateToken(role, id, secret, ttl)` / `auth.ParseToken(token, secret) (role, id, err)`（Task 4）與 WS handler 使用（Task 5）一致 ✅
- `NewDispatchService`/`NewTrackingService` 新增的尾參數 `publisher events.Publisher`（Task 6/7）與 main.go 呼叫（Task 8）一致 ✅
- `fakePublisher` 定義於 Task 6，Task 7 同 package 重用——不重複定義 ✅

**4. 依賴順序：** Task 1→2→3→4→5 可獨立推進；Task 6/7 依賴 2；Task 8 依賴 5/6/7。順序正確。
