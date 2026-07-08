# M6：司機端 App（Flutter）Implementation Plan

> 承接設計 §6（line-fleet-app）。後端 WS（M5-WS）+ 司機認證（既有密碼 JWT）已就緒。
> 實作在 `~/Documents/line-fleet-app`；本計畫存於後端 repo 的 plans 以集中管理。
>
> **回填（2026-07-08 / A4）**：四片主鏈路已落地於 `line-fleet-app` `main`；證據以
> commit 歷史、`flutter analyze` / `flutter test` 為主。A1 鎖屏長跑改列「待真機驗收」。

**Goal:** 把 line-fleet-app 從 scaffold 做成可用的司機端 App：登入 → 上線/待命 → 背景 GPS 回報 → 收派單 → 接單 → Google Maps 導航 → 抵達/上車/完成。Android 為主，在 `m6_pixel` 模擬器上逐片驗證。

**Architecture:** `lib/core/`（dio API client、models、token 儲存、WS client）共用；`lib/driver/`（登入、首頁/上線、派單、行程流程）。狀態用 `ChangeNotifier + provider`。後端位址用 `--dart-define=API_BASE=...`，模擬器連宿主機用 `10.0.2.2:8080`。

**Tech Stack:** Flutter 3.44 / Dart / dio / provider / shared_preferences / web_socket_channel / geolocator / url_launcher / permission_handler。

## Global Constraints
- 只改 `~/Documents/line-fleet-app`（不碰後端/其他 repo；後端要改另開）。
- 註解/UI 文字繁中；識別字英文。
- 模擬器連後端用 `10.0.2.2`（Android 模擬器對映宿主 localhost）；後端跑在宿主 :8080。
- 跑 flutter/adb 前先 export JAVA_HOME(openjdk@17)/ANDROID_HOME/PATH（見 STATUS.md）。
- 每片做完 `flutter analyze` + 模擬器實跑驗證 + git commit（line-fleet-app 獨立 repo，main）。

## 後端契約（既有，供對接）
- `POST /api/driver/login` `{line_user_id, password}` → `{driver_id, token}`
- `POST /api/driver/register` `{name, line_user_id, password}` → `{driver_id, name, token}`
- `POST /api/driver/location`（Bearer）`{lat, lng}` → `{ok:true}`
- `POST /api/rides/:id/{accept,pickup,complete,cancel}`（Bearer）
- `GET /api/driver/rides/active`（Bearer）→ App 重啟還原進行中行程
- `GET /ws?token=`（driver JWT）→ 事件 `ride.assigned`/`ride.accepted`/`ride.cancelled`（payload 見 events/event.go）；`ride.accepted` 可帶 `dropoff_address`

## 完成證據索引（line-fleet-app）
| Slice | 代表 commit / 路徑 | 自動驗證 |
|-------|-------------------|---------|
| 1 登入 | `16772c5` 起 `lib/driver/` + `TokenStorage` | `flutter analyze` |
| 2 定位 | `cd5a039` A1：`getPositionStream` + FGS | `driverLocationSettings` 單測 |
| 3 派單 | `lib/driver/driver_controller.dart` WS + accept | WS 事件解析單測 |
| 4 行程 | dropoff 導航 + phase 狀態機 | ActiveRide / dropoff 單測 |

---

## Slice 1：專案結構 + 司機登入 — ✅ 完成

**做什麼**：加依賴、建 core（config/api/token）、driver 登入畫面 + 首頁；登入打後端取 JWT 存起來，導到首頁顯示司機資訊 + 上線 toggle。

**Files（line-fleet-app）**
- `pubspec.yaml`：dio/provider/shared_preferences/web_socket_channel/geolocator/url_launcher（後續另加 permission_handler）
- `lib/core/config/app_config.dart`、`lib/core/api/fleet_api_client.dart`、`lib/core/storage/token_storage.dart`
- `lib/driver/app.dart`、`screens/driver_login_screen.dart`、`screens/driver_home_screen.dart`
- `lib/main_driver.dart` / `lib/main.dart`

**步驟**
- [x] 1. 加依賴
- [x] 2. 寫 core（config/api/token）
- [x] 3. 寫 driver（login/home/app），改進入點
- [x] 4. `flutter analyze` 綠
- [x] 5. 登入流程可連後端（開發過程已驗證；帳密依環境）
- [x] 6. commit 進 `main`

**驗收**：登入成功進首頁、錯誤密碼顯示錯誤、token 持久化重開仍登入。

---

## Slice 2：定位回報（上線 → 背景 GPS）— ✅ 程式完成 / 真機長跑待驗

- [x] geolocator 位置權限；上線後以 `getPositionStream` POST `/api/driver/location`；離線停止。
- [x] Android `ForegroundNotificationConfig` 前景服務常駐通知（`lib/core/location/`）。
- [x] Manifest：`FOREGROUND_SERVICE*`、`POST_NOTIFICATIONS`、`ACCESS_BACKGROUND_LOCATION`；iOS `UIBackgroundModes: location`。
- [ ] **真機**：鎖屏 10 分鐘後後台地圖該司機座標仍持續更新（A1 尾巴）。

---

## Slice 3：收派單 + 接單 — ✅ 完成

- [x] 登入後連 `/ws?token=`；`ride.assigned` → 接單卡片。
- [x] 接受 → POST `/rides/:id/accept`；進入「前往上車點」；失敗顯示錯誤。
- [x] 單元測試覆蓋 `ride.assigned` / `ride.accepted` 解析。

---

## Slice 4：導航 + 行程狀態流程 — ✅ 完成

- [x] 接單後上車點 + 導航（Google Maps deep link）；上車後目的地導航（dropoff）。
- [x] 按鈕：客戶已上車 → pickup、完成 → complete、放棄 → cancel。
- [x] 收 `ride.cancelled` / `ride.completed` → 清行程。
- [x] App 重啟可由 `GET /driver/rides/active` 還原 Accepted/PickedUp。

---

## 備註
- FCM 推播（A2）仍依賴後端 D1（推播抽象 + `device_tokens`）+ Firebase + 真裝置。
- 乘客端 App（M7）已在同 repo `lib/customer/` 落地最小可用版。
