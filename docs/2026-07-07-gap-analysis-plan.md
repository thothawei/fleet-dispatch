# 派車系統 — 缺口分析與後續規劃書

> 建立：2026-07-07。範圍：**line-fleet-dispatch（後端）／line-fleet-app（雙端 App）／line-fleet-admin（營運後台前端）** 三個 repo。
> 目的：盤點「現在到底有什麼、還缺什麼」，把缺口拆成可勾選、可續接的待辦清單，之後照本文件逐項執行即可。
> 上位文件：[roadmap.md](roadmap.md)（Phase A~D）、[dual-client design](superpowers/specs/2026-07-06-fleet-dual-client-design.md)。
> [STATUS.md](STATUS.md) 已於 2026-07-08 同步至本文件現況。

## 0.0 2026-07-28 勾選稽核（本文件是三 repo 的編號來源，過期會害人做白工）

三個 repo 的 TODO 都引用本文件的編號（A/B/C/D/E），但它從 2026-07-08 之後就沒再回填。
逐條對照三 repo 的**現行程式碼**後補勾，每項的證據寫在該條目上。

**最會害人的一條**：B2／B3 原本寫「**待填 Google Maps API key**」——
但 App 早在 2026-07-16 就改用 **flutter_map + OpenStreetMap，完全不需要任何 key**
（`pubspec.yaml` 已無 `google_maps_flutter`，全 repo 也搜不到 `GOOGLE_MAPS_API_KEY`）。
照舊文件走的人會去申請、甚至付費開通一把根本用不到的金鑰。

本次補勾：**B2、B3、C4、C5、D2、D3**（全部 → `[x]`）；
**A5、B5、D1 生產驗收、D7** 改 `[~]` 並寫清楚做完哪一半。
**仍是 `[ ]` 的**：A1 真機驗收、A2 推播（缺 Firebase 專案／實機）、E3 生產部署、E4 監控——
這四項是真的沒做，不是沒回填。

## 0.1 2026-07-08 複查更新（實測程式碼回填）

當日盤點三 repo 現況，以下項目**已完成**並在下方勾選（各附證據）：

- **D5** App 直接下單端點 `POST /api/rides` → 已上線（`cmd/server/main.go` customerAuthed 群組；`RideService.CreateByCustomer`）。連帶 P0 #2/#3/#4（進行中訂單/單筆查詢/App 取消）也全數完成，見 [backend-api-gaps.md](backend-api-gaps.md)。
- **A3** 司機端測試 → `test/widget_test.dart` 76 行：行程狀態機 + WS 事件解析（commit 7ef6370）。
- **C1** 訂單詳情 + 軌跡回放 → `src/pages/OrderDetailPage.tsx`（commit 1702fec）。
- **E1** line-fleet-app 遠端 → `github.com/thothawei/fleet-app`，已 push 同步。

**→ B2 的依賴（D5）已解除，乘客端 App（M7）可直接動工，是目前最大缺口。**

仍未動的重點缺口（優先序）：
1. **B. 乘客端 App M7 = 0%**（`main_customer.dart` 仍是 placeholder）
2. **後端安全洞**：`GET /api/rides/:id/track`、`GET /api/reports/daily` 仍無認證（main.go 公開群組）
3. ~~**A1 背景定位**~~：✅ 2026-07-08 App 端已落地前景服務 GPS（`cd5a039`）；**待真機長跑驗收**。**A2/D1 FCM 推播**仍缺（無 firebase 依賴、無 device_tokens migration）
4. **P1 司機 API**（me/online/offline/rides/active/decline 皆不存在於路由）
5. **資料層缺口**（詳見 [backend-api-gaps.md](backend-api-gaps.md) 資料層節）：`GeoPoint.Scan` no-op → 讀回座標全零值；司機端拿不到 dropoff（「導航去目的地」被擋）；Ride 無 JSON tag
6. **D4 ride_events 審計表**（migrations 只到 000007，未建）
7. **C2/C3/D2/D3 後台寫入**（admin 路由全 GET）
8. **C4 admin 無測試無 code-splitting、C5 視覺驗證未做**
9. **E2 CI 三 repo 全無**（皆無 .github/workflows）、E3 生產部署、E4 監控
10. ~~**A4 文件回填**~~：✅ 2026-07-08 已回填 M6 計畫勾選與 STATUS（證據以 commit/`flutter test` 為主；A1 真機長跑仍待）

