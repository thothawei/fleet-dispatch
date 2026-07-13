# 派車系統 — 工作狀態與待辦清單

> 最後更新：2026-07-12（手續費／會費／報表子系統 + F3 里程退路 + 會費帳單 UI + 訂單伺服器端分頁 + antd deprecation 清理收尾）。此檔記錄「雙端 App + 後台」擴張的整體進度，跨三個 repo。
> 總體設計見 [dual-client design](superpowers/specs/2026-07-06-fleet-dual-client-design.md)；
> **可執行的缺口清單**見 [2026-07-07-gap-analysis-plan.md](2026-07-07-gap-analysis-plan.md)（§0.1 有 2026-07-08 複查更新）與 [backend-api-gaps.md](backend-api-gaps.md)。

## Repo 與遠端

| Repo | 路徑 | 遠端 | 狀態 |
|---|---|---|---|
| 後端 line-fleet-dispatch | `~/Documents/line-fleet-dispatch` | `github.com/thothawei/fleet-dispatch` | 已 push |
| 後台前端 line-fleet-admin | `~/Documents/line-fleet-admin` | `github.com/thothawei/fleet-frontEnd` | 已 push |
| 雙端 App line-fleet-app | `~/Documents/line-fleet-app` | `github.com/thothawei/fleet-app` | 已 push |

git 慣例（2026-07-10 起）：三 repo 的 `main` **受 branch protection 保護、不可直推**，一律開分支 → `gh pr create` → 等 CI 綠 → `gh pr merge --squash --delete-branch`。詳見下方「Git 工作流」。（push 用 repo 內 `core.sshCommand` 指定的 thothawei 金鑰。）

## 已完成

### 後端（line-fleet-dispatch）
- **Phase A**：LINE 派單核心、JWT、派單重試/取消/節流、軌跡分區、testcontainers 測試。
- **M5-WS**：WebSocket hub（`/ws`，events.Hub 單 goroutine，Publisher 注入 dispatch/tracking）。
- **M5-ADMIN**：admin 認證 + 後台 API（唯讀 + **P2 寫入** 2026-07-08）。
- **M5-CUSTOMER-AUTH**：乘客 line_user_id + 密碼 JWT，解鎖乘客 WS 訂閱。
- **後台帳號登入**：email→username（migration 000007）；種子帳號 **admin/admin**。
- **P0 乘客 App 端點（2026-07-07）**：`POST /api/rides` 下單、`GET /api/customer/rides/active`、`GET /api/customer/rides/:id`、`POST /api/rides/:id/cancel-by-customer`。
- **P1 司機 App 端點（2026-07-08）**：`/api/driver/me`、online/offline、`/api/driver/rides/active`、decline。
- **安全 + 資料層（2026-07-08）**：`/track` 補 MultiAuth；公開 `/api/reports/daily` 下架；`GeoPoint.Scan` 修復；`Ride`/`GeoPoint` JSON tag。
- **dropoff 鏈路（2026-07-08；2026-07-10 補齊座標）**：App 下單寫入 `dropoff_address/lat/lng`；`rides/active` 回傳 dropoff；
  `ride.assigned`／`ride.accepted` WS 事件與 **pickup 回應**皆帶 `dropoff_address` + `dropoff_lat/lng`，司機端改以座標導航（地址僅供顯示與退路）。
- **P2 後台寫入 API（2026-07-08）**：司機啟停、派單參數 GET/PUT、後台強制取消。
- **smoke_test.sh 同步（2026-07-08）**：track 帶司機 JWT、日報改 admin JWT；對齊 M5 安全改動。

### 司機端 App（line-fleet-app，Flutter）— M6 主鏈路完成
- 登入 → 上線 → **前景服務 GPS 持續回報**（A1，2026-07-08）→ WS 收派單 → 接單 → Google Maps 導航 → 上車 → **導航去目的地** → 完成／放棄，全鏈路已實作（`lib/core/` + `lib/driver/`）。
- 單元測試：行程狀態機 + WS 事件解析 + dropoff 鏈路（`test/widget_test.dart`，21 項全過）。
- 環境：Flutter 3.44.4 + Android SDK 36 + **JDK 17**（JDK 26 會壞 build）；模擬器 AVD `m6_pixel`。

