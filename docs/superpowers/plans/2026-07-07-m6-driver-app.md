# M6：司機端 App（Flutter）Implementation Plan

> 承接設計 §6（line-fleet-app）。後端 WS（M5-WS）+ 司機認證（既有密碼 JWT）已就緒。
> 實作在 `~/Documents/line-fleet-app`；本計畫存於後端 repo 的 plans 以集中管理。

**Goal:** 把 line-fleet-app 從 scaffold 做成可用的司機端 App：登入 → 上線/待命 → 背景 GPS 回報 → 收派單 → 接單 → Google Maps 導航 → 抵達/上車/完成。Android 為主，在 `m6_pixel` 模擬器上逐片驗證。

**Architecture:** `lib/core/`（dio API client、models、token 儲存、WS client）共用；`lib/driver/`（登入、首頁/上線、派單、行程流程）。狀態用 `ChangeNotifier + provider`。後端位址用 `--dart-define=API_BASE=...`，模擬器連宿主機用 `10.0.2.2:8080`。

**Tech Stack:** Flutter 3.44 / Dart / dio / provider / shared_preferences / web_socket_channel / geolocator / url_launcher。

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
- `GET /ws?token=`（driver JWT）→ 事件 `ride.assigned`/`ride.accepted`/`ride.cancelled`（payload 見 events/event.go）

---

## Slice 1：專案結構 + 司機登入（本次）

**做什麼**：加依賴、建 core（config/api/token）、driver 登入畫面 + 首頁 stub；登入打後端取 JWT 存起來，導到首頁顯示司機資訊 + 上線 toggle（暫僅本地）。

**Files（line-fleet-app）**
- `pubspec.yaml`：加 dio/provider/shared_preferences/web_socket_channel/geolocator/url_launcher
- `lib/core/config.dart`：`apiBase`（dart-define，預設 `http://10.0.2.2:8080`）
- `lib/core/api_client.dart`：dio 實例 + JWT 攔截器
- `lib/core/auth_store.dart`：token/driverId 存取（shared_preferences）
- `lib/core/driver_api.dart`：`login(lineUserId,password)`、`reportLocation(lat,lng)`
- `lib/driver/driver_app.dart`：MaterialApp + 依登入狀態決定 login/home
- `lib/driver/login_screen.dart`
- `lib/driver/home_screen.dart`（顯示司機、上線 toggle stub、登出）
- `lib/main.dart`：改為啟動 DriverApp

**步驟**
- [ ] 1. 加依賴：`flutter pub add dio provider shared_preferences web_socket_channel geolocator url_launcher`
- [ ] 2. 寫 core（config/api_client/auth_store/driver_api）
- [ ] 3. 寫 driver（login_screen/home_screen/driver_app），改 main.dart
- [ ] 4. `flutter analyze` 綠
- [ ] 5. 後端起來 + 註冊測試司機 `driver001/pw123456`；模擬器跑 App，登入 → 進首頁顯示 driver_id/名稱
- [ ] 6. 截圖驗證，commit

**驗收**：模擬器上輸入 driver001/pw123456 → 登入成功 → 首頁顯示司機資訊；錯誤密碼顯示錯誤；重開 App 仍保持登入（token 存 shared_preferences）。

---

## Slice 2：定位回報（上線 → 背景 GPS）
- geolocator 要位置權限；上線後每 5–10s 取位置 POST `/api/driver/location`；離線停止。
- 前景先跑通；背景回報（App 切走仍回報）用 Android foreground service（geolocator 的 `getPositionStream` + `foregroundNotificationConfig`）。
- 驗收：上線後後端 Redis 有此司機位置（`OnlineDriverLocations`）、後台地圖看得到；切到背景仍持續回報。

## Slice 3：收派單 + 接單
- 登入後連 `/ws?token=`；收到 `ride.assigned`（payload: address/eta_sec/dist_m）→ 彈出接單卡片（含倒數）。
- 按接受 → POST `/rides/:id/accept`；搶到→進「前往接客」；沒搶到→提示「已被接走」。
- 驗收：後台/模擬器叫一次車，司機 App 即時跳派單、可接單、狀態轉「載客中」。

## Slice 4：導航 + 行程狀態流程
- 接單後首頁顯示上車點 + 「導航」按鈕（url_launcher 開 Google Maps deep link）。
- 按鈕：抵達（自動/手動）、客戶已上車 → POST `/rides/:id/pickup`、完成 → `/rides/:id/complete`。
- 收 `ride.cancelled` → 回待命。
- 驗收：完整跑一趟 接單→導航→上車→完成，狀態機無誤。

---

## 備註
- iOS/背景定位的完整解（flutter_background_geolocation）與 FCM 推播延後（需 Firebase + 真裝置）；Slice 2 先用 geolocator 前景+foreground service 展示管線。
- 乘客端 App（M7）另開計畫，可大量重用 core。
