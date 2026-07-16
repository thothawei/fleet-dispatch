# line-fleet-dispatch — 補強清單（後端）

> 建立：2026-07-11。以程式碼實測為準。
> 本檔專收「後端待辦」；總體階段規劃見 [roadmap.md](roadmap.md)、API 缺口見 [backend-api-gaps.md](backend-api-gaps.md)。
> 每完成一項：實跑驗收 → 勾選回填 → 走 PR（main 受保護，`enforce_admins: true`，不可直推）。

---

## F. 手續費／會費／營運報表（2026-07-11 規劃）

> 需求：後台可設「手續費%數」，並在報表顯示司機營業狀況（營業額）、應付總公司金額，
> 以及車隊會費的計算與收取。跨三專案，本區塊為**後端地基**，admin/app 依賴這裡。
>
> **前置事實（2026-07-11 盤點）**：目前 `rides` 表**沒有任何金額欄位**，只有實際里程
> `distance_m`（`tracking.go:228` 完成時由 `TrackDistanceM()` 以 GPS 軌跡連線計算，
> = 行程實際里程，可直接拿來計費）。日報表 `DailyDriverStats`（`repository.go:445`）
> 只統計趟數／里程／平均接客秒數，**完全沒有錢**。所以整條「營業額→手續費→付總公司」
> 都要從零建。`roadmap.md`「資料模型新增彙總」已預留 `fares`/`fare_rules`（C1）、
> `payments`（C3），本區塊將其具體化。

### 已定案設計決策

| 決策 | 結論 | 理由 |
|---|---|---|
| 車資來源 | **距離自動計費**：`起步價 + 每公里費率 × (distance_m/1000)` | 已有實際里程 |
| 收費模式 | **手續費 + 會費並存** | 每趟抽成 + 每月固定會費，兩筆都算「應付總公司」 |
| 會費週期 | **月費（固定金額）** | 每位司機每月一筆 |
| **費率快照** | 車資、手續費在**完成當下**算好寫進該筆 ride，**不從當前設定回推** | 費率日後會調，歷史報表必須用當時費率 |
| 金額單位 | 全系統統一整數（建議存「分」，顯示除 100） | 避免浮點誤差 |
| 權限 | 費率／會費設定僅 superadmin 可改（沿用既有 RBAC） | 金額設定屬敏感操作 |

### 施作項目（嚴格相依，由上而下）

> **實作進度（2026-07-11）**：F1–F7、F9-1、F9-2 已完成並過測試（build/vet 綠、
> 費率單元測試 6 案、testcontainers 整合測試 `TestBillingReports`/`TestCompleteRideSnapshotsFare`
> 對真 Postgres 跑全 migration 通過）。命名實作採 `*_cents`（金額存分）、`commission_bps`
> （手續費存基點，避免浮點）。F3 的 OSRM 里程退路已於 2026-07-12 補上（見下）。

- [x] **F1. 費率設定表 `fleet_settings`**（migration `000011`）✅
      單列表（`id` 固定 1），欄位 `base_fare_cents`／`per_km_fare_cents`／`min_fare_cents`／
      `commission_bps`（1500=15%）／`monthly_membership_fee_cents`／`updated_by`／`updated_at`，
      含非負與 bps≤10000 的 CHECK，seed 一列預設。model：`model.FleetSettings`。

- [x] **F2. rides 加計費欄位**（migration `000012`）✅
      `fare_amount_cents`／`commission_amount_cents`／`driver_net_amount_cents`（BIGINT，nullable，
      完成時定格）。`model.Ride` 已加對應 `*int64` 欄位 + snake_case json tag。

- [x] **F3. 完成時計算車資**（`internal/service/tracking.go` 的 `Complete`）✅
      里程 → `FeeSettings.Quote()`（`internal/service/fee_settings.go`，全整數運算）→
      定格寫進 ride；同時把 `fare_amount_cents` 塞進乘客 `ride.completed` 事件（為 E2 鋪路）。
      只有 `COMPLETED` 計費。
      **OSRM 里程退路（2026-07-12）✅**：`TrackingService` 注入 OSRM client（`SetOSRM`），
      完成時**計費里程＝max(GPS 軌跡里程, OSRM pickup→dropoff 路線里程)**（`billableDistanceM`）——
      軌跡真的長於路線＝司機繞路照實計；軌跡 0/稀疏偏低則用路線里程補回。存進 `distance_m` 讓報表與車資一致。
      需有 dropoff 座標才觸發退路（LINE 建的無目的地訂單維持軌跡里程）。OSRM 不可用時 client 內建 haversine×1.4 退路。
      驗收：單元測試 `TestBillableDistanceM`（8 案 max 邏輯）+ docker E2E（稀疏軌跡 track_m=0 → route_m=6263 →
      fare 21026 分＝NT$210.26，非 min_fare 8500）。

- [x] **F4. 費率設定 API**（superadmin）✅
      `GET/PUT /api/admin/settings/fees`（`AdminHandler.GetFeeSettings`/`PutFeeSettings`），
      掛在 superadmin 路由。費率採 **DB 持久化 + 記憶體快取**（刻意不同於 `DispatchSettings`
      的 env-記憶體式），重啟不還原、避免算錯帳。

- [x] **F5. 日報表金額擴充**（`ReportRepository.DailyDriverStats`）✅
      加 `total_revenue_cents`／`total_commission_cents`／`driver_net_cents`；
      同時修掉大資料量兩坑（見 F9-1/F9-2）。

- [x] **F6. 月營運報表 API**（新端點）✅
      `GET /api/admin/reports/monthly?month=YYYY-MM`（viewer 唯讀）：每司機趟數／營業額／手續費／
      司機實得（repo 聚合），handler 補月會費與 **應付總公司＝手續費＋月會費**（當月有完成行程者計會費）。

