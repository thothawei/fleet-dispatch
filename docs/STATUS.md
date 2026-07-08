# 派車系統 — 工作狀態與待辦清單

> 最後更新：2026-07-08（複查盤點，以程式碼實測為準）。此檔記錄「雙端 App + 後台」擴張的整體進度，跨三個 repo。
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
- **M5-ADMIN**：admin 認證 + 唯讀後台 API（`/api/admin/` login/fleet/drivers/rides/rides/:id/reports/daily）。
- **M5-CUSTOMER-AUTH**：乘客 line_user_id + 密碼 JWT，解鎖乘客 WS 訂閱。
- **後台帳號登入**：email→username（migration 000007）；種子帳號 **admin/admin**。
- **P0 乘客 App 端點全數完成（2026-07-07）**：`POST /api/rides` 下單、`GET /api/customer/rides/active`、`GET /api/customer/rides/:id`、`POST /api/rides/:id/cancel-by-customer` — 乘客端 App（M7）後端依賴已全部就緒。

### 司機端 App（line-fleet-app，Flutter）— M6 主鏈路完成
- 登入 → 上線 → 前景 GPS 回報 → WS 收派單 → 接單 → Google Maps 導航 → 上車 → 完成／放棄，全鏈路已實作（`lib/core/` 共用層 + `lib/driver/`）。
- 單元測試：行程狀態機 + WS 事件解析（`test/widget_test.dart`，commit 7ef6370）。
- 環境：Flutter 3.44.4 + Android SDK 36 + **JDK 17**（JDK 26 會壞 build）；模擬器 AVD `m6_pixel`。

### 後台前端（line-fleet-admin，React+TS+Vite）
- 頁面：登入、即時車隊地圖（MapLibre + WS）、訂單列表、**訂單詳情 + 軌跡回放**（commit 1702fec）、司機列表、日報表。
- Ant Design(zh_TW) + TanStack Query + axios(JWT) + react-router 受保護路由；`npm start` 一鍵開前後端。

## 待補強（2026-07-08 盤點，依優先序）

> 逐項驗收條件見 [gap-analysis-plan](2026-07-07-gap-analysis-plan.md)；App/後台各自的清單在
> `line-fleet-app/docs/TODO.md`、`line-fleet-admin/docs/TODO.md`。

1. **M7 乘客端 App = 0%**（最大缺口；後端依賴已解除，可直接動工）：`main_customer.dart` 仍是 placeholder。B1 登入 → B2 地圖叫車 → B3 WS 追蹤 → B4 狀態流。先寫 M7 slice 計畫（B6）。
2. **後端安全洞**：`GET /api/rides/:id/track`、`GET /api/reports/daily` 無認證（main.go 公開群組）；後者與 admin 版重複，建議下架。
3. **A1 司機背景定位**：現只有 geolocator 前景回報，「解 LIFF 死穴」賣點未兌現。
4. **A2/D1 FCM 推播**：App 被殺收派單。需推播抽象層 + `device_tokens` 表（migration 未建）+ Firebase 專案 + 真裝置。
5. **P1 司機 API**：`/api/driver/me`、顯式 online/offline、`/api/driver/rides/active`（App 重啟恢復行程）、HTTP 拒單 decline — 路由皆不存在。
6. **D4 `ride_events` 審計表**：migrations 只到 000007，未建。
7. **後台寫入**：D2 司機停用（須配派單池）+ C2 UI、D3 派單參數設定 + C3 UI、admin 強制取消 — admin 路由目前全 GET。
8. **品質**：C4 admin 無測試、無 code-splitting（單包 >500KB）；C5 各頁視覺截圖驗證未做；A4 M6 計畫勾選框未回填（留待附實跑證據再勾）。
9. **DevOps**：E2 三 repo 皆無 CI；E3 生產部署（現僅 dev docker-compose）；E4 監控（Prometheus/Grafana）。
10. **延後**：A5 iOS build（需完整 Xcode + CocoaPods）；D6 RBAC 多角色；D7 Phase C 計費/評分/金流/metrics。

## 環境備忘
- Flutter/Android 環境變數在 `~/.zshrc`（JAVA_HOME→openjdk@17、ANDROID_HOME、PATH）。Bash 工具跨回合 cwd 會重設，跑 flutter/adb 前自行 export。
- Node v23 + npm 10（後台前端用）。
- 後端 docker：`cd line-fleet-dispatch && docker compose up --build -d`；或在 line-fleet-admin 用 `npm start`（一起開前後端）。
- 帳號：後台 **admin / admin**。
- 踩雷紀錄見 `line-fleet-dispatch/docs/decisions.md`（含 JDK 版本、家目錄 git repo 陷阱、.env 覆蓋種子密碼等）。