### 乘客端 App（line-fleet-app，Flutter）— M7 最小可用版已落地
- `main_customer.dart` 已接 `CustomerApp`（非 placeholder）。
- **B1** 登入/註冊；**B2** 叫車帶目的地（文字 + 地圖選點接線，API key 待填）；**B3** WS 即時顯示司機距離/ETA；**B4** 行程狀態流 + App 端取消 + 分階段畫面（含 `driver.arrived`）。
- 端到端：乘客下單 dropoff → 司機上車後導航去目的地，已通（LINE 叫車仍無目的地，屬設計取捨）。

### 後台前端（line-fleet-admin，React+TS+Vite）
- 頁面：登入、營運總覽 Dashboard、即時車隊地圖（MapLibre + WS，marker 連司機詳情）、訂單列表（**伺服器端分頁/日期/關鍵字**）＋**訂單詳情+軌跡回放**、司機列表（搜尋/篩選）＋司機詳情、日報表、**月營運報表**、派單參數、**費率設定**、使用者管理。
- Ant Design(zh_TW，v6 deprecation 全清、message/Modal 走 `App.useApp()`) + TanStack Query + axios(JWT `exp` 主動登出) + react-router（`RequireAuth`/`RequireRole`）；全域 Error Boundary、統一 `apiError`、Skeleton 載入；`npm start` 一鍵開前後端。
- Vitest 22 檔 92 tests、CI（lint→test→build）綠；路由 lazy import 拆包（maplibre 單 chunk >500KB 屬警告非阻塞）。

### 手續費／會費／營運報表（2026-07-11~12，跨三端，皆已合併進 main）
> 需求：後台設手續費%、報表顯示司機營業額與應付總公司、車隊會費。設計定案與逐項見各 repo `docs/TODO.md`。
- **後端 F1–F8（line-fleet-dispatch）**：`fleet_settings` 費率表（migration 000011）、rides 加計費快照欄位（000012）、完成時依當前費率快照定格計費（金額存分、手續費存 bps）、費率設定 API（superadmin）、日/月報表加金額、司機收入 API、會費落帳 `membership_invoices`（000013，`UNIQUE(driver_id,period)` 防重複）。
- **F3 OSRM 里程退路（2026-07-12）**：完成計費里程＝`max(GPS 軌跡里程, OSRM pickup→dropoff 路線里程)`，軌跡 0/稀疏時用路線里程補回。docker E2E：track_m=0→route_m=6263→fare NT$210.26（非 min_fare 8500）。
- **F9 大資料量預防**：F9-1 sargable 範圍查詢+複合索引、F9-2 加總防溢位、F9-3 預聚合彙總表 `daily_driver_earnings`（月報表/司機收入改讀彙總、完成時冪等重算 rollup）、F9-4 查詢跨度上限（rides `from~to` ≤31 天）+`statement_timeout` 逾時保護、F9-5 逐筆列表加硬上限 `MaxListRows=5000`（禁無上限回傳）、F9-6 會費防重複入帳已做；**只剩 F9-7 rides 月分割**（及 drivers/membership 逼近上限時的真分頁），留「量體上升後」。
- **後台 G1–G3（line-fleet-admin）**：費率設定頁、日報表金額欄、月營運報表頁（應付總公司+CSV）。
- **會費帳單 UI（line-fleet-admin，2026-07-12）**：`MembershipInvoicesPage`（`/membership-invoices`）——月選+狀態篩選、未繳/已繳計數，superadmin 可「產生本月帳單」（冪等）與「標記已繳/改未繳」、viewer 只讀。**F8 至此後端＋後台 UI 全鏈路打通**。
- **App E1–E2（line-fleet-app）**：司機收入頁（月切換、應付總公司）、乘客完成卡顯示車資。
- **三端對帳**：`driver/earnings`（app 來源）==`reports/monthly`（admin 來源）逐欄相同、admin UI 逐欄一致、快照制驗過——**app E1 ↔ admin G3 ↔ 後端 F6/F7 三端金額全對齊**。