- [x] **F7. 司機收入 API**（app 司機端用）✅
      `GET /api/driver/earnings?month=YYYY-MM`（driver JWT，`DriverHandler.Earnings`）：
      回傳趟數／營業額／手續費／實得／本月會費／應付總公司；無趟數則不收會費。

- [x] **F8. 會費落帳 `membership_invoices`** ✅（migration `000013`）
      表：`driver_id`、`period`（YYYY-MM）、`amount_cents`（產生時定格快照）、
      `status`（unpaid/paid）、`paid_at`。`MembershipInvoiceRepository`：
      `GenerateForMonth`（對當月有完成行程的司機各開一張，`ON CONFLICT DO NOTHING` 冪等）、
      `List(period,status)`、`SetPaid`。API：`POST /api/admin/membership-invoices/generate`
      與 `PATCH /api/admin/membership-invoices/:id`（superadmin）、`GET /api/admin/membership-invoices`（viewer）。
      **語意**：只對「當月有完成行程」的司機開帳單，與 F6/F7「無跑車不收會費」一致；金額為產生當下快照，
      日後改月會費不影響歷史帳單。
      **單一真源（2026-07-13 修正）**：F6 月報表與 F7 司機收入原本用「即時費率」算會費，與已落帳 invoice 是兩個視角——
      調費率會追溯竄改已結清月份、對從未產生帳單的月份也照收、且無視繳款。已改為 F6/F7 的會費/應付總公司
      一律 LEFT JOIN `membership_invoices` 讀帳本快照（`repository.go` MonthlyDriverStats/DriverMonthlyEarnings），
      未產生帳單者會費為 0（尚未入帳＝尚未應付）。與車資/手續費的快照心智模型一致，三端（F6=F7=帳本）恆等、不受費率調整影響。

### 大資料量預防措施（DB scale，2026-07-11 納入）

> 報表是全表聚合（`SUM/GROUP BY` over `rides`），是最先被資料量拖垮的地方。
> 前置盤點：`rides` 目前只有 `idx_rides_status` 與 pickup GiST 索引，**無 `completed_at` 索引**；
> 現行日報表用 `WHERE r.completed_at::date = ?::date`（函式轉型 → 索引失效、全表掃）；
> 投影 `TotalDistanceM int`（`repository.go:441`）在大量加總下會溢位。這些在建計費/報表時一併修。

- [x] **F9-1. sargable 範圍查詢 + 複合索引** ✅
      日/月/司機收入查詢都改半開區間（`completed_at >= start AND < end`，移除 `::date` 轉型）；
      migration `000012` 建 `idx_rides_status_completed`（`status, completed_at`）與
      `idx_rides_driver_completed`（`driver_id, completed_at`）。
      > 待補：以造數腳本灌 50~100 萬筆做 `EXPLAIN ANALYZE`，確認實走索引範圍掃描（見驗收）。

- [x] **F9-2. 加總型別防溢位** ✅
      報表金額加總與里程加總一律 `::bigint`／Go `int64`；
      金額欄位本身 `BIGINT`（存分）。`DailyDriverReport.TotalDistanceM` 由 `int` 改 `int64`。

- [x] **F9-3. 預聚合彙總表 `daily_driver_earnings`** ✅（2026-07-13）
      表（migration `000014`）：`driver_id, day, trip_count, revenue_cents, commission_cents, net_cents`，
      PK `(driver_id, day)` + `day` 索引；`day` 以 **Asia/Taipei 日界**（與報表月界一致）。
      **重算式 rollup（非 +1 增量）**：`ReportRepository.RollupRideDay(rideID)` 於完成時
      `INSERT…SELECT…GROUP BY…ON CONFLICT DO UPDATE` 覆寫該 (司機,日) 桶——冪等、永遠等於 rides 即時聚合、
      可安全重跑/自我修復；`TrackingService.Complete` best-effort 呼叫（失敗不擋完成，rides 為真源）。
      月報表（F6）／司機收入（F7）改讀彙總表（每司機 ≤31 列），不再即時 GROUP BY 全表 rides；
      日報表維持 live（含 distance/pickup，單日有界）。migration 回填既有已完成行程。
      驗收：整合測試 `TestBillingReports`（讀彙總 + rollup + 冪等）通過；docker E2E——回填 3 司機值正確、
      月報表(彙總)==live GROUP BY rides 逐欄相同、完成新行程觸發 rollup 新增當日桶、F6==F7 跨日加總正確。

- [x] **F9-4. 查詢範圍上限 + 逾時保護** ✅（2026-07-13）
      **查詢跨度上限**：`parseRideListFilter` 對 `from`~`to` 自訂區間加 `rideListMaxRangeDays=31`（含頭尾）上限，
      超過回 `400 查詢區間不可超過 31 天`（日/月報表本就單日/單月有界，不受此限）。
      **逾時保護**：config 加 `DB_STATEMENT_TIMEOUT_MS`（預設 10000），`DSN()` 附 `statement_timeout` runtime 參數
      （pgx 於每條連線建立時套用；migrations 走 `MigrateDSN` 不受影響），啟動時 `SHOW statement_timeout` 記 log 確認。
      驗收：單元測試（parseRideListFilter 區間上限/邊界、config DSN 含/不含 timeout、MigrateDSN 不受影響）；
      docker E2E：啟動 log `statement_timeout=10s`；`?from=2026-06-01&to=2026-08-01`→400、剛好 31 天→200。

- [x] **F9-5. 分頁與有界回傳** ✅（2026-07-13）
      訂單列表 `GET /api/admin/rides` 伺服器端分頁（`offset`/日期/`q`/`total`，dispatch#2）。
      **禁無上限回傳通盤檢查**：所有「逐筆」列表查詢加共用硬上限 `MaxListRows=5000`
      （`DriverRepository.ListAll`/`ListIdle`、`AdminRepository.ListAll`、`MembershipInvoiceRepository.List`、
      `DeviceTokenRepository.ListBySubject`、`RideEventRepository.ListByRideID`），任何清單不再無上限回傳。
      報表 API 只回**聚合列**（每司機一列，隨 drivers 上限間接有界）；track 回單一 LineString（受行程長度自然有界）。
      **後續（量體上升後）**：drivers／membership 逼近 `MaxListRows` 時改真 offset/keyset 分頁（比照 rides，含前端）。

