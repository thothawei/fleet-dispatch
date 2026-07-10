# 派車系統 — 工作狀態與待辦清單

> 最後更新：2026-07-08（debug 複查 + smoke_test 同步）。此檔記錄「雙端 App + 後台」擴張的整體進度，跨三個 repo。
> 總體設計見 [dual-client design](superpowers/specs/2026-07-06-fleet-dual-client-design.md)；
> **可執行的缺口清單**見 [2026-07-07-gap-analysis-plan.md](2026-07-07-gap-analysis-plan.md)（§0.1 有 2026-07-08 複查更新）與 [backend-api-gaps.md](backend-api-gaps.md)。

## Repo 與遠端

| Repo | 路徑 | 遠端 | 狀態 |
|---|---|---|---|
| 後端 line-fleet-dispatch | `~/Documents/line-fleet-dispatch` | `github.com/thothawei/fleet-dispatch` | 已 push |
| 後台前端 line-fleet-admin | `~/Documents/line-fleet-admin` | `github.com/thothawei/fleet-frontEnd` | 已 push |
| 雙端 App line-fleet-app | `~/Documents/line-fleet-app` | `github.com/thothawei/fleet-app` | 已 push |

git 慣例：fleet 三 repo 直接在 `main` 開發、commit 後直接 push（push 用 repo 內 `core.sshCommand` 指定的 thothawei 金鑰）。

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
- 頁面：登入、即時車隊地圖（MapLibre + WS）、訂單列表、**訂單詳情 + 軌跡回放**（commit 1702fec）、司機列表、日報表。
- Ant Design(zh_TW) + TanStack Query + axios(JWT) + react-router 受保護路由；`npm start` 一鍵開前後端。
- 路由 lazy import 已拆包（maplibre 單 chunk 仍 >500KB，屬警告非阻塞）。

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
10. **延後**：A5 iOS build（需完整 Xcode + CocoaPods）；D7 Phase C 計費/評分/金流/metrics。Maps API key／真 FCM 屬外部依賴。

## 下次任務

1. **開 main 分支保護**：go-ci **有**在 `21e031d` 與後續 docs commit 兩次轉紅（run 29082655288／29082686314），
   但被無視、照樣推。CI 抓得到不代表擋得住——需要 GitHub branch protection 要求 go-ci 綠燈才能 push/merge。
2. **座標導航 E2E**：`docker compose up` 後跑 `scripts/smoke_test.sh`，確認 pickup 回應含 `dropoff_lat/lng`；
   再配 App 模擬器驗司機端「導航去目的地」開出的是座標而非地址。
3. **E3 生產部署 / E4 監控**（尚未開始，DevOps 剩下的兩項）。
4. ~~**後台前端剩餘小項**（line-fleet-admin）~~：✅ 2026-07-10。Token 過期處理（JWT `exp`）、
   日期篩選／關鍵字搜尋、匯出 CSV、全域 Error Boundary、README 版本校正皆完成；
   lint + tsc + 76 tests + build 全綠，另跑 Playwright 對真後端做 15 項真瀏覽器驗收。
5. **後端補訂單查詢 API**（新缺口，擋住後台真分頁）：`RideRepository.ListRecent(status, limit)`
   （`internal/repository/repository.go:505`）只吃 status + limit，沒有 offset／日期區間／關鍵字。
   後台目前只能在「已載入的最近 100 筆」內做 client-side 過濾（頁面已明文標示這個限制）。
   要做真分頁需替 `GET /api/admin/rides` 加上 `offset`、`from`/`to`、`q` 參數。

## 環境備忘
- Flutter/Android 環境變數在 `~/.zshrc`（JAVA_HOME→openjdk@17、ANDROID_HOME、PATH）。Bash 工具跨回合 cwd 會重設，跑 flutter/adb 前自行 export。
- Node v23 + npm 10（後台前端用）。
- 後端 docker：`cd line-fleet-dispatch && docker compose up --build -d`；或在 line-fleet-admin 用 `npm start`（一起開前後端）。
- 煙霧測試：`bash scripts/smoke_test.sh`（前提 docker compose 已啟動）。
- 帳號：後台 **admin / admin**。
- 踩雷紀錄見 `line-fleet-dispatch/docs/decisions.md`（含 JDK 版本、家目錄 git repo 陷阱、.env 覆蓋種子密碼等）。
