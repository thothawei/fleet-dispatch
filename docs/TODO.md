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
> （手續費存基點，避免浮點）。**唯一暫緩**：F3 的 OSRM 里程退路（見下）。

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
      **暫緩**：`distance_m == 0` 目前用 `min_fare` 樓地板擋「算成 0」，但中段里程軌跡稀疏仍會偏低；
      OSRM pickup→dropoff 退路尚未接（需把 OSRM/ETA 注入 TrackingService），留待強化。

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
      日後改月會費不影響歷史帳單。**注意**：F6 月報表仍即時算會費（live view），與已落帳的 invoice 為兩個視角。

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

- [ ] **F9-3. 預聚合彙總表 `daily_driver_earnings`**（量體上升後啟用）
      欄位：`driver_id, day, trip_count, revenue, commission, net`，
      主鍵 `(driver_id, day)`。完成行程時增量更新，或每日排程 rollup。
      報表優先讀彙總表，原始 `rides` 僅供稽核/回溯，避免每次即時 GROUP BY 全表。

- [ ] **F9-4. 查詢範圍上限 + 逾時保護**
      報表 API 限制查詢跨度（如日報表單日、月報表單月、自訂區間上限 1 個月）；
      DB 連線設 `statement_timeout`，避免誤觸全表掃描拖垮線上。

- [ ] **F9-5. 分頁與有界回傳**
      任何「逐筆」列表（訂單明細等）一律 keyset/offset 分頁、禁止無上限回傳；
      報表 API 只回**聚合列**（每司機一列，天然有界）。
      （另補後端 `GET /api/admin/rides` 的 `offset`/日期區間，解掉 admin 現有「client-side 過濾最近 100 筆」的限制。）

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

1. **里程準確度**：計費綁 GPS 軌跡里程，軌跡稀疏會少算。已規劃 OSRM／`min_fare` 退路；
   是否再加「後台可手動校正單筆車資」待定。
2. **費率調整時機**：快照制 → 改費率只影響之後的行程，歷史不變（刻意）。
3. **會費落帳**：F6 先即時算，不管已繳/未繳；要狀態管理再開 F8。
4. **取消行程不計費**：只有 `COMPLETED` 計車資與手續費。
5. **大資料量**：報表聚合是最先被拖垮處，預防措施見上「大資料量預防措施（DB scale）」
   F9-1~F9-7；`daily_driver_earnings` 彙總表與 rides 月分割屬「量體上升後」啟用，勿過早最佳化。

---

## 參考

- 總體階段規劃：[roadmap.md](roadmap.md)（資料模型彙總已預留 `fares`/`fare_rules`/`payments`）
- 後端 API 缺口：[backend-api-gaps.md](backend-api-gaps.md)
- 前台對應：[line-fleet-admin/docs/TODO.md](../../line-fleet-admin/docs/TODO.md)「手續費／會費／營運報表」
- App 對應：[line-fleet-app/docs/TODO.md](../../line-fleet-app/docs/TODO.md)「手續費／會費／司機收入」