- [x] **F9-6. 會費表防重複入帳** ✅（隨 F8 migration `000013`）
      `membership_invoices` 加 `UNIQUE(driver_id, period)`（`uq_membership_driver_period`）+
      `(period, status)` 索引；`GenerateForMonth` 以 `ON CONFLICT DO NOTHING` 保證月結重跑不重複帳。
      整合測試 `TestMembershipInvoices` 已驗冪等與金額快照。

- [ ] **F9-7.（Phase 2）rides 月分割**
      量體達千萬級時，`rides` 依 `completed_at` 做 PostgreSQL declarative partitioning
      （按月），冷月資料可歸檔；先留規劃，勿過早最佳化。

### 驗收方式（後端）

- migration up/down 可逆；`fleet_settings` seed 一筆。
- **大資料量**：以造數腳本灌入 ~50萬~100萬筆已完成 rides，`EXPLAIN ANALYZE` 確認日/月報表
  走 `idx_rides_completed` 範圍掃描（非 Seq Scan），且回應時間在可接受範圍；
  驗證加總不溢位（金額與預期一致）。
- 單元/整合測試：給定里程 + 設定，`CompleteRide` 後 ride 的 `fare_amount`/`commission_amount`
  數值正確；`distance_m == 0` 走退路；改費率後**舊 ride 金額不變**（驗證快照制）。
- 報表 API：以 docker 起後端造 2~3 筆已完成行程，`reports/daily` 與 `reports/monthly`
  的營業額／手續費／應付總公司加總正確。

**跨端對帳 ✅（2026-07-11，docker 後端 + admin 真瀏覽器）**：造司機#2 一筆完成行程（`min_fare` 樓地板 8500），
`GET /api/driver/earnings`（F7）與 `GET /api/admin/reports/monthly`（F6）對該司機逐欄相同——
趟數 1、營業額 8500、手續費 1275、實得 7225、月會費 300000、應付總公司 301275（分）；
日報表（F5）金額欄亦一致。**快照制**：透過 admin 費率頁把每公里 2000→2500 再改回，
既有 ride #5 金額保持凍結 8500 未受影響。admin UI（G2/G3）逐欄渲染相同 → 三端金額對齊。

### 風險與待拍板細項

1. **里程準確度**：計費綁 GPS 軌跡里程，軌跡稀疏會少算——已補 OSRM pickup→dropoff 路線里程退路（F3，2026-07-12），
   計費里程取軌跡與路線大者；`min_fare` 仍是最後地板。是否再加「後台可手動校正單筆車資」待定。
2. **費率調整時機**：快照制 → 改費率只影響之後的行程，歷史不變（刻意）。
3. **會費落帳**：F6 先即時算，不管已繳/未繳；要狀態管理再開 F8。
4. **取消行程不計費**：只有 `COMPLETED` 計車資與手續費。
5. **大資料量**：報表聚合是最先被拖垮處，預防措施見上「大資料量預防措施（DB scale）」
   F9-1~F9-7；`daily_driver_earnings` 彙總表與 rides 月分割屬「量體上升後」啟用，勿過早最佳化。

---

## H. 對話與遺失物協尋（2026-07-13 實作）

> 需求：會員（乘客）與司機**即時**對話（WebSocket 推播，非留言板）；乘客弄丟東西可對
> 已完成行程建協尋單聯絡司機，並支付「找回處理費」——費用＝該趟車資 × 處理費%，
> % 由後台費率設定頁調整（`lost_item_fee_bps`），金額於**建單當下快照**（同車資快照制）。

- [x] **H1. 行程內對話**（migration `000015` `ride_messages`）
      `ChatService`：訊息持久化後經 WS Hub 即時推 `chat.message` 給行程雙方；
      `GET/POST /api/rides/:id/messages`（MultiAuth——本趟乘客/司機可發話、admin 唯讀稽核；
      `after` 參數供斷線重連補漏）。長度限制 500 rune（DB CHECK ≤1000 為最後防線）。
- [x] **H2. 遺失物協尋**（migration `000016` `lost_item_requests` + `fleet_settings.lost_item_fee_bps`）
      `LostItemService`：乘客對已完成行程建單（`fee_cents = round(車資 × bps / 10000)` 快照）；
      狀態機 open→found（司機尋獲）→paid（乘客付處理費，記帳式）→returned（歸還結案），
      open/found 可 closed（未尋獲/取消）；守門式 UPDATE 防競態；部分唯一索引
      `uq_lost_item_ride_active` 擋同行程重複未結案單（結案後可再開）。
      `lost_item.created`/`lost_item.updated` 即時推播行程雙方。
- [x] **H3. 處理費%設定**：`GET/PUT /api/admin/settings/fees` 帶 `lost_item_fee_bps`
      （superadmin；預設 1000＝10%，0~10000 驗證）。
- 驗收：整合測試 `TestChatSendAndList`、`TestLostItemFlow`（testcontainers 真 Postgres）；
  **live E2E 30/30 ✅（2026-07-13，docker db/redis + 本機 server + gorilla/websocket 雙連線）**：
  完整行程中乘客↔司機互傳訊息，**對方 WS 於微秒級收到 `chat.message`**（非輪詢）；
  歷史 2 則、`after` 增量補讀 1 則；路人發話/讀史/查協尋皆 403；
  協尋單 fee＝車資×10% 快照、admin 調成 20% 後**既有單凍結、新單吃新費率**；
  重複建單 409、open 付款 409、paid 後 close 409；found→paid（帶 paid_at）→returned 全鏈路；
  司機工作清單即時反映。驗畢費率還原、環境清除。
  另注意：本機 `go test ./...` 因 service 套件整合測試逾 10 分鐘，需 `-timeout 30m`
  （CI 只跑非 Docker 白名單，不受影響）。
