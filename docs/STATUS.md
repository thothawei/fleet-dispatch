# 派車系統 — 工作狀態與待辦清單

> 最後更新：2026-07-06。此檔記錄「雙端 App + 後台」擴張的整體進度，跨三個 repo。
> 總體設計見 [docs/superpowers/specs/2026-07-06-fleet-dual-client-design.md](superpowers/specs/2026-07-06-fleet-dual-client-design.md)。

## Repo 與遠端

| Repo | 路徑 | 遠端 | 狀態 |
|---|---|---|---|
| 後端 line-fleet-dispatch | `~/Documents/line-fleet-dispatch` | `github.com/thothawei/fleet-dispatch` | 已 push |
| 後台前端 line-fleet-admin | `~/Documents/line-fleet-admin` | `github.com/thothawei/fleet-frontEnd` | 已 push |
| 雙端 App line-fleet-app | `~/Documents/line-fleet-app` | **尚無遠端** | 僅本機 commit |

git 慣例：fleet 三 repo 直接在 `main` 開發、commit 後直接 push（push 用 repo 內 `core.sshCommand` 指定的 thothawei 金鑰）。

## 已完成

### 後端（line-fleet-dispatch）
- **Phase A**（先前）：LINE 派單核心、JWT、派單重試/取消/節流、軌跡分區、testcontainers 測試。
- **M5-WS**：WebSocket hub（`/ws`，events.Hub 單 goroutine，Publisher 注入 dispatch/tracking 在既有 LINE 推播點旁多發事件）。
- **M5-ADMIN**：admin 認證 + 唯讀後台 API（`/api/admin/` login/fleet/drivers/rides/rides/:id/reports/daily）。
- **M5-CUSTOMER-AUTH**：乘客 line_user_id + 密碼 JWT（鏡射司機），解鎖乘客 WS 訂閱。
- **後台改帳號登入**：email→username（migration 000007）；種子超級帳號 **admin/admin**（docker-compose `ADMIN_SEED_USERNAME/PASSWORD` 預設）。單一 admin 角色 = 全權限。
- 所有 go build/vet/test 綠（含整合測試）；admin/admin 登入 docker 端到端驗證取得 token。

### 後台前端（line-fleet-admin，React+TS+Vite）
- 頁面：登入（帳號/密碼）、即時車隊地圖（MapLibre + WS driver.location）、訂單列表、司機列表、日報表——全部串接後端 API/WS。
- Ant Design(zh_TW) + TanStack Query + axios(JWT) + react-router 受保護路由。
- 開發用 Vite proxy（`/api`、`/ws`→:8080）；`npm start` 一鍵開前後端、Ctrl+C/`npm stop` 一起關。
- `npm run build`（tsc 嚴格 + vite）通過；登入→API 端到端實測通。

### 雙端 App（line-fleet-app，Flutter）
- 環境裝好：Flutter 3.44.4 + Android SDK 36 + **JDK 17**（JDK 26 會壞 build）；模擬器 AVD `m6_pixel`。
- 目前僅 `flutter create` scaffold（預設 counter app），已在模擬器實跑+互動驗證。

## 待執行清單（未做）

### App 功能（主線，尚未動工）
- [ ] **M6 司機端 App**（Flutter）：登入 → 上線/待命 → **背景 GPS 回報**（解 LIFF 死穴）→ 收派單 → 接單 → Google Maps 導航 → 抵達/上車/完成。目前只有 scaffold。
- [ ] **M7 乘客端 App**（Flutter）：地圖叫車 → WS 即時追蹤 ETA → 完成。
- [ ] line-fleet-app 尚未建 git 遠端。
- [ ] **iOS**：需完整 Xcode（App Store 7GB）+ CocoaPods，未裝；iOS build/側載延後。

### 後端
- [ ] **FCM/APNs 推播**（M5 剩項）：App 被殺時喚醒收派單。需 Firebase 專案 + 真裝置，適合和 M6 一起做。
- [ ] `ride_events` 審計表（設計 §5.2 提及，未建）。
- [ ] 後台**寫入類** API：司機停用（需派單邏輯配合，否則假功能）、派單參數設定。
- [ ] RBAC 多角色（目前單一 admin = 全權限）。
- [ ] Phase C 產品功能：計費 / 評分 / 金流 / Prometheus 監控（未來）。

### 後台前端
- [ ] 訂單詳情 + 軌跡回放（後端 `GET /rides/:id` 已回 GeoJSON，前端頁面未做）。
- [ ] 司機審核啟用/停用 UI（等後端寫入端點）。
- [ ] 派單參數設定頁。
- [ ] 元件/單元測試、bundle code-splitting（目前 >500KB 單包）。
- [ ] 視覺畫面尚未截圖驗證（瀏覽器自動化當時不可用；功能已用 curl/proxy 實測）。

## 環境備忘
- Flutter/Android 環境變數在 `~/.zshrc`（JAVA_HOME→openjdk@17、ANDROID_HOME、PATH）。Bash 工具跨回合 cwd 會重設，跑 flutter/adb 前自行 export。
- Node v23 + npm 10（後台前端用）。
- 後端 docker：`cd line-fleet-dispatch && docker compose up --build -d`；或在 line-fleet-admin 用 `npm start`（一起開前後端）。
- 帳號：後台 **admin / admin**。
- 踩雷紀錄見各 repo `docs/decisions.md`（含 JDK 版本、家目錄 git repo 陷阱、.env 覆蓋種子密碼等）。
