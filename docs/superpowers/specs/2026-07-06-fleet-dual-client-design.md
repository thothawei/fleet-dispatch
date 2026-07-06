# LINE 叫車派遣 — 雙端 App + 後台系統 擴張設計（Design Spec）

> 日期：2026-07-06
> 承接 [spec.md](../../spec.md)（M1~M4 已完成）與 [roadmap.md](../../roadmap.md)（Phase A 已完成、B/C/D 待做）。
> **本文件的定位**：把「以 LINE 為前台」的系統升級成「**前後端分離 + 司機/乘客雙端原生 App + 營運後台**」的完整派遣平台。
> **本文件取代 roadmap.md 的以下決策**：Phase D0 的前端框架由 **React Native 改為 Flutter**；並**新增 roadmap 原本沒有的「後台系統」**為一級交付物。
> 語言慣例：程式碼識別字用英文，註解/log/UI 文字用繁體中文。

---

## 1. 一句話目標

在**不改動既有派單/訂單/軌跡業務邏輯**的前提下，把前台從「LINE Bot / LIFF」擴張成三個獨立前端——**司機端 App、乘客端 App、營運後台**——三者皆透過 Go 後端的 REST + WebSocket API 溝通，達成前後端徹底分離。

核心槓桿：spec 第 2 節那句「webhook 之後分不出資料來自真司機或模擬器」現在兌現——把資料來源從 LINE 換成 App 的 API，`service/`（dispatch/ride/tracking/eta）**一行不改**。

---

## 2. 現況盤點（evidence-based，非憑記憶）

**已完成（Phase A，見 roadmap.md）**
- LINE webhook 收單 + 簽章驗證、Redis GEO 派單、SETNX 搶單鎖、OSRM ETA、PostGIS 圍籬、軌跡回放、日報表
- **A1 認證已做**：司機端 **JWT + bcrypt 密碼登入**；`/api/driver/location`、`/api/rides/:id/{accept,pickup,complete}` 受保護，`driver_id` 一律取自 token；跨司機操作回 403
- A2 派單逾時重派、A3 取消流程、A4 ETA 推播節流、A5 軌跡分區自動維護、A6 testcontainers 整合測試
- 分層 `Handler → Service → Repository → DB/Redis`，docker-compose 一鍵起

**尚未做（本設計要處理）**
- Phase B：**WebSocket 即時通道尚未實作** —— App 在地圖上即時看車移動的前置
- **乘客身分認證尚未做**（A1 只做司機；roadmap A1 備註「客戶延到 Phase D」）
- **後台系統完全不存在**（roadmap 只有可選 Grafana / Prometheus，無 admin 前端與 admin API）
- Phase D App 化尚未動工

**本設計不重做已完成項**：司機密碼 JWT、派單邊界（逾時/取消/重派）、軌跡分區皆沿用，不重寫。

---

## 3. 目標架構

```
  乘客端 App(Flutter) ─┐
  司機端 App(Flutter) ─┼──REST(JWT)──►┌──────── Go 後端（單一專案 line-fleet-dispatch，擴充）────────┐
  後台前端(React+TS) ──┤──WebSocket──►│  Handler 層新增：                                             │
  LINE / LIFF（保留）  ─┘   /webhook   │   handler/customer/*   handler/admin/*   handler/ws/*        │
                                       │  （既有 handler/driver、handler/line 不動）                   │
                                       │  middleware/auth_jwt（driver/customer/admin 三種 subject）    │
                                       │  service/hub（WebSocket 連線管理，Go goroutine+channel）      │
                                       │  ── service/dispatch·ride·tracking·eta 完全沿用（零改動）──   │
                                       │  Repository → PostgreSQL(PostGIS) · Redis(GEO/lock) · OSRM    │
                                       └───────────────────────────────────────────────────────────────┘
```

**前後端分離的具體落實**
- 後端**只吐 JSON / WS 事件**，不再為 App 或後台產生 HTML（LINE 的 Flex Message 屬 LINE 前台，保留）。
- 三個前端各自獨立 repo、獨立部署、獨立版本，只依賴 API 契約。

---

## 4. 交付物：三個 Repo

| Repo | 技術 | 職責 | 狀態 |
|---|---|---|---|
| `line-fleet-dispatch` | Go + Gin | 後端 API（含**後台 API**）、WebSocket、既有派遣核心 | 既有，**擴充** |
| `line-fleet-app` | Flutter (Dart) | **司機端 + 乘客端**雙端 App，一 repo 兩 flavor 出兩支 APK/IPA | **新建** |
| `line-fleet-admin` | React + TypeScript (Vite) | 營運後台前端（純 SPA，吃 `/api/admin/*`） | **新建** |