- **Phase 2（部分完成）**：付款目前為記帳式確認（無金流）；聊天訊息的推播通知（FCM，App 被殺時）仍未做。
- [x] **H4. admin 協尋單總覽 API**（2026-07-15）：`GET /api/admin/lost-items?status=`（viewer 唯讀，
      掛 admin read 群組）——`LostItemRepository.ListAll` JOIN 司機/乘客姓名、status 白名單驗證（非法 400）、
      新的在前、`LIMIT MaxListRows`。比照 membership-invoices 的 setter 注入。
      驗收：整合測試 `TestLostItemAdminList`（JOIN 姓名/篩選/排序）＋ handler 測試（400/503/空庫 200 空陣列）；
      docker E2E——smoke_test 造完成行程→互傳訊息→建協尋單（fee 快照 850）→標記尋獲→admin 總覽含姓名/狀態/快照、
      `status=found` 有 `open` 空、非法 status 400、未帶 token 401、admin 讀 `/rides/:id/messages` 稽核 OK。
      前端頁面：fleet-frontEnd#11（`/lost-items` 總覽頁＋訂單詳情對話稽核卡）。

---

## 💰 M. 金額改用整數台幣（無小數）— ✅ 已實作（2026-07-15）

> 使用者回報（2026-07-15）：**台幣沒有小數點，支付無法用小數**。曾出現 `NT$21.03`、`NT$17.96` 這種**不可支付**金額。
> **已定案並實作：採 A 模型（維持存「分」但金額一律落在整數元，分為 100 的倍數）；手續費無條件捨去、
> 車資與遺失物處理費四捨五入。** 三端全鏈路已改（後端計算＋前端顯示），詳見下方「實作結果」。

**問題根源（現況）**：
- `fee_settings.go` `Quote()`：`fare = max(min_fare, base + roundDiv(per_km × 公尺/1000))`、
  `commission = roundDiv(fare × bps/10000)`——`roundDiv` 只四捨五入到「分」，`fare × 15%`、
  `fare × 里程` 都會落在非整數元（例：2103 分＝NT$21.03）。
- 遺失物處理費 `lost_item.go`：`fee_cents = round(車資 × lost_item_fee_bps/10000)`，同樣產生 NT$17.96 這種。
- 顯示層：admin `src/utils/money.ts`、app `lib/core/util/money.dart` 都做「分→NT$XX.XX（兩位小數）」。

**已定案的決策（2026-07-15，使用者拍板）**：
1. **金額模型 → A**：維持存「分」但所有計算/收費/顯示一律落在整數元（金額恆為 100 的倍數，schema 不動）。
2. **進位規則**：**手續費無條件捨去**（利司機）、**車資與遺失物處理費四捨五入**。
3. **快照一致性**：費率快照制已保護歷史；改規則只影響日後新行程/新帳單，未回算歷史（沿用既有心智模型）。

**影響範圍（三端全鏈路）**：
- **後端（dispatch）**：`fee_settings.go` `Quote()`／`roundDiv`、`tracking.go` 完成計費、`lost_item.go` 處理費、
  會費金額、報表聚合（daily/monthly、`daily_driver_earnings`）、司機收入 API（值本身要是整數元）。
  費率設定的輸入單位也要想清楚（目前存 `*_cents`；若改整數元要一併調 API/migration/驗證）。
- **後台（admin）**：`src/utils/money.ts` 顯示與 CSV、費率設定頁輸入（元/%）、日/月報表、會費帳單、協尋處理費顯示。
- **App**：`lib/core/util/money.dart` 顯示、司機收入頁、乘客完成卡車資、遺失物處理費與**支付金額**。

**實作結果（2026-07-15）**：
- **後端**：`fee_settings.go` 加 `roundNtd`（四捨五入到整數元）、`floorNtd`（捨去到整數元）；`Quote()` 改
  `fare = max(min_fare, roundNtd(base + round(per_km × 公尺/1000)))`、`commission = floorNtd(fare × bps/10000)`、
  `driver_net = fare − commission`。`lost_item.go` 處理費 `FeeCents = roundNtd(車資 × bps/10000)`。
  三金額皆為 100 的倍數。單元測試 `TestQuote` 加「三值皆整數元」斷言＋車資四捨五入案（1348m→NT$112）；
  整合測試 `TestBillingReports`/`TestCompleteRideSnapshotsFare`/`TestLostItemFlow` fixture 改整數元、皆綠。
  會費（固定設定值）與報表（加總已定格的整數元）本就整數，無需改。
- **前端**：admin `money.ts`、app `money.dart` 顯示改整數元、不帶小數點（防禦性四捨五入）；admin 費率頁金額欄位
  `precision=0`（%欄位維持 2 位）。相關測試斷言全部改整數元。admin 102 tests、app 73 tests 綠。
- **待做（未涵蓋）**：真金流付款目前仍是記帳式確認（無金流），金額已是整數元、可直接收；若日後接金流沿用即可。

對應前端：[line-fleet-admin/docs/TODO.md](../../line-fleet-admin/docs/TODO.md)、[line-fleet-app/docs/TODO.md](../../line-fleet-app/docs/TODO.md)。

---

## 🧍‍♂️🧍‍♀️ N. 多乘客／多停靠點行程（2026-07-16 規劃，未實作）

> 需求（使用者 2026-07-16）：乘客端可在一張訂單裡安排**多位客人**各自的上車／下車點，
> 中途可設**中斷點**（停靠點），**最多 5 位**；司機端要同步收到「客人 A/B/C/D 分別在哪上車、
> 哪下車、最終目的地在哪」，並**依最終目的地計算車資**。
>
> **這不是陌生人拼車（carpool）配對**，而是**同一張訂單、同行的多位乘客、依序停靠**
> （車隊包車情境）。不需要配對演算法、不需要跨訂單分帳。