各 repo 端的可執行清單：App → `line-fleet-app/docs/TODO.md`、後台 → `line-fleet-admin/docs/TODO.md`、後端端點 → [backend-api-gaps.md](backend-api-gaps.md)。

---

## 0. 現況快照（2026-07-07 實測，以程式碼為準 — **歷史快照，勿當現況讀**）

> ⚠️ 2026-07-28 註記：這張表是**建立本文件當天**的狀態，已與現況嚴重脫節——
> 例如「乘客端 App 0%」在 2026-07-16 就已是含地圖叫車／即時追蹤的完整鏈路，
> 「admin 全部 GET 唯讀」也早就有寫入端點（見 D2／D3）。
> **現況請看下方各條目的 2026-07-28 稽核結果**，這張表只保留為起點對照。

| 元件 | Repo | 現況 | 完成度 |
|---|---|---|---|
| 後端派遣核心 | line-fleet-dispatch | Phase A 全綠、WS hub、admin 唯讀 API、乘客/司機/admin 三種 JWT | 高 |
| 司機端 App | line-fleet-app | 登入→上線→前景 GPS→WS 收派單→接單→導航→上車→完成→放棄，全鏈路已寫 | 高（差背景定位/推播/測試） |
| 乘客端 App | line-fleet-app | 只有 `main_customer.dart` placeholder「M7 待實作」 | **0%** |
| 營運後台前端 | line-fleet-admin | 登入、即時車隊地圖、訂單列表、司機列表、日報表 | 中（全唯讀、缺詳情/寫入/測試） |

**已驗證的後端路由**（`cmd/server/main.go`）：
- 公開：`/ws`、`POST /api/{driver,customer,admin}/{register,login}`、`GET /api/rides/:id/track`
- 司機 JWT：`POST /api/driver/location`、`POST /api/rides/:id/{accept,pickup,complete,cancel}`
- Admin JWT（**全部 GET，唯讀**）：`/api/admin/fleet`、`/drivers`、`/rides`、`/rides/:id`、`/reports/daily`

**司機端 App 已實作**（`lib/driver/driver_controller.dart` 等 722 行）：`acceptOffer / pickUpPassenger / completeTrip / abandonTrip`、geolocator 定位、WS 派單、`url_launcher` 開 Google Maps 導航。→ M6 的 4 個 slice 實質都動過，但 [M6 計畫](superpowers/plans/2026-07-07-m6-driver-app.md) 的勾選框沒回填。

---

## A. 司機端 App（line-fleet-app）— 收尾項

主鏈路已完成，缺的是「App 被殺也收得到單」與品質。

- [x] **A1. 真背景定位**：✅ 2026-07-08 完成程式落地（`getPositionStream` + Android `ForegroundNotificationConfig`；iOS background location plist）。
  - 驗收（程式）：上線後常駐通知、切背景仍走 position stream 回報。
  - [ ] 驗收（真機）：鎖屏 10 分鐘後，後台地圖上該司機座標仍持續更新。
- [ ] **A2. FCM 推播收派單**（與後端 D1 綁）：App 被系統殺掉時，靠推播喚醒收派單。
  **2026-07-28 現況**：App 端契約與解析已落地（含「FCM data 值全是字串」的型別坑修正與回歸測試），
  **後端真 FCM 也已上線**（見 D1，`internal/notify/fcm.go`）。**只差外部資源**：
  Firebase 專案 ＋ `google-services.json`（當日實查：檔案不存在）＋ 真裝置。
  - 驗收：App 完全關閉 → 叫車 → 手機跳推播 → 點開可接單。
- [x] **A3. 司機端測試**：✅ 2026-07-07 完成（commit 7ef6370）。`test/widget_test.dart` 覆蓋行程狀態機與 WS 事件解析（後續持續擴充）。
- [x] **A4. 回填 M6 計畫勾選框**、更新 STATUS.md 司機端段落：✅ 2026-07-08。
- [~] **A5. iOS build**：**階段 1–4＋7 已完成**（2026-07-21，2026-07-28 重驗）——
  Xcode 26.6 工具鏈就緒、`flutter build ios --no-codesign` **兩個 flavor 都過**
  （driver 本機 20.6MB／customer 由 CI `build-ios` job 每次 push 建）、
  雙 flavor 九組 build configuration、ATS 與背景定位 plist 齊備、iPhone 17 Pro 模擬器雙端實跑。
  詳見 App [`docs/IOS_PLAN.md`](../../line-fleet-app/docs/IOS_PLAN.md)。
  **剩階段 5**（實機部署，需接上 iPhone＋Xcode 選 Personal Team＋手機信任憑證）
  **與階段 6**（iOS 推播，卡付費 Apple Developer Program）。

