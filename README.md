# LINE 叫車派遣系統（fleet-dispatch）

用 **Go + PostgreSQL(PostGIS) + Redis + LINE Bot** 打造的叫車派遣系統。客戶在 LINE 傳一則「位置訊息」就能叫車，系統即時找出最近的待命司機、推播接單邀請、產生 Google Maps 導航連結，並全程回報司機位置與預估抵達時間（ETA），最後留下行程軌跡與日報表。

> 這是一個學習／作品專案，重點在把即時地理查詢、分散式搶單鎖、路徑 ETA、地理圍籬等後端主題用一條完整業務流程串起來。完整開發規格見 [docs/spec.md](docs/spec.md)，技術決策見 [docs/decisions.md](docs/decisions.md)。

---

## 系統架構

```
  客戶 LINE ──叫車(位置訊息)──┐
                             ├─► LINE Webhook ─► Go 後端 (Gin) ─┐
  司機 LINE / LIFF ─接單/回報─┘                                  │
                                                                 │
         ┌───────────────────────┬──────────────────────┬───────┘
         ▼                       ▼                      ▼
     Redis GEO             PostgreSQL + PostGIS     OSRM 路徑引擎
  (司機即時位置、          (訂單狀態機、軌跡、       (最短路徑 + ETA，
   最近車查詢、搶單鎖)       電子圍籬、日報表)         預設公開 router)
```

分層遵循 `Handler → Service → Repository → DB`，與 Laravel 的 Controller/Service/Repository 對應，方便從 PHP 背景切換閱讀。

---

## 端到端業務流程

一趟完整行程會經過以下狀態機（`internal/constants/ride.go`）：

```
REQUESTED(0) ─► ASSIGNED(1) ─► ACCEPTED(2) ─► PICKED_UP(3) ─► COMPLETED(4)
                                                                （或任一步 CANCELLED(9)）
```

| # | 步驟 | 觸發 | 系統行為 |
|---|------|------|----------|
| 1 | **叫車** | 客戶在 LINE 傳位置 | webhook 驗簽 → 建立/更新客戶 → 建立訂單 `REQUESTED`，pickup 存成 PostGIS `geography` 點 |
| 2 | **派單** | 建單後非同步觸發 | Redis `GEOSEARCH` 找 pickup 半徑內最近 N 台待命司機（濾掉離線）→ 訂單轉 `ASSIGNED` → 推播接單邀請（含 postback 按鈕） |
| 3 | **搶單** | 司機按「接受派單」 | Redis `SETNX` 搶單鎖，**只有第一位搶到的司機成單** → 訂單轉 `ACCEPTED`、寫 `driver_id`、司機轉載客中；其餘司機收到「手慢了」 |
| 4 | **導航** | 接單成功 | 回傳 `https://www.google.com/maps/dir/?...` deep link，司機一點開啟手機 Google Maps 導航到上車點 |
| 5 | **ETA 回報** | 司機位置更新 | OSRM 算「司機→上車點」實際路網時間，推播客戶「距您 X 公尺、約 Y 分鐘抵達」 |
| 6 | **抵達／上車** | 司機進入上車點 100m | PostGIS `ST_DWithin` 電子圍籬自動判定抵達；司機按上車 → 訂單轉 `PICKED_UP`，開始記錄軌跡到 `ride_tracks` |
| 7 | **下車／完成** | 司機按完成 | 訂單轉 `COMPLETED`，PostGIS `ST_Length` 由軌跡算里程；**計費里程取「軌跡 vs OSRM pickup→dropoff 路線」大者**（F3，軌跡稀疏時的退路），依當前費率算好車資／手續費／司機實得，**快照定格**寫進該筆 ride |
| 8 | **報表** | 隨時查詢 | 軌跡回放輸出 GeoJSON；日／月報表聚合各司機趟數、里程、**營業額／手續費／應付總公司**（＝手續費＋月會費）；會費可產生 `membership_invoices` 帳單 |