**前置事實（2026-07-16 盤點）**：`rides` 目前是**單點對單點**——`pickup_point`（not null）＋
`dropoff_point`（nullable）各一個，計費吃 `distance_m`（GPS 軌跡 vs OSRM 路線取大者，見 F3）。
多停靠點無處可存，**必須新建資料表**。

### 施作項目（嚴格相依，由上而下）

- [ ] **N1. `ride_stops` 資料表**（新 migration）
      欄位建議：`ride_id`、`seq`（停靠順序，1..N）、`kind`（pickup／dropoff）、
      `point`（geography Point）、`address`、`passenger_label`（A/B/C/D，或 `passenger_name`）、
      `arrived_at`（司機到達時間，供軌跡稽核）。
      索引 `(ride_id, seq)`；CHECK `seq >= 1`。
      **相容性**：既有單點訂單維持只用 `rides.pickup_point`/`dropoff_point`，
      `ride_stops` 為空＝舊行為（LINE 建的無目的地訂單也走這條）。

- [ ] **N2. 乘客數上限與驗證** ✅ **已定案（2026-07-16）：5 位乘客，各自上下車**
      → **最多 5 位乘客、最多 10 個停靠點**（每位一上一下）。上限值放常數或 `fleet_settings`（實作時定）。
      驗證：每位乘客必須成對出現 pickup＋dropoff；dropoff 的 `seq` 必須大於同一位乘客的 pickup；
      最終目的地＝`seq` 最大的 dropoff。超過上限回 400。

- [ ] **N3. 建單 API 擴充**（`POST /api/rides`）
      body 加選填 `stops: [{seq, kind, lat, lng, address, passenger_label}]`。
      無 `stops` 時完全維持現行行為（不可破壞既有 App／LINE 建單）。
      `rides.pickup_point` 仍寫第一個 pickup、`dropoff_point` 寫最終 dropoff，
      讓派單／地圖／報表等既有讀取路徑不必全面改寫。

- [ ] **N4. 派單與 ETA**（`dispatch.go`）
      派單仍以「第一個上車點」找最近司機（現行邏輯不變）。
      `ride.assigned` payload 加 `stops`（司機接單前就能看到全程）——注意 FCM data 值一律是字串，
      複雜結構要 JSON 字串化，App 端解析要防禦（見 pitfall-fcm-data-all-strings）。

- [ ] **N5. 計費改吃「全程實際路線」** ✅ **已定案（2026-07-16 使用者拍板）**
      （`internal/osrm/client.go` + `tracking.go` `Complete`）

      **定案**：車資＝**全程實際路線**（起點 → 各停靠點 → 最終目的地，**含繞路**），
      不是「起點→最終目的地」的直達路線。理由：司機真的開了那些路，繞路不該做白工；
      也與現行「軌跡 vs 路線取大者」的精神一致。

      **現況與缺口（已盤點）**：
      - `billableDistanceM(trackM, routeM)`＝取大者，**這個邏輯不變**，只有 `routeM` 的算法要改。
      - `tracking.go` `Complete` 目前呼叫 `RouteDuration(pickup → dropoff)` 兩點直達 →
        多停靠點時會**嚴重低估**（繞路全部沒算到），取大者就只會拿到軌跡里程，
        軌跡若稀疏（見 F3）就會低估車資。
      - **`osrm.Client.RouteDuration` 介面寫死兩點**：
        `RouteDuration(ctx, fromLat, fromLng, toLat, toLng)`，
        URL 是 `/route/v1/driving/{lng},{lat};{lng},{lat}`。
        OSRM 的 `/route/v1` 本身支援多座標（`;` 分隔），**是我們的 client 沒開放**。

      **要做**：
      1. OSRM client 加多點介面（例如 `RouteVia(ctx, points []GeoPoint)`），
         URL 串接所有座標；保留現有兩點方法給派單 ETA 用（不動 F3／dispatch 既有行為）。
      2. `fallbackETA` 目前是「兩點 haversine × 1.4」——多點時要**逐段累加**再取和，
         否則 OSRM 掛掉時多停靠點的退路里程會退化成直達距離（同樣低估）。
      3. `Complete` 算 `routeM` 時，有 `ride_stops` 就走全程多點路線，沒有則維持現行兩點（相容舊訂單）。

      車資公式本身（起步＋每公里、整數元、快照制、`min_fare` 地板）**完全不變**。

- [ ] **N6. 司機端行程 API 帶 stops**
      `GET /api/driver/rides/active`、`ride.accepted` 等回傳 `stops`（含順序與乘客標籤），
      司機才知道下一站是誰、在哪。

- [ ] **N7. 到站標記（可選）**
      司機標記「A 已上車／B 已下車」→ 更新 `ride_stops.arrived_at`。
      決定是否需要（會影響司機端 UI 複雜度與 picked_up/completed 的語意）。

### 風險與待拍板

1. ~~**車資算法**~~ ✅ **已拍板（2026-07-16）：採全程實際路線（含繞路）**，見 N5。
   衍生風險：乘客建單時看不到預估車資，繞路越多車資越高 → 若日後要「先報價」，
   需要一支報價 API（以全程路線試算），目前**沒有**（後端只在完成時定格計費）。
2. **狀態機**：現行 `picked_up` 是單一時間點。多位乘客時「第一位上車」＝picked_up？
   還是每位各自狀態？影響 F 系列報表與 tracking 的里程計算起點。
3. ~~**上限 5 位**~~ ✅ **已拍板（2026-07-16）：5 位「乘客」，各自上下車 → 最多 10 個停靠點**。
4. **取消／缺席**：某位乘客沒出現時，司機能否跳過該停靠點？
5. **admin 呈現**：訂單詳情要顯示多個 stops 與軌跡（`line-fleet-admin` 對應項）。

---