後台的**後端 API 放在 `line-fleet-dispatch`**（依你的要求），不另開後端服務。

---

## 5. 後端擴充細節（`line-fleet-dispatch`）

### 5.1 認證層
- **司機**：沿用 A1 既有密碼 JWT，不動。
- **乘客**：新增登入。**決策見 §12.4**——預設採 **LINE Login（OAuth）** 取得 `line_user_id`（與既有 schema 無縫、免自管密碼），Flutter 用官方 LINE SDK；備案為手機 OTP。
- **後台管理員**：新增 `admins` 表 + email/密碼登入（bcrypt），JWT subject 標記 `role=admin`。
- middleware `auth_jwt` 依 token 的 subject/role 分流三種身分；admin 路由再套 RBAC（先做「全權管理員」單一角色，多角色留待後續）。

### 5.2 WebSocket 即時通道（= roadmap Phase B，本設計納入）
- 開 `/ws`，連線時用 JWT 驗證。
- `service/hub`：Go 原生 goroutine + channel 管理連線與訂閱；
  - 乘客訂閱「自己這趟的司機位置 + 訂單事件」；
  - 司機訂閱「派給自己的邀請 + 自身訂單事件」；
  - 後台訂閱「全域車隊位置 + 全域訂單事件」。
- 訂單狀態轉換（派單/接單/抵達/上車/完成/取消）廣播給相關方；新增 `ride_events` 表留審計（roadmap B2）。
- 斷線清理、重連可續訂。

### 5.3 推播（App 被殺掉時）
- 新增 `device_tokens` 表存 FCM/APNs token。
- 推播層抽象成 `Notifier` 介面，實作 `LINE` / `FCM` / `APNs`，依使用者裝置選路——LINE push 不再是唯一管道，**解除月額度瓶頸**。

### 5.4 後台 API（`/api/admin/*`）
| 端點群 | 內容 |
|---|---|
| `GET /api/admin/fleet` | 即時在線司機清單 + 座標（初始快照，後續走 WS 增量） |
| `GET/POST/PATCH /api/admin/drivers` | 司機列表、審核（啟用/停用）、詳情 |
| `GET /api/admin/rides` | 訂單列表（可篩狀態/日期）、單筆詳情 + 軌跡 GeoJSON |
| `GET /api/admin/reports/daily` | 日報表（趟數/里程/平均接單耗時，沿用既有彙總） |
| `GET/PATCH /api/admin/settings` | 派單參數（半徑、逾時、最大司機數）線上調整 |

### 5.5 既有邏輯沿用（不重寫）
派單重試、取消、節流、軌跡分區、搶單鎖——皆 Phase A 已完成，本設計只在其上加「新的入口與即時通道」。

---

## 6. Mobile App 設計（`line-fleet-app`，Flutter）

### 6.1 為何 Flutter（取代 roadmap D0 的 React Native）
單一 codebase 出雙平台；背景定位套件（`flutter_background_geolocation` 等）成熟；Android 直接出 APK 側載最簡單；效能佳。決策理由見 §12.1。

### 6.2 一 repo 兩端（build flavors + 雙 entrypoint）
```
line-fleet-app/
├── lib/core/          # 共用：dio API client、ws client、models、共用 UI、地圖封裝
├── lib/driver/        # 司機：上線 toggle、背景 GPS 回報、收派單、導航、抵達/上車/完成
├── lib/customer/      # 乘客：地圖選點叫車、即時看司機位置與 ETA、行程狀態、（後續）評分/付款
├── main_driver.dart   # 司機 flavor 進入點
└── main_customer.dart # 乘客 flavor 進入點
```
共用 `core` 讓 API client / models / WS 只寫一次；兩個 flavor 各出一支獨立 APK/IPA。（若日後要徹底隔離，可升級成 melos monorepo；作品階段 flavors 已足夠。）

### 6.3 司機端（先做，鏈路最集中）
- 登入（既有密碼 JWT）→ 上線/待命 → **背景 GPS**每 5–10s 上報（REST `/api/driver/location` + WS）→ 收派單邀請（WS，App 被殺時 FCM）→ 接單（搶單鎖）→ Google Maps deep link 導航 → 按抵達/上車/完成。
- **背景定位是整個升級的最大價值**：解掉 spec 第 7 節「LIFF `watchPosition` 需前景」的死穴。