---

## 快速開始

```bash
cp .env.example .env          # LINE 憑證可留空，留空時 API 測試免簽章
docker compose up --build -d  # 起 app + postgis + redis

curl http://localhost:8080/healthz          # → {"status":"ok"}
sh scripts/smoke_test.sh                     # 跑完整 M1-M4 流程（需乾淨 DB）

make test                                    # 整合測試（testcontainers 起真 Redis/PostGIS，需 Docker）

# 選用：起 20 台模擬司機灌位置（勿與 smoke_test 同時跑，會互相搶單）
docker compose --profile simulator up -d simulator
```

> 重跑 `smoke_test.sh` 前建議先 `docker compose down -v` 清掉舊資料，避免殘留訂單干擾。

---

## API 端點

| 方法 | 路徑 | 說明 |
|------|------|------|
| GET  | `/healthz` | 健康檢查（DB + Redis） |
| POST | `/webhook/line` | LINE webhook（受 `X-Line-Signature` 保護；回應含 `ride_ids` 方便測試） |
| POST | `/api/driver/register` | 註冊司機 |
| POST | `/api/driver/location` | 司機回報位置（進 Redis GEO；行程中同時寫軌跡） |
| POST | `/api/rides/:id/accept` | 接單（搶單鎖） |
| POST | `/api/rides/:id/pickup` | 客戶上車 |
| POST | `/api/rides/:id/complete` | 完成行程 |
| GET  | `/api/rides/:id/track` | 軌跡回放（GeoJSON Feature） |
| POST | `/api/customer/register` `login`、`POST /api/rides` | 乘客 App：註冊/登入、叫車（可帶 `dropoff_lat/lng`） |
| GET  | `/api/driver/earnings?month=YYYY-MM` | 司機收入（趟數/營業額/手續費/實得/會費/應付總公司） |
| GET  | `/liff/` | 司機 LIFF 定位頁 |

**後台 `/api/admin/*`**（帳密登入，角色 viewer/dispatcher/superadmin）：

| 方法 | 路徑 | 說明 |
|------|------|------|
| POST | `/api/admin/login` | 後台登入（種子 admin/admin） |
| GET  | `/api/admin/rides?status=&limit=&offset=&from=&to=&q=` | 訂單列表（伺服器端分頁，回 `total`） |
| GET/POST | `/api/admin/rides/:id`、`/api/admin/rides/:id/cancel` | 訂單詳情（軌跡+事件）／強制取消 |
| GET/PATCH | `/api/admin/drivers`、`/api/admin/drivers/:id/status` | 司機列表／啟停 |
| GET  | `/api/admin/reports/daily?date=`、`/reports/monthly?month=` | 日／月報表（含金額） |
| GET/PUT | `/api/admin/settings/dispatch`、`/api/admin/settings/fees` | 派單參數／費率設定（費率限 superadmin） |
| GET/POST/PATCH | `/api/admin/membership-invoices[...]` | 會費帳單：列表／產生／標記已繳 |
| GET/POST/PATCH | `/api/admin/admins[...]` | 後台帳號管理（superadmin） |

---

## 資料模型（PostgreSQL / PostGIS）

| 表 | 用途 | 地理欄位 |
|----|------|----------|
| `drivers`   | 司機、狀態（離線/待命/載客中） | — |
| `customers` | 客戶（依 LINE userId 唯一） | — |
| `rides`     | 訂單狀態機、上/下車點、里程、ETA、**計費快照**（`fare_amount_cents`／`commission_amount_cents`／`driver_net_amount_cents`） | `pickup_point` / `dropoff_point` `geography(Point,4326)` |
| `ride_tracks` | 行程軌跡（按月分區） | `location geography(Point,4326)` |
| `fleet_settings` | 費率設定單列（起步價/每公里/最低車資/手續費 bps/月會費，金額存分） | — |
| `membership_invoices` | 會費帳單（每司機每月一張，金額快照、`UNIQUE(driver_id, period)` 防重複） | — |
| `admins` | 後台帳號（角色 viewer/dispatcher/superadmin） | — |

