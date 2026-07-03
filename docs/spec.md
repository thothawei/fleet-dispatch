# LINE 叫車派遣系統 — 建置規格（Build Spec）

> 這份文件是「額度重置後給執行者（我或排程 agent）直接照做」的施工藍圖。
> 打開這份文件的人不需要看過先前的討論，所有上下文都寫在這裡。
> 語言：程式碼識別字用英文，註解/log/LINE 訊息用繁體中文。

---

## 0. 一句話目標

用 **Go + PostgreSQL(PostGIS) + Redis + OSRM + Docker**，做一個以 **LINE Bot** 為前台的叫車派遣系統：
客戶用 LINE 傳位置叫車 → 系統找最近司機 → 司機用 Google Maps 導航去接 → 客戶即時收到「司機在哪、還有幾分鐘到」→ 抵達上/下車自動偵測 → 產生行程紀錄。

**這是一個學習用作品（portfolio project）**，不是商用產品。設計上以「面試能講清楚、demo 跑得動、技術決策有理由」為最高原則。

---

## 1. 技術棧與版本

| 類別 | 選型 | 版本/備註 |
|---|---|---|
| 語言 | Go | 1.23+ |
| HTTP 框架 | Gin | `github.com/gin-gonic/gin` |
| ORM（一般 CRUD） | GORM | `gorm.io/gorm` + `gorm.io/driver/postgres` |
| 核心查詢（型別安全 SQL） | sqlc | 帳務/地理查詢用 sqlc，一般 CRUD 用 GORM（此取捨本身是面試題材） |
| 主資料庫 | PostgreSQL + PostGIS | PG 16 + PostGIS 3.4（用 `postgis/postgis` 映像） |
| 快取 / 即時位置 | Redis | 7.x，用 GEO 指令 |
| 路徑 / ETA 引擎 | OSRM | `osrm/osrm-backend`，載台灣 OSM 圖資，自架（免費） |
| 前台 | LINE Messaging API + LIFF | 司機端定位用 LIFF 網頁 |
| 導航 | Google Maps URL deep link | 免 API key、免費 |
| Migration | golang-migrate | `github.com/golang-migrate/migrate` |
| 設定 | envconfig 或 viper | 讀 `.env` |
| 日誌 | zerolog | 結構化 log |
| 容器 | Docker + docker-compose | 一鍵起全套 |
| 測試 | go test + testcontainers-go | 用真 PG/Redis 跑整合測試 |

**為什麼是 PostgreSQL 不是 MongoDB**：即時位置那種「NoSQL 感」的需求由 Redis GEO 承擔；訂單需要交易一致性（搶單不能兩個司機同時接到）、軌跡需要地理查詢（PostGIS 完勝），MongoDB 在這題沒有位置。

---

## 2. 系統架構

```
客戶 LINE ──叫車(位置訊息)──┐
                            ├──> LINE Webhook ──> Go 後端 (Gin) ──┐
司機 LINE/LIFF ──接單/回報──┘                                     │
                                                                  │
        ┌──────────────────────────┬──────────────────────────┬──┘
        ▼                          ▼                          ▼
   Redis GEO                  PostgreSQL+PostGIS          OSRM 路徑引擎
 (司機即時位置              (訂單/狀態機/軌跡/            (最短路徑 + ETA)
  最近車查詢)                圍籬/行程報表)
```

資料流關鍵原則：**broker/webhook 以後的整條管線分不出數據來自真司機還是模擬器**——這讓我們能先用模擬器把後端做完，之後接真手機一行後端程式碼都不用改。

---

## 3. 資料庫設計

### 3.1 PostgreSQL（PostGIS）

啟用擴充：
```sql
CREATE EXTENSION IF NOT EXISTS postgis;
```

**drivers（司機）**
| 欄位 | 型別 | 說明 |
|---|---|---|
| id | bigserial PK | |
| line_user_id | text unique | LINE 使用者 ID |
| name | text | |
| phone | text | |
| status | smallint | 0=離線 1=待命 2=載客中 |
| created_at / updated_at | timestamptz | |

**customers（客戶）**
| id | bigserial PK |
| line_user_id | text unique |
| name / phone | text |
| created_at / updated_at | timestamptz |

**rides（訂單／行程）** — 核心狀態機
| 欄位 | 型別 | 說明 |
|---|---|---|
| id | bigserial PK | |
| customer_id | bigint FK | |
| driver_id | bigint FK nullable | 派單後才有 |
| status | smallint | 見下方狀態定義 |
| pickup_point | geography(Point,4326) | 上車點 |
| pickup_address | text | |
| dropoff_point | geography(Point,4326) nullable | 下車點 |
| dropoff_address | text | |
| requested_at | timestamptz | 叫車時間 |
| accepted_at / picked_up_at / completed_at | timestamptz nullable | |
| distance_m | int nullable | 行程距離（PostGIS 算） |
| eta_pickup_sec | int nullable | 派單時算的接客 ETA |