### 6.4 乘客端
- 登入（§5.1 乘客認證）→ 地圖選上車/目的地 → 叫車 → **WebSocket 即時看司機移動與 ETA** → 完成 →（Phase C 後）評分/付款。

### 6.5 地圖與導航
- 地圖：`flutter_map`(MapLibre/OSM 圖磚，免付費 key) 或 `google_maps_flutter`（需 key）——預設 `flutter_map` 走免費路線，與作品「不依賴付費 API」的敘事一致。
- 導航沿用 spec 的 Google Maps deep link（免 key）。

### 6.6 安裝（不上架）
- **Android**：直接 build APK 側載，無門檻。
- **iOS**：用**免費 Apple 開發者證書**簽名裝到自己 iPhone；README 誠實標明「iOS 憑證 7 天到期需重簽、綁定裝置」的邊界（面試官欣賞你懂邊界）。

---

## 7. 後台前端設計（`line-fleet-admin`，React + TypeScript + Vite）

### 7.1 技術
- React 18 + TypeScript + Vite；UI 用 Ant Design（admin 元件完整）；地圖用 MapLibre GL / Leaflet（免付費 key）。
- 狀態/資料：TanStack Query 打 REST；即時看板用 WebSocket 訂閱。

### 7.2 畫面（依重要性排序，可漸進交付）
1. **登入**（admin JWT）
2. **即時車隊地圖**（WS 訂閱全域司機位置，地圖上即時移動）← 最有故事，先做
3. **訂單管理**（列表 + 篩選 + 單筆詳情 + 軌跡回放 GeoJSON）
4. **司機管理**（列表、審核啟用/停用、詳情）
5. **日報表**（趟數/里程/平均接單耗時）
6. **派單參數設定**（半徑/逾時/最大司機數線上調整）

作品階段先交付 1+2+3，其餘漸進補齊。

---

## 8. 資料模型新增彙總

| 新表 | 用途 | 出現里程碑 |
|---|---|---|
| `customers` 認證欄位 / 或沿用 `users` | 乘客登入身分（LINE Login） | M5 |
| `admins` | 後台管理員帳號 | M5 |
| `ride_events` | 訂單狀態轉換審計 | M5（Phase B2） |
| `device_tokens` | FCM/APNs 推播 token | M5 |

（`fares`/`ratings`/`payments` 屬 roadmap Phase C，非本設計範圍，App/後台預留欄位但不實作。）

---

## 9. 分階段里程碑與相依

> 承接既有 M1~M4（已完成）。本設計為 M5~M8。每個里程碑是**獨立 sub-project，各自有自己的實作 plan**；本 doc 是總藍圖。

### M5 — 平台化後端（App 與後台的共同前置，**必做核心**）
**做什麼**：乘客認證 + admin 認證 + WebSocket hub（Phase B1/B2）+ `ride_events` + `device_tokens` + FCM/APNs Notifier + `/api/admin/*` 端點。
**驗收條件**
- [ ] 乘客可登入取得 JWT；未帶 token 打乘客 API 回 401
- [ ] admin 可登入；admin API 受 `role=admin` 保護
- [ ] WS：乘客連上後，司機每次回報位置，客戶端 1 秒內收到新座標；斷線自動清理
- [ ] 每次訂單狀態轉換，相關方即時收到事件，`ride_events` 完整記錄
- [ ] `/api/admin/fleet`、`/rides`、`/drivers`、`/reports/daily` 回傳正確

### M6 — 司機端 App（Flutter，**必做核心**，解死穴）
**做什麼**：Flutter repo 建置 + core 層 + 司機 flavor 全流程 + 背景定位 + FCM 接收派單。
**驗收條件**
- [ ] 真機（Android）背景定位持續回報，切到 Google Maps 導航仍上報
- [ ] 收到派單推播可接單（搶單鎖生效）
- [ ] 完整跑一趟：上線→接單→導航→抵達→上車→完成
- [ ] iOS 用免費證書可裝到 iPhone 並完成同一趟

### M7 — 乘客端 App（Flutter，加分）
**做什麼**：乘客 flavor 全流程 + 地圖叫車 + WS 即時追蹤。
**驗收條件**
- [ ] 地圖選點叫車 → 訂單建立
- [ ] WS 即時看司機移動與 ETA 更新
- [ ] 完成整趟，LINE 版與 App 版共用同一後端/訂單資料

