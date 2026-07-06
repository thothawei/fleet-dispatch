# 強化與 App 化 完整路線圖（Roadmap）

> 承接 [spec.md](spec.md)（M1~M4 已完成並驗證）。本文件規劃「從能跑的 demo → 生產級系統 → 手機 App」的所有後續流程。
> 格式同 spec：每個項目含「做什麼」與「驗收條件」，可勾選。程式碼識別字用英文、說明用繁體中文。

---

## 0. 目標與現況

**願景**：把目前以 LINE 為前台的叫車 demo，強化成架構完整、可上線的派遣系統，並延伸出 iOS/Android 手機 App。

**現況（baseline，已完成）**：
- LINE webhook 收單（含簽章驗證）、Redis GEO 派單、SETNX 搶單鎖、OSRM ETA、PostGIS 圍籬、軌跡回放、日報表
- 分層 `Handler → Service → Repository → DB/Redis`，docker-compose 一鍵起

**四個階段**：
- **Phase A**：後端生產化強化（補洞）— 先做
- **Phase B**：即時化（WebSocket）
- **Phase C**：產品功能（計費 / 評分 / 金流 / 監控）
- **Phase D**：App 化（React Native 雙端）

---

## Phase A — 後端生產化強化

> 目標：從「demo 級」升級到「架構完整」。這階段做完，履歷含金量跳一級。

### A1. API 認證（JWT）— ✅ 已完成（2026-07-04）
> 司機端已導入 JWT + bcrypt 密碼登入；`/api/driver/location` 與 `/api/rides/:id/{accept,pickup,complete}` 受保護，driver_id 一律取自 token。已 build/vet/test 綠燈並 docker 端到端驗證。
> 客戶端共用 user 表留待 Phase D（LINE 客戶不直接打 /api，走 webhook 簽章保護，暫無暴露）。

**做什麼**
- 現況 `/api/driver/*`、`/api/rides/*` 全裸，任何人知道 `driver_id` 就能替別人操作
- 導入 JWT：客戶、司機各一種身分；登入/註冊發 token；中介層 `auth.jwt` 驗證
- 司機操作（接單/上車/完成）只能操作「自己的」訂單（比對 token 內 driver_id）

**驗收條件**
- [x] 未帶 token 打 `/api/rides/:id/accept` → `401`（smoke_test 已含此斷言）
- [x] 司機 A 的 token 去接／完成「司機 B 的訂單」→ `403`（雙司機實測通過）
- [ ] LINE 客戶身分與 App 客戶身分共用同一套 user 表（延到 Phase D）

### A2. 派單重試（re-dispatch timeout）— ✅ 已完成（2026-07-04）
> 派單改為多輪逾時重派：每輪擴大搜尋半徑、只派給尚未派過的待命司機；達上限仍無人接則自動取消並通知客戶。已 docker 端到端驗證（司機在線不接單 → ASSIGNED→CANCELLED，log 顯示 attempt=1 派單/attempt=2 跳過已派司機/逾時取消），正常接單流程回歸測試亦綠。

**做什麼**
- 現況：派給幾台車後若都不接，訂單永遠卡在 `ASSIGNED`
- 加逾時重派：派單後啟一個計時器（goroutine + timer，或背景掃描），**N 秒內無人接** →
  1. 擴大搜尋半徑再派下一批司機；或
  2. 連續數輪無人接 → 通知客戶「目前無司機」並可取消

**驗收條件**
- [x] 模擬「所有被派司機都不接」→ 逾時後系統自動取消並通知客戶（實測 ASSIGNED→CANCELLED）
- [x] 訂單不會永久停在 `ASSIGNED`（由 giveUpIfUnaccepted 終結，已實測）
- [x] 重派不重複派給同一台車（offered map 過濾，log 顯示 attempt=2「本輪無新可用司機」）