## 🚗 O. 司機車輛資訊（車種／車牌／寵物車清潔費）（2026-07-16 規劃，未實作）

> 需求（使用者 2026-07-16，含後續拍板）：
> - 乘客端顯示司機的**車種與車牌**，以便知道搭什麼車；司機**必須先填車種車牌才能接單**（強制跳轉引導）。
> - 車種為**選單**：轎車／休旅／七人座／無障礙／**寵物用車**。
> - **寵物用車加收清潔費**（比原本車資貴一些），乘客端要顯示，**加收上限 30%**。
> - 司機換車後，乘客仍要能查到**當時的車輛**與**司機聯絡方式**，並有**留言板**可聯絡（與 H 遺失物協尋相關）。

**前置事實（2026-07-16 盤點）**：
- `drivers` 表：`line_user_id`／`name`／**`phone`（已存在，not null default ''）**／`status`／`password_hash`，
  **沒有任何車輛欄位**；司機上線只檢查定位權限，不檢查車輛資訊。
  → 「司機聯絡方式」的欄位**已經有了**，缺的是「填寫、開放給該趟乘客查詢」。
- `fleet_settings` 已有 `lost_item_fee_bps`（遺失物處理費 bps）→ 清潔費可完全比照（bps + 快照制）。
- 既有 H1 `ride_messages` 聊天（REST 歷史 + WS 即時推播）**綁 ride、無時間限制**，
  本質上已是「留言板 + 即時通知」→ 第 5 點多半是**沿用既有 chat**，而非另建一套。

### 施作項目

- [ ] **O1. `drivers` 加車輛欄位**（新 migration）
      `vehicle_type`（車種，**選單值**）、`plate_number`（車牌）、`vehicle_color`（可選）。
      **車種為選單** ✅ 已定案（2026-07-16）：`sedan`（轎車）／`suv`（休旅）／`van7`（七人座）／
      `accessible`（無障礙）／`pet`（寵物用車）。存 enum 字串或 smallint（實作時定），
      **顯示名稱在前端對應**，後端只認 code。非白名單值回 400。
      **車牌唯一性**：建議 `UNIQUE(plate_number)`（同一車牌不該同時掛兩個司機帳號）——待拍板。

- [ ] **O2. 車輛資訊 API**
      `GET/PUT /api/driver/vehicle`（driver JWT，只能改自己的）。
      驗證：車種必填、車牌必填且格式檢查（台灣車牌格式待拍板，例如 `ABC-1234`／`1234-AB`）。

- [ ] **O3. 接單前置檢查（gate）** ✅ **已定案（2026-07-16）：強制填寫，無寬限期**
      **後端強制**：無車輛資訊的司機不得被派單（`dispatch.go` 候選司機過濾）
      且 `POST /api/rides/:id/accept` 回 409／400。
      **不能只靠 App 端擋**——API 可被直接呼叫。
      **既有司機**：上線時全部沒有車輛資料 → **一律無法接單，直到填完**（使用者拍板：不設寬限期）。
      App 端以**強制跳轉**引導填寫（見 App TODO）；後端不需要回填 migration，
      但**上線前要通知司機**（營運事項，非工程項）。

- [ ] **O4. 乘客端可見車輛資訊**
      `ride.accepted` payload 與 `GET /api/customer/rides/active` 加
      `driver_vehicle_type`／`driver_plate_number`（比照現行 `driver_name` 的送法）。
      **隱私**：只在該趟行程的乘客可見（沿用既有 MultiAuth 授權），不可對外公開司機車牌。

- [ ] **O5. admin 呈現／審核（可選）**
      司機列表顯示車種車牌；是否需要「車輛審核」狀態（pending/approved）待拍板——
      若需要，O3 的 gate 條件要改成「已審核」而非「有填」。

- [ ] **O6. 寵物用車清潔費** ✅ **已定案（2026-07-16）：比例加收，上限 30%**
      **規則** ✅ 觸發條件已拍板（2026-07-16）：**依「乘客指定的車種」加收，不是依司機的車種**——
      乘客叫車時指定 `pet`（寵物用車）的行程，才在車資之上加收清潔費（見 **P. 乘客指定車種**）。
      比例由後台設定，**硬上限 30%**（超過一律拒絕）。

      **重要**：判斷依據是 `rides.required_vehicle_type == 'pet'`，**不可寫成 `driver.vehicle_type == 'pet'`**。
      乘客沒指定寵物車、卻剛好被派到寵物車司機時（未指定＝不過濾車種）**不得加收**——
      那位乘客沒有要求寵物服務。

      **資料**：`fleet_settings` 加 `pet_cleaning_fee_bps`（比照 `lost_item_fee_bps`），
      CHECK `>= 0 AND <= 3000`（3000 bps = 30%，**上限寫進 DB CHECK，不只靠 API 驗證**）。

      **計算**（`fee_settings.go` `Quote()`）：
      1. 先照現行算出 `fare`（起步＋每公里、`min_fare` 地板、`roundNtd` 整數元）
      2. 若 `rides.required_vehicle_type == 'pet'`：
         `cleaning_fee = floorNtd(fare × pet_cleaning_fee_bps / 10000)`（**捨去到整數元**，利乘客；
         比照 M 的整數台幣原則，金額必為 100 的倍數）；否則為 0／null
      3. `total = fare + cleaning_fee`
      **手續費基準待拍板**（見風險 6）：抽成要算在 `fare` 還是 `fare + cleaning_fee` 上？

      **落帳**：`rides` 加 `cleaning_fee_cents`（nullable，完成時定格，**比照費率快照制**）。
      報表（F5/F6/F7、`daily_driver_earnings`）要決定清潔費算不算「營業額」（見風險 5）。

      **乘客端可見**：完成卡與（若日後有）報價都要**分項顯示**「車資」與「清潔費」，
      不可只給總額——使用者明確要求「乘客端要顯示多增加清潔費」。