ride 狀態（放 `app/constants` 對應常數）：
```
0 REQUESTED  已叫車、待派單
1 ASSIGNED   已派給司機、待司機接受
2 ACCEPTED   司機已接單、前往接客中
3 PICKED_UP  客戶已上車、行程中
4 COMPLETED  已完成
9 CANCELLED  取消
```

**ride_tracks（行程軌跡）** — 按月分區
| id | bigserial |
| ride_id | bigint FK |
| driver_id | bigint |
| location | geography(Point,4326) |
| recorded_at | timestamptz |

用**宣告式 partitioning** 按 `recorded_at` 月分區（對照製作者在 MySQL 做過的分區維護經驗）。

索引：
```sql
CREATE INDEX idx_rides_status ON rides(status);
CREATE INDEX idx_rides_pickup_gix ON rides USING GIST(pickup_point);
CREATE INDEX idx_tracks_ride ON ride_tracks(ride_id, recorded_at);
CREATE INDEX idx_tracks_gix ON ride_tracks USING GIST(location);
```

### 3.2 Redis 鍵設計

| 鍵 | 型別 | 用途 | TTL |
|---|---|---|---|
| `drivers:geo` | GEO(sorted set) | 所有待命司機的即時位置，`GEOSEARCH` 找最近 | 位置 60s 未更新視為離線（用 score 時間戳過濾） |
| `driver:{id}:loc` | hash | 司機最新位置+時間戳明細 | 120s |
| `ride:{id}:lock` | string(SETNX) | 搶單鎖，防兩個司機同時接同一單 | 30s |
| `ratelimit:{lineUserId}` | string(INCR+EXPIRE) | 叫車 API 限流 sliding window | 60s |

---

## 4. 外部服務設定（動工前先備好，寫進 .env）