## 待補強（2026-07-08 盤點，依優先序）

> 逐項驗收條件見 [gap-analysis-plan](2026-07-07-gap-analysis-plan.md)；App/後台各自的清單在
> `line-fleet-app/docs/TODO.md`、`line-fleet-admin/docs/TODO.md`。

1. **M7 乘客端收尾**（主鏈路已通）：~~B6 計畫~~ ✅ 2026-07-08（[2026-07-08-m7-customer-app.md](superpowers/plans/2026-07-08-m7-customer-app.md)）。仍待：Maps API key、地圖追蹤（Slice 5）、B5 評分/付款（Phase C）、端到端驗收截圖。
2. ~~**A1 司機背景定位**~~：✅ 已實作（2026-07-08，line-fleet-app）。`getPositionStream` + Android 前景服務通知；**待真機驗收**鎖屏 10 分鐘後座標仍更新。
3. ~~**A2/D1 FCM 推播（後端）**~~：✅ 2026-07-08 D1 契約落地——`device_tokens` migration、`Notifier` stub（LogPusher）、
   `POST/DELETE /api/{driver,customer}/device-token`、派單時並發 App 推播。**真 FCM/APNs 與 App A2 註冊 token 仍待** Firebase／真裝置。
4. ~~**P1 小尾巴**~~：✅ 已完成（2026-07-08 事件層；**2026-07-10 修復並補完**）。`ride.assigned`／`ride.accepted` 事件與 pickup 回應皆帶
   `dropoff_address/lat/lng`；司機接單前可預覽目的地，上車後以座標導航。
   ⚠️ 2026-07-10 修：commit `21e031d` 宣稱「RideService/RideRepository 新增 dropoff 參數」，但實際只提交了 `line_webhook.go` 與一個
   從未編譯過的測試檔，導致 **main 連續三個 commit 編譯失敗**（`service.RideRequest` 沒有 `DropoffLat/Lng/Address` 欄位）。
   已移除該測試檔，並拿掉 webhook 中硬編的「台北 101」預設目的地——**LINE 流程只有位置訊息（上車點），沒有目的地輸入來源**，
   硬塞預設值會讓每張 LINE 訂單的司機上車後導航到 101。LINE 叫車無目的地維持設計取捨。
5. ~~**D4 `ride_events` 審計表**~~：✅ 2026-07-08。migration `000009`、狀態轉換寫入（叫車/派單/接單/抵達/上車/完成/取消/重派）、`GET /api/admin/rides/:id` 回傳 `events`。
6. ~~**後台寫入（後端 P2 + 前端 C2/C3）**~~：✅ 後端 API 2026-07-08；前端（line-fleet-admin）司機啟停、派單參數、強制取消亦已完成。**D4 前端**：✅ 訂單詳情「狀態時間軸」顯示 `events`（2026-07-08）。
7. **品質**：admin 測試／C5 視覺驗證已在 admin repo 完成；~~A4 M6 計畫勾選~~ ✅。本機 Go 整合測試需完整 Xcode（CGO stdlib.h）或 Docker PostGIS。
8. **DevOps**：~~E2 App CI~~ ✅；~~E2 admin CI~~ ✅；~~E2 後端 CI~~ ✅ 2026-07-08（`.github/workflows/go-ci.yml`：build + 單元測）。仍待：E3 生產部署、E4 監控。
9. ~~**D6 RBAC 多角色**~~：✅ 2026-07-09（spec [2026-07-09-d6-rbac-design.md](superpowers/specs/2026-07-09-d6-rbac-design.md)／計畫 [2026-07-09-d6-rbac.md](superpowers/plans/2026-07-09-d6-rbac.md)）。
   三層角色 viewer/dispatcher/superadmin（migration `000010` 加 `role`/`is_active` + CHECK）；`AdminAuth` 改查 DB（停用即時生效）+ `RequireAdminRole` 分級；
   帳號管理 API `/api/admin/admins`（superadmin，防鎖死 **FOR UPDATE** 交易）+ `/api/admin/me`；前端（line-fleet-admin）bootstrap 補 role、路由守衛、使用者管理頁、viewer 寫入降級。
   端到端 curl 驗證分級/停用即時失效/防自我鎖死全通過。