---

## B. 乘客端 App（line-fleet-app，M7）— 主鏈路已落地，收尾中

後端乘客側已就緒；前端最小可用版於 2026-07-08 落地（見 App `docs/TODO.md`）。

- [x] **B1. 乘客登入/註冊**：✅ 2026-07-08（`lib/customer/` + CustomerApiClient）。
- [x] **B2. 地圖叫車** ✅ 2026-07-16（2026-07-28 查證補勾）：**改用 flutter_map + OpenStreetMap
  後不需要任何 API key**——`pubspec.yaml` 已無 `google_maps_flutter`（只有 `flutter_map` ＋ `latlong2`），
  全 repo 搜不到 `GOOGLE_MAPS_API_KEY`。模擬器實跑「地圖選點 → 反查地址 → 回填 → 叫車」，
  後端 `dropoff_point` 座標與選點一致。**原本寫的「待填 Google Maps API key」是過期資訊。**
- [x] **B3. 即時追蹤** ✅ 2026-07-16（同上，免 key）：模擬器實跑司機 marker 隨 WS
  `driver.location` 移動、相機跟隨、距離／ETA 即時更新（1427m/3分 → 676m/2分）。
- [x] **B4. 行程狀態流**：✅ WS + 取消 + 分階段畫面（含 `driver.arrived`）。
- [~] **B5. 完成後評分/付款入口**：**評分已完整上線 ✅ 2026-07-27（三端）**——
  後端 `ride_ratings`（migration 000023）＋ `POST /api/customer/rides/:id/rating`（一趟一評）、
  App 完成卡與歷史清單可補評、司機收入頁看得到自己的平均分、admin 司機管理頁「評價」欄可排序。
  模擬器實跑 E2E ＋ DB 交叉驗證通過。
  **付款仍未做**：需真金流方案（見 D7-C3），屬產品決策，不是技術待辦。
  - 整體驗收：模擬器上「叫車 → 看到司機移動與 ETA → 司機完成 → 收到完成」整條通。
- [x] **B6. 先寫 M7 實作計畫**：✅ 2026-07-08（[2026-07-08-m7-customer-app.md](superpowers/plans/2026-07-08-m7-customer-app.md)）。

---

## C. 營運後台前端（line-fleet-admin）— 補寫入與品質

現有頁面全是唯讀，且部分後端端點尚未串。

- [x] **C1. 訂單詳情 + 軌跡回放**：✅ 2026-07-07 完成（commit 1702fec，`src/pages/OrderDetailPage.tsx`）。注意：C5 的視覺截圖驗證仍未做，詳情頁一併列入。
- [x] **C2. 司機審核啟用/停用 UI**（依賴 D2 後端寫入端點，否則是假按鈕）。✅ 2026-07-08（line-fleet-admin）。
- [x] **C3. 派單參數設定頁**（依賴 D3 後端寫入端點）。✅ 2026-07-08（line-fleet-admin）。
- [x] **C4. 前端測試 + bundle 拆分** ✅（2026-07-28 查證補勾）：
  測試 **26 個測試檔／134 tests**（`npm test` 實跑）；
  code-splitting 也做了——`src/App.tsx` 全部路由走 `lazy(() => import(...))` ＋ `Suspense`。
- [x] **C5. 視覺驗證** ✅ 2026-07-08（2026-07-28 查證補勾）：
  截圖存在 `line-fleet-admin/docs/screenshots/c5-2026-07-08/`，
  另有 UI/UX 翻新後的 `ux-2026-07-10/`。

---

## D. 後端（line-fleet-dispatch）— Phase B 尾 + Phase C + App 支撐

- [x] **D1. 推播抽象層 + `device_tokens` 表**（撐 A2）：✅ 2026-07-08。migration `000008`、`internal/notify`（AppPusher + LogPusher stub）、
  `DeviceTokenService` + `POST/DELETE /api/{driver,customer}/device-token`；派單 `pushOffer` 並發 App 推播。
  - 驗收（契約）：註冊 token 後派單會走 AppPusher（目前 stub 打 log）。
  - [~] 驗收（生產）：**後端半邊已完成**——`internal/notify/fcm.go` 的 `FCMPusher`
        （真 FCM，非 stub）已上線。**App 半邊卡在 Firebase 專案**（見 A2），故整條未驗收。