### 4.1 LINE
1. 到 [LINE Developers](https://developers.line.biz/) 建一個 Messaging API channel。
2. 拿到 `CHANNEL_SECRET`、`CHANNEL_ACCESS_TOKEN`。
3. 設定 Webhook URL（本機開發用 ngrok/cloudflared 打通：`https://xxx.ngrok.io/webhook/line`）。
4. 再建一個 LIFF app（司機定位頁），拿 `LIFF_ID`。
- 客戶叫車：用 LINE 內建「位置訊息」，webhook 收到 `message.type == "location"`，直接有 lat/lng/address，零額外開發。
- 額度：reply token 回覆免費、主動 push 有月額度——設計上盡量用 reply，demo 綽綽有餘。

### 4.2 OSRM（自架路徑引擎）
```bash
# 下載台灣圖資
wget http://download.geofabrik.de/asia/taiwan-latest.osm.pbf
# 預處理（car profile）
docker run -t -v "$PWD:/data" osrm/osrm-backend osrm-extract -p /opt/car.lua /data/taiwan-latest.osm.pbf
docker run -t -v "$PWD:/data" osrm/osrm-backend osrm-partition /data/taiwan-latest.osrm
docker run -t -v "$PWD:/data" osrm/osrm-backend osrm-customize /data/taiwan-latest.osrm
# 起服務（compose 內做）
# 查 ETA：GET http://osrm:5000/route/v1/driving/{lng1},{lat1};{lng2},{lat2}?overview=false
```
OSRM 回傳 `routes[0].duration`（秒）與 `distance`（公尺），直接當接客 ETA 與行程距離。

### 4.3 Google Maps 導航（deep link，免 key）
推給司機的訊息帶：
```
https://www.google.com/maps/dir/?api=1&destination={lat},{lng}&travelmode=driving
```
司機點了直接開手機 Google Maps 導航。

---

## 5. 專案目錄結構

```
line-fleet-dispatch/
├── cmd/
│   ├── server/main.go          # 主服務進入點
│   └── simulator/main.go       # 司機模擬器（假造 N 台車發位置）
├── internal/
│   ├── handler/                # ≈ Laravel Controller（Gin handler）
│   │   ├── line_webhook.go     # 收 LINE 事件
│   │   ├── liff.go             # 司機定位回報 API
│   │   └── ride.go
│   ├── service/                # ≈ Laravel Service（業務邏輯）
│   │   ├── dispatch.go         # 派單：找最近司機 + 搶單鎖
│   │   ├── ride.go             # 訂單狀態機
│   │   ├── tracking.go         # 位置回報、圍籬偵測
│   │   └── eta.go              # 呼叫 OSRM 算 ETA
│   ├── repository/             # GORM 一般查詢
│   ├── db/                     # sqlc 生成的地理/核心查詢
│   ├── redis/                  # GEO、鎖、限流封裝
│   ├── line/                   # LINE SDK 封裝（reply/push/flex message）
│   ├── osrm/                   # OSRM client
│   ├── middleware/             # 簽章驗證、限流、logging
│   ├── constants/              # ride 狀態等常數
│   └── model/
├── db/
│   ├── migrations/             # golang-migrate 檔
│   └── queries/                # sqlc 的 .sql 原始檔
├── web/liff/                   # 司機定位 LIFF 靜態頁（watchPosition）
├── deploy/
│   └── grafana/                # （可選）行程/派單看板
├── docker-compose.yml
├── Dockerfile                  # 多階段 build，產出 <20MB 映像
├── .env.example
├── Makefile
└── docs/
    ├── spec.md                 # 本檔
    └── decisions.md            # 技術決策紀錄（動工時邊做邊補）
```

Laravel 概念對照（給製作者降低學習阻力）：

| 你會的（Laravel） | 這裡對應 |
|---|---|
| Artisan migrate | golang-migrate |
| Eloquent | GORM |
| DB::raw 複雜查詢 | sqlc |
| Horizon queue | goroutine worker |
| Controller/Service/Repository | 同名分層，保留 |
| .env + config/ | envconfig + `.env` |
| PHPUnit | go test + testcontainers |

---

## 6. 里程碑與驗收條件

> 每個里程碑做完要能 `docker-compose up` 跑起來、且驗收條件全過才算完成。

### M1 — LINE Bot 收單基礎（約 1 週）
**做什麼**
- docker-compose 起 Go app + PostGIS + Redis
- golang-migrate 建 drivers/customers/rides 表
- LINE webhook：收客戶「位置訊息」→ 建立 customer（若新）→ 建立 ride(status=REQUESTED) 存 pickup_point → reply「已收到您的叫車，正在為您派車」
- 健康檢查端點 `/healthz`

**驗收條件**
- [ ] `docker-compose up` 一鍵起全部服務、無錯誤
- [ ] 用 LINE 傳位置給 bot，DB `rides` 表出現一筆 REQUESTED、pickup_point 座標正確
- [ ] webhook 有驗簽（X-Line-Signature），偽造請求被擋
- [ ] `/healthz` 回 200

### M2 — 派單與搶單（約 2 週）— 本專案核心
**做什麼**
- 司機端 LIFF 頁：`watchPosition()` 每 5–10s POST `/api/driver/location` → 寫 Redis GEO
- 司機模擬器 `cmd/simulator`：可配置 N 台車，在台北市範圍內隨機移動、發位置
- 派單 service：ride 建立後，`GEOSEARCH` 找 pickup 半徑 3km 內最近 5 台待命車 → 依序推播接單邀請（Flex Message + 接受按鈕）
- 搶單鎖：司機按接受時 `SETNX ride:{id}:lock`，搶到的才成單（status=ACCEPTED、寫 driver_id），沒搶到回「手慢了」
- 派單成功推給客戶：司機資訊 + 接客 ETA（M3 接 OSRM 前先用直線距離估）

**驗收條件**
- [ ] 模擬器起 20 台車，叫一次車能正確找到最近的車並派單
- [ ] **併發測試**：兩個司機同時接同一單，只有一個成功、另一個收到「已被接走」（搶單鎖有效）
- [ ] 司機狀態正確流轉：待命→載客中；完成後回待命
- [ ] Redis 中超過 60s 沒回報的司機不會被派到

### M3 — ETA 與導航（約 1 週）
**做什麼**
- 起 OSRM 服務（compose 內，載台灣圖資）
- eta service：呼叫 OSRM 算「司機→上車點」的實際路徑時間，取代 M2 的直線估算
- 派單訊息帶 Google Maps deep link（司機一點開導航）
- 客戶端每逢司機位置更新，推播/更新「司機距您 X 公里，約 Y 分鐘抵達」

**驗收條件**
- [ ] OSRM 回傳合理的台灣路網時間（非直線距離）
- [ ] 司機收到的訊息點擊後正確開啟 Google Maps 導航到上車點
- [ ] 客戶能收到至少 3 次 ETA 更新（接單、接近中、即將抵達）

### M4 — 圍籬、軌跡、報表（約 2 週）
**做什麼**
- PostGIS 電子圍籬：司機進入上車點 100m（`ST_DWithin`）自動觸發「司機已抵達」通知
- 客戶上車：司機按「客戶已上車」→ status=PICKED_UP、開始記軌跡到 ride_tracks（按月分區）
- 下車：客戶傳下車位置或司機按「完成」→ status=COMPLETED、用 PostGIS 算總距離 distance_m
- 行程回放 API：回傳某趟 ride 的完整軌跡 GeoJSON
- 行程報表：每日/每司機的趟數、里程、平均接客時間（PG window function/CTE 彙總）
- （可選）Grafana 看板：即時在線司機數、當日派單量、平均 ETA

**驗收條件**
- [ ] 司機開到上車點 100m 內，客戶自動收到「司機已抵達」（圍籬觸發）
- [ ] 完整跑一趟：叫車→派單→抵達→上車→下車→完成，狀態機無誤
- [ ] ride_tracks 正確落在對應月分區
- [ ] 行程回放 API 回傳可畫在地圖上的 GeoJSON
- [ ] 日報表數字正確（手動對一筆）

---

## 7. 已知限制與 trade-offs（README 要誠實寫出）

1. **LIFF 背景定位限制**：網頁 `watchPosition` 需頁面在前景（螢幕亮）。司機切到 Google Maps 導航時，LIFF 回報會暫停。
   - 真實車隊用**原生 App 背景定位**解決。
   - 作品階段的務實處理：接受「導航中位置更新變慢」，或用司機模擬器展示完整管線，README 寫明此邊界與原生 App 的正解。面試官會欣賞你懂邊界。
2. **LINE 主動 push 有月額度**：設計上盡量用 reply token（互動觸發的回覆不計額度）。Demo 夠用，商用要另算成本。
3. **OSRM 用固定歷史圖資、無即時路況**：ETA 是自由流時間、不含塞車。要即時路況得換 Google Distance Matrix API（付費）。作品用 OSRM 足夠且「自架路徑引擎」更有故事。
4. **搶單鎖用 Redis SETNX**：單 Redis 實例夠用；若要多實例高可用得上 Redlock，但作品不需要過度設計。

---

## 8. .env.example（動工時建立 `.env` 填真值）

```dotenv
# App
APP_PORT=8080
APP_ENV=local

# PostgreSQL
DB_HOST=postgis
DB_PORT=5432
DB_NAME=fleet
DB_USER=fleet
DB_PASSWORD=change_me

# Redis
REDIS_ADDR=redis:6379

# LINE
LINE_CHANNEL_SECRET=
LINE_CHANNEL_ACCESS_TOKEN=
LIFF_ID=

# OSRM
OSRM_URL=http://osrm:5000

# Dispatch 參數
DISPATCH_RADIUS_M=3000
DISPATCH_MAX_DRIVERS=5
DRIVER_OFFLINE_SEC=60
```

---

## 9. docker-compose 服務清單（動工時實作）

- `app` — Go 服務（Dockerfile 多階段 build）
- `postgis` — `postgis/postgis:16-3.4`，掛 volume、init 跑 CREATE EXTENSION
- `redis` — `redis:7-alpine`
- `osrm` — `osrm/osrm-backend`，掛預處理好的台灣圖資、跑 `osrm-routed`
- （可選）`grafana` — 接 PostGIS 做看板
- （開發用）`simulator` — 可獨立起，灌模擬司機位置

---

## 10. 開工順序（給執行者的第一步指令）

額度重置後，執行者請照此順序動工，每完成一個里程碑就 `git commit`：

1. `git init`、建 Go module `go mod init line-fleet-dispatch`
2. 寫 `docker-compose.yml`（先只含 app+postgis+redis）+ 多階段 `Dockerfile`
3. 建 golang-migrate migration（M1 的三張表 + PostGIS 擴充）
4. 實作 M1（LINE webhook 收位置建單）→ 驗收 → commit
5. 依序 M2 → M3 → M4，每個里程碑驗收條件全過才進下一個
6. 過程中把踩到的技術決策寫進 `docs/decisions.md`

**注意**：LINE channel 與 OSRM 圖資這兩項外部依賴，建議動工前先由使用者備好（見第 4 節），否則 M1/M3 會卡在等憑證/圖資。

---

## 11. 完成後這個作品能在面試講的故事

- 「用 Redis GEO 做即時最近司機查詢，搶單用 SETNX 分散式鎖防重複派單」
- 「訂單狀態機 + PostGIS 電子圍籬自動偵測抵達」
- 「自架 OSRM 路徑引擎算 ETA，不依賴付費 API」
- 「軌跡表用宣告式分區、報表用 window function 彙總」
- 「先寫模擬器解耦硬體依賴，架構隔著 MQTT/LINE，真裝置接上零改動」

這些每一句都是資深後端的語言，且全部有可跑的 code 佐證。