### A3. 取消流程（cancel）— ✅ 已完成（2026-07-04）
> 客戶取消（LINE 傳「取消」）、司機拒單（Flex「拒絕」按鈕→加入 Redis 拒接集合，重派跳過）、司機放棄已接單（POST /rides/:id/cancel→重派給別人）。取消一律釋放搶單鎖、司機回待命、通知對方。已 docker 端到端驗證。

**做什麼**
- `CANCELLED(9)` 狀態已存在但無端點。補三條路徑：
  - 客戶取消（接單前免責、接單後可設政策）
  - 司機拒單 / 放棄
  - 系統逾時自動取消
- 取消時：釋放搶單鎖、司機轉回待命、通知對方

**驗收條件**
- [x] 客戶取消 → 訂單 `CANCELLED`、通知司機（實測 ride→9、司機待命）
- [x] 司機拒單/放棄 → 不再被本單重派（實測重派 offered 2→1，被放棄者被排除）
- [x] 取消後司機回 `待命`、Redis 鎖已釋放（releaseAndReset 實測）

### A4. ETA 推播節流
**做什麼**
- 現況：司機每次回報位置就推播客戶一次 → 洗版、耗 LINE/推播額度
- 改為「距離變化超過門檻（如 300m）或距上次推播超過 30 秒」才推一次

**驗收條件**
- [ ] 司機 5 秒回報一次、連續 1 分鐘，客戶收到的 ETA 推播 ≤ 3 則
- [ ] 抵達（進入圍籬）仍即時通知，不受節流影響

### A5. 軌跡分區自動維護
**做什麼**
- `ride_tracks` 按月分區。補**排程自動預建下個月分區**，避免跨月寫入失敗
- 加保留策略（如保留 12 個月，舊分區 detach/歸檔）

**驗收條件**
- [ ] 排程能自動建立「下個月」分區（dry-run 可先驗證）
- [ ] 模擬寫入次月時間的軌跡 → 落在正確分區、不報錯

### A6. 測試補強
**做什麼**
- 現況只有簽章單元測試。補關鍵路徑整合測試（testcontainers 起真 PG/Redis）：
  - **搶單併發**：N 個 goroutine 同時接同一單，只有 1 個成功
  - **圍籬判定**：ST_DWithin 邊界（99m 內/外）
  - **派單**：最近司機選取、離線過濾

**驗收條件**
- [ ] `go test ./...` 綠燈，含上述整合測試
- [ ] 搶單併發測試能穩定重現「只有一位成功」

---

## Phase B — 即時化（WebSocket）

> 目標：App 要在地圖上即時看司機移動，輪詢 API 體驗差；改用 WebSocket 推。

### B1. WebSocket 即時位置通道
**做什麼**
- 開 `/ws`（JWT 驗證），客戶訂閱「自己這趟的司機位置」，司機位置更新即時廣播給對應客戶
- 後端用 hub/channel 管理連線（Go 原生 goroutine + channel）

**驗收條件**
- [ ] 客戶連上 WS，司機每次回報位置，客戶端 1 秒內收到新座標
- [ ] 斷線自動清理、重連可續訂

### B2. 訂單事件廣播
**做什麼**
- 訂單狀態變化（派單/接單/抵達/上車/完成/取消）即時推送給相關方
- 新增 `ride_events` 表留審計軌跡

**驗收條件**
- [ ] 每次狀態轉換，客戶與司機端都即時收到事件
- [ ] `ride_events` 完整記錄每筆轉換與時間

---

## Phase C — 產品功能

### C1. 計費（fare）
- 距離（PostGIS 已能算）+ 時間 + 費率表（起跳/每公里/每分鐘/尖峰加成）
- 完成時計算車資寫入訂單
- 驗收：[ ] 一趟完成後車資 = 起跳 + 里程費 + 時間費，數字可手算核對

### C2. 評分
- 客戶對司機 1~5 星 + 評論；司機平均分入 driver 表
- 驗收：[ ] 完成後客戶可評分、司機平均分正確更新