- [ ] **O7. 車輛快照 + 司機聯絡方式 + 留言板** ✅ **已定案（2026-07-16）**
      **問題**：司機換車／換手機後，乘客回頭找遺失物時，要知道「當時搭哪台車」「怎麼聯絡司機」。

      1. **車輛快照**：`rides` 加 `driver_vehicle_type`／`driver_plate_number`（完成或接單時定格）。
         **理由**：`drivers` 的車輛欄位會被司機改掉，歷史行程不能跟著變
         （與費率／會費／遺失物處理費的快照制一致）。
      2. **司機聯絡方式**：`drivers.phone` **欄位已存在**，缺的是
         (a) 司機填寫入口（併入 O2 車輛設定頁），
         (b) 開放**該趟乘客**查詢（`ride.accepted` payload／`rides/active`／遺失物協尋詳情）。
         **隱私**：僅該趟行程的乘客可見（沿用既有 MultiAuth），**絕不可公開列出**；
         是否遮罩（如 `09xx-xxx-123`）或改用轉接號碼待拍板（見風險 6）。
      3. **留言板**：**沿用既有 H1 `ride_messages`**（REST 歷史 + WS 即時；已綁 ride、無時間限制），
         **不另建一套**。遺失物協尋（H2）本來就共用同一條對話。
         需確認：行程完成很久之後乘客仍能發話（目前授權是「本趟乘客／司機」，看起來可以，
         **實作前要寫一個測試釘住這個行為**）。

### 風險與待拍板

1. ~~**車種自由文字 vs 選單**~~ ✅ **已拍板：選單**（轎車／休旅／七人座／無障礙／寵物用車），見 O1。
2. **車牌格式驗證**：台灣車牌格式多樣（舊式／新式／機車／電動車），過嚴會擋到真司機。
   建議寬鬆格式 + 後台可修正。
3. ~~**既有司機資料**~~ ✅ **已拍板：強制填寫、不設寬限期**，App 端強制跳轉引導，見 O3。
   **營運提醒**：上線當下所有司機都會無法接單，需事前通知。
4. ~~**車輛異動**~~ ✅ **已拍板：rides 落車輛快照**＋開放司機聯絡方式＋沿用既有 chat 當留言板，見 O7。

5. ~~**寵物車加收的觸發條件**~~ ✅ **已拍板（2026-07-16）：(b) 乘客叫車時指定車種**
   → 衍生出一整塊新功能，獨立成 **P. 乘客指定車種**（叫車帶車種、派單依車種過濾、乘客端選擇 UI）。
   清潔費依 `rides.required_vehicle_type == 'pet'` 觸發，與司機車種無關。

6. **手續費／營業額基準**：清潔費要不要計入抽成與營業額？
   - 若計入：司機被抽的是 `(車資＋清潔費) × 15%`，清潔費也算營業額。
   - 若不計入：清潔費全額歸司機（清潔成本本來就是司機負擔），報表要另立一欄。
   **建議不計入抽成、但在報表分項顯示**（清潔費是成本補償，不是營收）。**需拍板**。

7. **司機電話的隱私**：直接給乘客看真實號碼，司機可能被騷擾（行程結束後仍可撥）。
   選項：明碼／部分遮罩／轉接號碼（proxy number，需電信服務）。**需拍板**；
   最小可行是明碼 + 僅該趟乘客可見，但要有心理準備。

---

## 🐾 P. 乘客指定車種（2026-07-16 規劃，未實作）

> 由 O6 寵物車清潔費的拍板衍生：**加收依「乘客指定的車種」而非司機車種**（使用者 2026-07-16 選 (b)），
> 因此必須先有「乘客叫車時指定車種」的能力。
>
> **這不只服務寵物車**——無障礙車、七人座同樣是「乘客有特定需求才要」的車種，
> 本章節做的是**通用的車種需求**，清潔費只是其中一種車種的計費後果。

**前置事實（2026-07-16 盤點）**：
- 叫車 API（`internal/handler/ride.go`）的 request **沒有車種欄位**（只有 pickup/dropoff 座標與地址）。
- `dispatch.go` 候選司機過濾**只看 `driver.Status != DriverStatusIdle`**，
  完全不管車種（因為 `drivers` 現在也沒有車種欄位，見 O1）。
- 找不到司機時的現行行為：擴大半徑重派 → 達 `maxAttempts` 仍無人接 → **自動取消**並推播
  「抱歉，附近暫無可用司機，請稍後再試」（`giveUpIfUnaccepted`）。

### 施作項目

- [ ] **P1. `rides` 加 `required_vehicle_type`**（新 migration，接在 O1 之後）
      選填（null／空＝不指定，任何車種都可派）；值域同 O1 的車種 code 白名單。
      **必須存在 ride 上**（不是只當派單參數）——清潔費、報表、稽核都要回頭看「這趟乘客要的是什麼車」。

- [ ] **P2. 叫車 API 帶車種**（`POST /api/rides`）
      request 加選填 `required_vehicle_type`；非白名單值回 400。
      未帶＝維持現行行為（不過濾車種），**不可破壞既有 App／LINE 建單**。

- [ ] **P3. 派單依車種過濾**（`dispatch.go` 候選司機迴圈）
      現行：
      ```go
      driver, err := s.drivers.FindByID(id)
      if err != nil || driver.Status != constants.DriverStatusIdle { continue }
      ```
      加上：`ride.RequiredVehicleType != "" && driver.VehicleType != ride.RequiredVehicleType` → skip。
      注意這與 O3 的 gate 疊加（無車輛資料的司機本來就不得被派單）。