### M8 — 營運後台（React+TS，加分）
**做什麼**：`line-fleet-admin` repo + 登入 + 即時車隊地圖 + 訂單管理 + 司機管理 +（漸進）報表/設定。
**驗收條件**
- [ ] admin 登入後，即時車隊地圖上看到在線司機隨 WS 移動
- [ ] 訂單列表可篩選、單筆可看軌跡回放（GeoJSON 畫在地圖）
- [ ] 司機審核啟用/停用生效

### 相依圖
```
M5(平台化後端) ──┬──► M6(司機App) ──► M7(乘客App)
                 └──► M8(後台)
M5 是硬性前置：M6/M7/M8 都依賴 JWT + WebSocket + admin API。
最短可 demo 路徑：M5 → M6（司機App，解死穴，最有故事）。
```

---

## 10. 測試策略
- **後端**：沿用 A6 的 testcontainers 模式，補 M5 新端點的整合測試（WS 廣播、admin API 授權邊界、乘客認證）。
- **Flutter**：核心 core 層（API client/models）單元測試；關鍵流程 widget 測試；背景定位靠真機手測（README 記錄步驟）。
- **後台**：關鍵頁 component 測試 + 對 API 契約的整合測試。

---

## 11. 已知限制與 trade-offs（README 誠實寫出）
1. **iOS 免費證書**：7 天到期需重簽、綁定裝置、無法給不特定他人安裝。要穩定散佈需 $99/年 Apple Developer（ad-hoc/TestFlight）。
2. **LINE push 月額度**：M5 導入 FCM/APNs 後，App 用戶推播走 FCM，繞開額度；LINE 前台保留但盡量用 reply token。
3. **OSRM 無即時路況**：沿用 spec 邊界，ETA 為自由流時間。
4. **後台 RBAC 先做單一管理員角色**：多角色/細權限留待後續，避免過度設計。
5. **LIFF 前台退場但保留**：司機定位改由原生 App；LIFF 頁保留當免下載輕入口與 demo，不再是主要定位來源。

---

## 12. 決策紀錄（Decision Log）

**12.1 Mobile 框架：Flutter（取代 roadmap D0 的 React Native）**
理由：背景定位是核心需求，Flutter 的背景定位/地圖套件成熟且單一 codebase 出雙平台；Android 側載最簡單。此為 roadmap D0 的**修訂**。

**12.2 一 repo 兩端（flavors）而非兩個 repo**
理由：司機/乘客共用大量 core（API client、models、WS、地圖）；flavors 讓共用只寫一次又能各出獨立 APK。徹底隔離需求出現時再升 melos monorepo。

**12.3 後台前端：React + TypeScript + Vite**
理由：admin 生態最成熟（Ant Design Pro/Refine），資源最多；純內部後台不需 SSR，Vite SPA 足夠。

**12.4 認證：司機沿用密碼 JWT；乘客用 LINE Login；後台用 email/密碼**
理由：司機端 A1 已實作密碼 JWT，不重做。乘客本來就是 LINE 用戶，LINE Login 延續 `line_user_id`、免自管密碼、與既有 schema 無縫。**此項為可改決策**：若不想綁 LINE 生態，改乘客手機 OTP 或密碼登入亦可——實作 M5 前確認。

**12.5 後台 API 併入 `line-fleet-dispatch`（不另開後端）**
理由：依需求指定；且後台讀寫的正是同一份派遣資料，共用 service/repository 最省、最一致。

---

## 13. 開工順序（給執行者）
1. **M5 先行**：在 `line-fleet-dispatch` 開 feature 分支，做認證擴充 + WebSocket hub + admin API + 推播抽象 → 驗收 → commit。
2. **M6**：新建 `line-fleet-app`（Flutter），core + 司機 flavor + 背景定位 → 真機驗收 → commit。
3. **M7**：同 repo 補乘客 flavor → 驗收 → commit。
4. **M8**：新建 `line-fleet-admin`（React+TS），登入 + 即時地圖 + 訂單/司機管理 → 驗收 → commit。
5. 每個里程碑的技術決策補進各 repo 的 `docs/decisions.md`。

**外部依賴先備**：M5 乘客若走 LINE Login 需 LINE Login channel；App 推播需 FCM（Firebase 專案）與（iOS）APNs 金鑰。這兩項建議 M5 動工前備妥。
