# M7：乘客端 App（Flutter）Implementation Plan

> 承接設計 §7／gap B 項（line-fleet-app）。後端乘客 JWT、下單／查詢／取消、WS 推送已就緒。
> 實作在 `~/Documents/line-fleet-app`；本計畫存於後端 repo `docs/superpowers/plans/`（比照 M6）。
>
> **B6（2026-07-08）**：主鏈路已落地後補寫本計畫，作追溯與收尾清單；未完成項以 `[ ]` 標出。
>
> **For agentic workers:** 剩餘尾巴（Maps key／地圖追蹤／B5）可依下方 Slice 5–6 逐項執行；
> REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` 或 `executing-plans`。

**Goal:** 乘客可在 App 登入、叫車（含目的地）、即時看司機接近狀態與 ETA、取消／收到完成，不依賴 LINE 前台。

**Architecture:** 重用 `lib/core/`（config、WS、models、`RideStatus`）；新增 `lib/customer/`（`CustomerApiClient`、`CustomerTokenStorage`、`CustomerController`、登入／首頁／地圖選點）。狀態用 `ChangeNotifier + provider`。進入點 `lib/main_customer.dart`。

**Tech Stack:** Flutter 3.44 / Dart / dio / provider / flutter_secure_storage / web_socket_channel / geolocator / google_maps_flutter / geocoding。

## Global Constraints

- 實作 repo：`~/Documents/line-fleet-app`（後端契約變更另開 `line-fleet-dispatch`）。
- 註解／UI 繁中；識別字英文。
- 模擬器連後端：`10.0.2.2:8080`（`--dart-define=API_BASE=...`）。
- 狀態碼對齊 `internal/constants/ride.go`（取消 = **9**，非 5）。
- 每片做完：`flutter analyze` + `flutter test` + commit（`main`）。
- Google Maps：原生 key 佔位 `YOUR_*_MAPS_API_KEY`；未填 key 時地圖空白屬預期，文字叫車仍可用。

## 後端契約（既有）

| Method | Path | 說明 |
|--------|------|------|
| POST | `/api/customer/register` | `{name, line_user_id, password}` → JWT |
| POST | `/api/customer/login` | `{line_user_id, password}` → JWT |
| POST | `/api/rides` | pickup + 選填 `dropoff_address` / `dropoff_lat` / `dropoff_lng` |
| GET | `/api/customer/rides/active` | `{"ride": ...\|null}` |
| GET | `/api/customer/rides/:id` | 單筆（owner 檢查） |
| POST | `/api/rides/:id/cancel-by-customer` | 上車前取消 |
| GET | `/ws?token=` | customer JWT |

WS 事件（乘客訂閱）：`ride.accepted`（`driver_name`/`eta_sec`）、`driver.location`（`lat`/`lng`/`eta_sec`/`dist_m`）、`driver.arrived`、`ride.picked_up`、`ride.completed`、`ride.cancelled`。

## 完成證據索引（line-fleet-app）

| Slice | 代表 commit / 路徑 | 自動驗證 |
|-------|-------------------|---------|
| 1 登入 | `b20ec5c` 起 `lib/customer/` + `CustomerTokenStorage` | CustomerLoginResult 單測 |
| 2 叫車 | `b20ec5c`／`29fd566` createRide + dropoff | createRide body 單測 |
| 3 狀態流 | `07e3b0e`／`6b71351` WS + phaseLabel | status／phaseLabel 單測 |
| 4 即時追蹤（文字） | `5fc5fad` `driver.location` | WS payload 單測 |
| 5 地圖選點／地圖追蹤 | `5a73b46` 接線；**key＋地圖追蹤未做** | MapPicker 需裝置／key |
| 6 評分／付款 | **未做**（Phase C） | — |

---

## Slice 1：結構 + 乘客登入／註冊 — ✅ 完成（B1）

**做什麼**：`CustomerApiClient`、`CustomerTokenStorage`、登入／註冊畫面、依 session 導向首頁。

**Files**
- `lib/core/api/customer_api_client.dart`
- `lib/core/storage/customer_token_storage.dart`
- `lib/customer/app.dart`、`customer_controller.dart`
- `lib/customer/screens/customer_login_screen.dart`
- `lib/main_customer.dart`

**步驟（已完成）**
- [x] 1. Customer Auth API + secure storage
- [x] 2. Login／Register UI + `CustomerApp` root
- [x] 3. `flutter analyze`／相關單測
- [x] 4. commit

**驗收**：正確帳密進首頁；錯誤顯示訊息；重開 App 保持登入。

---

## Slice 2：叫車帶目的地 — ✅ 最小版完成／地圖 key 待填（B2）

**做什麼**：GPS 上車點 + 目的地；可打字或地圖選點帶 `dropoff_lat/lng`。

**Files**
- `CustomerController.placeOrder`
- `customer_home_screen.dart` 叫車表單
- `map_picker_screen.dart`
- AndroidManifest／iOS AppDelegate Maps key 佔位

**步驟（已完成／尾巴）**
- [x] 1. `POST /api/rides` 帶 `dropoff_address`（必要時 lat/lng）
- [x] 2. 地圖選點 UI + geocoding 接線
- [x] 3. createRide body 單元測試
- [ ] 4. 填入真實 Google Maps API key（Android + iOS）
- [ ] 5. iOS `pod install` + 部署目標確認（與 A5 重疊）

**驗收**：叫車後有 active ride；司機端能拿到 dropoff 導航。地圖在填 key 後可選點。

---

## Slice 3：行程狀態流 + 取消 + 分階段畫面 — ✅ 完成（B4）

**做什麼**：WS／輪詢對帳；取消；尋找／前往／已抵達／行程中文案。

**Files**
- `CustomerController._handleWsEvent`、`_applyActiveRide`
- `CustomerRide.statusLabel`／`phaseLabel(driverArrived:)`
- `_ActiveRideCard`

**步驟（已完成）**
- [x] 1. 訂閱 `ride.accepted`／`picked_up`／`completed`／`cancelled` + 15s 輪詢
- [x] 2. `cancel-by-customer`
- [x] 3. `driver.arrived` 本地旗標 →「司機已抵達上車點」
- [x] 4. RideStatus 常數對齊後端（取消 = 9）
- [x] 5. 相關單測

**驗收**：各階段文案正確；取消後回到叫車表單；完成／取消清 stale 司機名／ETA。

---

## Slice 4：即時追蹤（文字 ETA／距離）— ✅ 完成（B3 文字）

**做什麼**：收 `driver.location`，在「司機前往上車點」顯示距離／ETA。

**步驟（已完成）**
- [x] 1. `FleetEventTypes.driverLocation`
- [x] 2. 更新 `liveEtaSec`／`liveDistM`（不打 GET active）
- [x] 3. UI「司機 距您約 X 公尺 · 約 N 分鐘抵達」
- [x] 4. WS payload 單測

**驗收**：司機移動時乘客端文字 ETA 更新（節流由後端 `shouldPushETA`）。

---

## Slice 5：地圖上顯示司機移動 — [ ] 未做（B3 地圖尾巴）

**依賴**：Slice 2 的 Maps API key。

**做什麼**：active ride 期間地圖顯示上車點＋司機 marker，隨 `driver.location` 的 lat/lng 移動。

**建議 Files**
- 新 widget 或擴充 `_ActiveRideCard`／獨立 `CustomerTrackingMap`
- `CustomerController` 暴露 `liveLat`／`liveLng`（若 payload 有；否則僅 ETA）

**步驟**
- [ ] 1. 確認後端 `driver.location` payload 含 `lat`／`lng`（已有）並在 controller 存下來
- [ ] 2. 僅在 status=Accepted 且有 key 時顯示地圖
- [ ] 3. marker 更新＋可選 camera follow
- [ ] 4. 無 key／無座標時優雅降級為純文字（現況）
- [ ] 5. 單測／golden 或手動模擬器截圖
- [ ] 6. commit

**驗收**：填 key 後可見司機 marker 移動；無 key 不崩潰。

---

## Slice 6：完成後評分／付款入口 — [ ] 未做（B5）

**依賴**：後端 Phase C（計費／評分 API 尚未定義於 production 契約）。

**做什麼**：行程 `completed` 後顯示「留下評分」／「查看費用」佔位或真實 API。

**步驟**
- [ ] 1. 與後端對齊評分／帳單 endpoint
- [ ] 2. 完成態 UI（不可再取消；可回首頁）
- [ ] 3. 單測
- [ ] 4. commit

**驗收**：完成後有明確下一步，不停留在空白 active 卡。

---

## 整體驗收（跨 slice）

模擬器／雙 App：

1. 乘客註冊／登入 → 叫車（含目的地）
2. 司機上線 → 收派單 → 接單
3. 乘客看到司機名／前往／ETA（文字；有 key 則地圖）
4. 司機抵達圍籬 → 乘客「已抵達」
5. 上車 → 行程中 → 完成
6. 乘客端清行程；可再叫下一單

自動化底線：`flutter analyze` + `flutter test`（含 `driver_controller_test`／乘客 model／createRide）。

---

## 備註

- LINE 傳位置叫車仍可不帶 dropoff（產品取捨）；App 下單為 dropoff 主路徑。
- 本計畫不涵蓋乘客 FCM（優先 A2／D1 司機推播）。
- 對照清單：`line-fleet-app/docs/TODO.md` B 節；進度總表：`STATUS.md`。