### C3. 金流
- 串支付（信用卡/第三方）；預授權→行程後請款
- 驗收：[ ] 完成行程觸發扣款、失敗有補救流程（你本業最熟，最快）

### C4. 可觀測性（metrics）
- Prometheus：派單成功率、平均接單耗時、平均 ETA、在線司機數、API 延遲
- 驗收：[ ] `/metrics` 端點輸出上述指標，可接 Grafana 面板

---

## Phase D — App 化（React Native 雙端）

> 後端約 8 成可沿用；主要是把「LINE 前台」換成「App 前台 + 原生定位 + 推播 + WebSocket」。

### D0. 技術決策
- **前端框架**：React Native（沿用你熟的 JS/React 生態，一套碼 iOS+Android）
- **地圖**：react-native-maps（Google Maps）或 Mapbox
- **推播**：FCM（Android）/ APNs（iOS）取代 LINE push
- **背景定位**：原生背景定位取代 LIFF（**直接解決現有最大限制**）

### D1. 後端為 App 調整
**做什麼**
- 沿用 Phase A 的 JWT、Phase B 的 WebSocket
- 新增 `device_tokens` 表存 FCM/APNs token；推播層抽象成介面（LINE / FCM / APNs 可切換）
- 客戶/司機註冊、登入、個人資料 API

**驗收條件**
- [ ] 同一套訂單/派單邏輯，前台換成 App 也能完整跑一趟
- [ ] 推播能依使用者裝置走 FCM/APNs

### D2. 司機 App（先做，鏈路最集中）
**功能**：登入 → 上線/待命 → 收派單通知 → 接單 → 背景回報 GPS → 導航 → 上車/完成
- 驗收：[ ] 真機背景定位持續回報；[ ] 收到派單推播可接單；[ ] 完成整趟

### D3. 客戶 App
**功能**：登入 → 地圖選上車/目的地 → 叫車 → 即時看司機移動與 ETA → 完成 → 評分/付款
- 驗收：[ ] 地圖叫車→WebSocket 看車移動→完成付款整條通

### D4. LINE 版保留為輕入口
- LINE 版與 App 共用同一後端，當作「免下載的輕量叫車入口」
- 驗收：[ ] LINE 與 App 同時運作、共用訂單/司機資料

---

## 資料模型新增彙總

| 新表 | 用途 | 出現階段 |
|---|---|---|
| `users` / `auth` | 統一客戶/司機身分與登入 | A1 |
| `ride_events` | 訂單狀態轉換審計 | B2 |
| `fares` / `fare_rules` | 車資與費率 | C1 |
| `ratings` | 評分評論 | C2 |
| `payments` | 金流交易 | C3 |
| `device_tokens` | FCM/APNs 推播 token | D1 |

---

## 建議執行順序與相依

```
A1(JWT) ─┬─► A2 派單重試 ─► A3 取消 ─► A4 節流 ─► A5 分區 ─► A6 測試
         └─► B1 WebSocket ─► B2 事件
A1+B1 完成後才進 Phase D（App 依賴認證與即時通道）
Phase C 可與 D 並行，依商業需求插入
```

**務實最短路徑（想最快做出 App）**：A1 → B1 → D1 → D2（司機 App）→ D3（客戶 App）。
**想先強化作品深度**：A1 → A2 → A3 → A6，先把後端做扎實再談 App。

---

## 每階段「面試能講的故事」

- **A**：JWT 授權邊界、派單逾時重試的狀態機設計、搶單併發測試
- **B**：Go 原生 goroutine/channel 實作 WebSocket hub、即時位置廣播
- **C**：PostGIS 里程計費、Prometheus 可觀測性
- **D**：一套 Go 後端同時服務 LINE 與 iOS/Android，原生背景定位解決 LIFF 限制

每一句都有可跑的 code 佐證，這就是資深後端的敘事。