10. **延後**：A5 iOS build（需完整 Xcode + CocoaPods）；D7 Phase C 中**計費已於 2026-07-11~12 完成**（見上「手續費／會費／營運報表」），剩評分/金流/metrics。Maps API key／真 FCM 屬外部依賴。

## 下次任務

> 2026-07-11~12 收尾：手續費／會費／報表三端（F1–F8、G1–G3、E1–E2）＋F3 里程退路＋會費帳單 UI＋
> 訂單伺服器端分頁＋antd v6 deprecation 全清，皆已合併進 main。以下多屬外部依賴或「量體上升後才需」：

1. ~~座標導航 E2E~~ ✅ 2026-07-11：App 模擬器驗證司機端「導航去目的地」開出的是座標而非地址（`dumpsys` 攔 intent `query=lat,lng`）。
2. ~~後端訂單查詢 API + 前端伺服器端分頁~~ ✅ 2026-07-11：`GET /api/admin/rides` 的 `offset`/`from`/`to`/`q`/`total`，admin 已改伺服器端分頁。
3. **E3 生產部署 / E4 監控**（尚未開始，DevOps 剩下的兩項）。
4. **F9-7（量體達千萬級才需）**：rides 依 `completed_at` 做 declarative partitioning。另 drivers/membership 逼近 `MaxListRows=5000` 時改真 offset/keyset 分頁。（F9-3 預聚合彙總表、F9-4 查詢跨度上限+`statement_timeout`、F9-5 逐筆列表硬上限已於 2026-07-13 完成。）
5. **外部依賴**：Maps API key（乘客端地圖版 B2/B3 實測）、真 FCM/APNs + Firebase 專案（A2 真裝置推播）、A5 iOS build（Xcode+CocoaPods）。
6. ~~會費帳單 UI~~ ✅ 2026-07-12（admin#8）：`MembershipInvoicesPage` 列表／產生（冪等）／標記已繳，viewer 只讀；docker E2E 驗過。

## Git 工作流（2026-07-10 起）

三個 repo 的 `main` 都開了 GitHub branch protection，**不能再直接 push**：

| repo | required status check |
|---|---|
| `thothawei/fleet-dispatch` | `build-and-unit-test` |
| `thothawei/fleet-frontEnd`（line-fleet-admin） | `check` |
| `thothawei/fleet-app` | `analyze-and-test` |

共通設定：`enforce_admins: true`（owner 也擋，實測直推會 `protected branch hook declined`）、
需經 PR 但 `required_approving_review_count: 0`（可自己 merge，不會死鎖）、
`strict: true`（PR 分支需與 main 同步）、禁 force push、禁刪分支。

流程：開 branch → `gh pr create` → 等 CI 綠 → `gh pr merge --squash --delete-branch`。
起因見 `decisions.md`：go-ci 兩次轉紅仍被無視照推，main 上有三個 commit 編譯失敗。

## 環境備忘
- Flutter/Android 環境變數在 `~/.zshrc`（JAVA_HOME→openjdk@17、ANDROID_HOME、PATH）。Bash 工具跨回合 cwd 會重設，跑 flutter/adb 前自行 export。
- Node v23 + npm 10（後台前端用）。
- 後端 docker：`cd line-fleet-dispatch && docker compose up --build -d`；或在 line-fleet-admin 用 `npm start`（一起開前後端）。
- 煙霧測試：`bash scripts/smoke_test.sh`（前提 docker compose 已啟動）。
- 帳號：後台 **admin / admin**。
- 踩雷紀錄見 `line-fleet-dispatch/docs/decisions.md`（含 JDK 版本、家目錄 git repo 陷阱、.env 覆蓋種子密碼等）。