- [ ] **P4. 「找不到指定車種」的行為** ✅ **已定案（2026-07-16，採建議方案）：不降級＋取消原因明確化**
      1. **不降級**：找不到指定車種時**不改派一般車**，走現行 `giveUpIfUnaccepted` 自動取消。
         寵物車／無障礙車是硬需求，默默派來一般車＝服務失敗還照收錢。
      2. **取消原因明確化**：`giveUpIfUnaccepted` 目前推播固定文案「抱歉，附近暫無可用司機，請稍後再試」；
         `ride.RequiredVehicleType != ""` 時改為帶車種的文案（例如「抱歉，附近暫無寵物用車，請稍後再試」，
         車種顯示名由 code 對應）。**LINE 推播與 WS `ride.cancelled` payload 都要帶**：
         payload 加 `cancel_reason`（機器可讀，如 `no_vehicle_of_type`）＋`required_vehicle_type`，
         讓 App 端能顯示明確原因而不是只靠文案字串。
      3. 實作時機：與 P3 同一批（P3 讓「找不到」變得可能發生，兩者不可分開上線——
         只上 P3 不上 P4，乘客會收到誤導性的泛用取消訊息）。

- [ ] **P5. 乘客可讀的唯讀費率端點** ✅ **已定案（2026-07-16，採建議方案）：新增專用端點**
      **`GET /api/customer/fees`**（customer JWT，唯讀）：只回乘客該知道的欄位——
      `pet_cleaning_fee_bps`（寵物車清潔費%）；日後乘客需要知道的費率（如起步價，若要做預估）再逐一加入。
      **白名單輸出，絕不可外洩** `commission_bps`（手續費）／`monthly_membership_fee_cents`（月會費）
      等內部費率——**不是把 admin 的 response 過濾，而是 handler 另建只含白名單欄位的 struct**
      （過濾式寫法日後加欄位容易忘了濾，新 struct 預設不洩漏）。
      掛既有 customer JWT middleware；讀 `FeeSettings` 記憶體快取（與 F4 同源），無額外 DB 負擔。
      乘客端於「選擇車種」UI 呼叫，選寵物車當下顯示「將加收清潔費 X%（上限 30%）」。

### 風險與待拍板

1. ~~**降級策略**~~ ✅ **已拍板（2026-07-16）：不降級**——找不到指定車種就明確取消並說明原因，見 P4。
2. **車種供給**：若車隊裡沒有任何寵物車司機，這個選項在乘客端要不要隱藏／停用？
   需要「有哪些車種目前可用」的查詢（可由 redis 在線司機 + 車種聚合，但會增加派單前的查詢成本）。
3. ~~**乘客可讀的費率 API**~~ ✅ **已拍板（2026-07-16）：做 `GET /api/customer/fees`**，
   白名單輸出（handler 另建 struct，非過濾 admin response），見 P5。
4. **與 N（多停靠點）的交互**：多乘客行程若指定寵物車，清潔費仍只加一次（依 ride 而非依乘客）——
   實作時要釘住這個語意。

---

## 下次任務

**新需求（2026-07-16 加入，尚未實作，皆需後端地基先行）**：
- **N. 多乘客／多停靠點行程**：一張訂單最多 5 位乘客各自上下車＋中斷點；司機端同步收到全程。
  **車資已拍板（2026-07-16）：全程實際路線含繞路**（N5）——需擴充 OSRM client 支援多 waypoint
  （現行介面寫死兩點）並讓 haversine 退路逐段累加。
- **O. 司機車輛資訊／寵物車清潔費**：車種**選單**（轎車／休旅／七人座／無障礙／寵物用車）＋車牌；
  司機沒填不得接單（後端 gate＋App 強制跳轉，**不設寬限期**）；乘客端顯示車種車牌與司機聯絡方式；
  rides 落車輛快照；**寵物車加收清潔費（上限 30%，DB CHECK 釘住）**。
  **寵物車加收觸發條件已拍板（2026-07-16）：依乘客指定車種** → 衍生 **P. 乘客指定車種**
  （叫車 API 帶車種、派單依車種過濾、找不到車種的行為、乘客端加價說明）。
  P4/P5 已拍板（2026-07-16）：**不降級**（取消時明確說是車種問題，payload 帶 `cancel_reason`）、
  **新增 `GET /api/customer/fees`**（白名單輸出，不外洩手續費／月會費）。
  **仍待拍板：清潔費是否計入抽成／營業額、司機電話隱私。**

計費地基 **F1–F8＋F3 里程退路＋F9-1~F9-6＋M 整數台幣 已全數合併進 main**，三端對帳與 F3/F9-3/F9-4 皆 docker E2E 驗過。
其餘皆屬「量體上升後才需」的大資料量最佳化，勿過早做：

1. **協尋/對話 Phase 2 剩餘**：遺失物處理費真金流（目前記帳式確認）；
   聊天訊息 FCM 推播（App 被殺時）。~~admin 協尋單總覽＋對話稽核 UI~~ ✅ 2026-07-15
   （後端 H4 `GET /api/admin/lost-items`＋前端 fleet-frontEnd#11）。
2. **F9-7 rides 月分割**：量體達千萬級時依 `completed_at` 做 declarative partitioning。
3. **drivers／membership 真分頁**：逼近 `MaxListRows=5000` 上限時，比照 rides 改 offset/keyset 伺服器端分頁（含前端）。
4. **F3 強化（可選）**：軌跡稀疏偵測目前用「軌跡 vs 路線取大者」，是否再加「後台手動校正單筆車資」待產品定。

驗收前先 `EXPLAIN ANALYZE` 灌 50~100 萬筆確認走索引範圍掃描（見上「驗收方式」）。Git 走 PR（main 受保護 `enforce_admins: true`）。

## 參考

- 總體階段規劃：[roadmap.md](roadmap.md)（資料模型彙總已預留 `fares`/`fare_rules`/`payments`）
- 後端 API 缺口：[backend-api-gaps.md](backend-api-gaps.md)
- 前台對應：[line-fleet-admin/docs/TODO.md](../../line-fleet-admin/docs/TODO.md)「手續費／會費／營運報表」
- App 對應：[line-fleet-app/docs/TODO.md](../../line-fleet-app/docs/TODO.md)「手續費／會費／司機收入」
