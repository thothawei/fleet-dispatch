# 派車系統 — 缺口分析與後續規劃書

> 建立：2026-07-07。範圍：**line-fleet-dispatch（後端）／line-fleet-app（雙端 App）／line-fleet-admin（營運後台前端）** 三個 repo。
> 目的：盤點「現在到底有什麼、還缺什麼」，把缺口拆成可勾選、可續接的待辦清單，之後照本文件逐項執行即可。
> 上位文件：[roadmap.md](roadmap.md)（Phase A~D）、[dual-client design](superpowers/specs/2026-07-06-fleet-dual-client-design.md)。
> [STATUS.md](STATUS.md) 已於 2026-07-08 同步至本文件現況。

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

## 0. 現況快照（2026-07-07 實測，以程式碼為準）

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
- [ ] **A2. FCM 推播收派單**（與後端 D1 綁）：App 被系統殺掉時，靠推播喚醒收派單。需 Firebase 專案 + 真裝置。
  - 驗收：App 完全關閉 → 叫車 → 手機跳推播 → 點開可接單。
- [x] **A3. 司機端測試**：✅ 2026-07-07 完成（commit 7ef6370）。`test/widget_test.dart` 覆蓋行程狀態機與 WS 事件解析（後續持續擴充）。
- [x] **A4. 回填 M6 計畫勾選框**、更新 STATUS.md 司機端段落：✅ 2026-07-08。
- [ ] **A5. iOS build**（延後）：需完整 Xcode + CocoaPods；背景定位的 iOS 權限設定（`Info.plist` 的 location always）。

---

## B. 乘客端 App（line-fleet-app，M7）— **最大缺口，尚未動工**

後端乘客側已就緒（`customer` JWT、WS 訂閱、`/rides/:id/track`），只差前端。建議照司機端的 `lib/core/` 共用層開 `lib/customer/`。

- [ ] **B1. 乘客登入/註冊**（重用 `core/api` + `token_storage`）。
- [ ] **B2. 地圖叫車**：選上車點/目的地 → 送單。後端端點 `POST /api/rides` **已就緒**（D5 已完成，2026-07-07），可直接串。
- [ ] **B3. 即時追蹤**：WS 訂閱司機位置 + ETA，地圖上看車移動。
- [ ] **B4. 行程狀態流**：已派單/司機接單/抵達/上車/完成 的畫面切換。
- [ ] **B5. 完成後評分/付款入口**（依賴 Phase C 的評分/金流；可先留位）。
  - 整體驗收：模擬器上「叫車 → 看到司機移動與 ETA → 司機完成 → 收到完成」整條通。
- [ ] **B6. 先寫 M7 實作計畫**（比照 M6 分 slice），再動工。

---

## C. 營運後台前端（line-fleet-admin）— 補寫入與品質

現有頁面全是唯讀，且部分後端端點尚未串。

- [x] **C1. 訂單詳情 + 軌跡回放**：✅ 2026-07-07 完成（commit 1702fec，`src/pages/OrderDetailPage.tsx`）。注意：C5 的視覺截圖驗證仍未做，詳情頁一併列入。
- [ ] **C2. 司機審核啟用/停用 UI**（依賴 D2 後端寫入端點，否則是假按鈕）。
- [ ] **C3. 派單參數設定頁**（依賴 D3 後端寫入端點）。
- [ ] **C4. 前端測試 + bundle 拆分**：目前無測試、單包 >500KB。加 code-splitting（路由層 lazy import）、關鍵頁 component 測試。
- [ ] **C5. 視覺驗證**：至今未截圖驗證（先前瀏覽器自動化不可用）。用 preview/瀏覽器實際載入各頁確認渲染。

---

## D. 後端（line-fleet-dispatch）— Phase B 尾 + Phase C + App 支撐

- [ ] **D1. 推播抽象層 + `device_tokens` 表**（撐 A2）：把推播抽成介面（LINE / FCM / APNs 可切換），存裝置 token。
  - 驗收：依使用者裝置走 FCM/APNs 送出派單推播。
- [ ] **D2. 後台寫入 API — 司機停用/啟用**：需與派單邏輯配合（停用者不進派單池），否則是假功能。
  - 驗收：後台停用司機 → 該司機不再被派單、也不能上線。
- [ ] **D3. 後台寫入 API — 派單參數設定**：逾時秒數、搜尋半徑、節流門檻等改為可線上調整（現多為 env/常數）。
- [ ] **D4. `ride_events` 審計表**（Phase B2 剩項）：記錄每次狀態轉換與時間，供訂單詳情/稽核。
  - 驗收：跑一趟後，`ride_events` 有派單→接單→上車→完成的完整時間序列。
- [x] **D5. App 直接下單端點 `POST /api/rides`**：✅ 2026-07-07 完成（commit 4f2ec93/845f16d，`RideService.CreateByCustomer` 含進行中訂單守門）。P0 #2/#3/#4 同批完成。
- [ ] **D6. RBAC 多角色**：目前單一 admin = 全權限。若要多後台使用者/權限分級再做（優先度低）。
- [ ] **D7. Phase C 產品功能**（依商業需求）：C1 計費（PostGIS 里程 + 費率表）、C2 評分、C3 金流（你本業最熟）、C4 Prometheus `/metrics`。

---

## E. 跨專案 / DevOps / 上線

- [x] **E1. line-fleet-app 建 git 遠端**：✅ 完成，`github.com/thothawei/fleet-app`，main 已 push 同步。
- [ ] **E2. CI/CD**：三 repo 皆無 CI。至少後端 `go test`、前端 `tsc + build`、App `flutter analyze/test` 的 pipeline。
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