- [x] **D2. 後台寫入 API — 司機停用/啟用** ✅（2026-07-28 查證補勾）：
  `PATCH /api/admin/drivers/:id/status`（ops 角色）＋ admin `DriversPage` 的 Switch。
  **不是假功能**：`DriverRegistry.GoOnline` 對 `DriverStatusDisabled` 直接回 `ErrDriverDisabled`
  （上不了線），而派單只收 `DriverStatusIdle` 的候選 → 停用者自然不進派單池。
- [x] **D3. 後台寫入 API — 派單參數設定** ✅（2026-07-28 查證補勾）：
  `GET /api/admin/settings/dispatch`（read 角色）／`PUT`（ops 角色）＋ admin `SettingsPage` 表單。
- [x] **D4. `ride_events` 審計表**（Phase B2 剩項）：✅ 2026-07-08。migration `000009`；
  service 於狀態轉換寫入審計；admin `RideDetail` 回傳 `events` 時間序列。
  - 驗收：跑一趟後，`ride_events`／admin 詳情有派單→接單→上車→完成的完整時間序列。
- [x] **D5. App 直接下單端點 `POST /api/rides`**：✅ 2026-07-07 完成（commit 4f2ec93/845f16d，`RideService.CreateByCustomer` 含進行中訂單守門）。P0 #2/#3/#4 同批完成。
- [x] **D6. RBAC 多角色**：✅ 2026-07-09。三層 viewer/dispatcher/superadmin（migration `000010`）、`AdminAuth` 查 DB（停用即時生效）+ `RequireAdminRole`、帳號管理 API（防鎖死 FOR UPDATE 交易）+ `/api/admin/me`、前端守衛與使用者管理頁。詳見 STATUS.md #9。
- [~] **D7. Phase C 產品功能**（依商業需求）——**四項做完兩項**（2026-07-28 查證）：
  - **C1 計費** ✅：F1–F8 全數上線（費率表 `fleet_settings`、OSRM 里程與軌跡取大者、
    手續費／清潔費分項、日結與月會費帳單），三端對帳通過。
  - **C2 評分** ✅ 2026-07-27：見 B5。
  - **C3 金流** ❌ 未做：需真金流方案，屬產品決策（B5 的付款那半也卡在這）。
  - **C4 Prometheus `/metrics`** ❌ 未做：`cmd/server/main.go` 沒有 metrics 路由，
    `go.mod` 也沒有 prometheus 相依（2026-07-28 grep 確認）。接 E4。

---

## E. 跨專案 / DevOps / 上線

- [x] **E1. line-fleet-app 建 git 遠端**：✅ 完成，`github.com/thothawei/fleet-app`，main 已 push 同步。
- [x] **E2. CI/CD**：✅ 三 repo 皆有（App `flutter-ci.yml`、admin `ci.yml`、後端 `go-ci.yml` 2026-07-08）。生產部署／監控仍屬 E3/E4。
- [ ] **E3. 生產部署**：目前只有 dev 用 docker-compose。缺正式環境（DB 備援、TLS、secrets 管理、OSRM/Redis/PG 的 prod 配置）。
- [ ] **E4. 監控與告警**（接 D7-C4）：Prometheus + Grafana 面板（派單成功率、接單耗時、在線司機數、API 延遲）。

---

## 優先順序建議

**若目標是「最快跑出乘客也能用的完整 demo」**（推薦）：
```
~~D5~~（已完成）→ B (乘客端 App 全鏈路) → A1 (司機背景定位)
```
理由：後端與司機端已幾乎完備，乘客端是唯一擋住「端到端不靠 LINE」的缺口；D5 已補完（2026-07-07），**B 可直接動工**，做完就有雙端 App 完整 demo。順手先補後端兩個無認證安全洞（track/reports）。

**若目標是「作品深度/生產化」**：
```
A1 背景定位 + A2/D1 推播 → D4 ride_events → C1 訂單詳情軌跡回放 → D7 計費/監控
```

**相依關係**：
- A2 依賴 D1（推播抽象 + device_tokens）
- B2 依賴 D5（App 下單端點）
- C2 依賴 D2；C3 依賴 D3
- B5 依賴 D7（評分/金流）

---

## 收尾檢查（每完成一項）

- 對照該項「驗收條件」實跑一次，記錄實測結果（非「應該會過」）。
- 回填本文件與對應 slice 計畫的勾選框。
- 完成一個有意義段落 → commit + push（fleet 三 repo 直接在 `main`，push 用 repo 內 `core.sshCommand` 的 thothawei 金鑰）。
- 階段性同步 [STATUS.md](STATUS.md)。