**計費設計**：金額全系統存「分」（整數，避免浮點），手續費存 bps（1500=15%）。車資／手續費在**行程完成當下依當前費率算好、快照定格**寫進該筆 ride，日後調費率不影響歷史帳。詳見 [docs/TODO.md](docs/TODO.md)「F. 手續費／會費／營運報表」。

Redis 鍵：`drivers:geo`（GEO 位置集合）、`driver:{id}:loc`（最新位置+時間戳）、`ride:{id}:lock`（搶單鎖，30s TTL）、`ratelimit:{lineUserId}`（叫車限流）。

---

## 驗證狀態

本專案已在本機 Docker 完整跑通並驗證（`go build` / `go vet` / `go test` 皆綠）：

- ✅ **端到端流程**：`smoke_test.sh` 於乾淨 DB 走完「叫車→派單→接單→上車→軌跡→完成→報表」，並斷言接單真的成功、日報表確實含該司機。
- ✅ **搶單鎖**：`SETNX` 確保同一單只有一位司機成單。
- ✅ **LINE 簽章安全**：以真實 HMAC-SHA256 簽章請求驗證——正確簽章 `200`、偽造簽章 `401`、缺簽章 `401`。
- ✅ **PostGIS**：pickup 點、`ST_DWithin` 圍籬、`ST_Length` 里程、`ST_AsGeoJSON` 回放均正確輸出。

---

## 接真 LINE（上手清單）

本機測試不需要 LINE 憑證；要接真帳號跑手機叫車時：

1. 在 [LINE Developers](https://developers.line.biz/) 建 Messaging API channel，取得 **Channel secret** 與 **Channel access token**，填入 `.env`。
2. 建一個 LIFF app（司機定位頁），Endpoint 指向 `https://<你的網域>/liff/`，取得 `LIFF_ID`。
3. 用 ngrok/cloudflared 對外：`ngrok http 8080`，把 webhook URL 設為 `https://xxxx.ngrok.io/webhook/line`。
4. 手機用 LINE 加官方帳號好友，傳一則位置訊息即可叫車。

完整前置清單見 [docs/checklist.md](docs/checklist.md)。

---

## 已知限制

1. **LIFF 背景定位**：網頁 `watchPosition` 需頁面在前景，司機切到導航時位置更新會變慢；正式車隊需原生 App 背景定位。
2. **LINE push 月額度**：設計上盡量用 reply token（互動回覆不計額度）；未設 token 時 push 自動略過。
3. **OSRM**：預設用公開 `router.project-osrm.org` demo server、無即時路況；自架請改 `OSRM_URL` 並參考 [docs/spec.md](docs/spec.md) §4.2。

---

## 技術棧

Go 1.25 · Gin · GORM · golang-migrate · PostgreSQL 16 + PostGIS 3.4 · Redis 7 · LINE Messaging API · OSRM · Docker Compose

## 專案結構

```
cmd/server        服務進入點與路由組裝
cmd/simulator     司機模擬器（灌位置、壓測用）
internal/handler  HTTP handler（webhook、driver、ride、report、health）
internal/service  業務邏輯（dispatch 派單、ride 訂單、tracking 追蹤、eta）
internal/repository  資料存取（GORM + 原生 PostGIS SQL）
internal/redis    Redis GEO / 搶單鎖 / 限流封裝
internal/line     LINE Messaging API 輕量封裝
internal/osrm     OSRM 路徑 client
internal/middleware  LINE 簽章驗證
db/migrations     golang-migrate SQL（啟動時自動套用）
web/liff          司機 LIFF 定位頁
scripts/smoke_test.sh  端到端煙霧測試
```
