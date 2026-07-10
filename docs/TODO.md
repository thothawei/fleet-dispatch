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

- [ ] **F1. 費率設定表 `fleet_settings`**（migration `000011`）
      單列設定表，欄位：`base_fare`（起步價）、`per_km_fare`（每公里）、
      `commission_rate`（手續費%，如 0.15）、`monthly_membership_fee`（月會費）、
      `min_fare`（最低車資）、`updated_by`、`updated_at`。
      預設值以 seed 塞一筆，避免完成計費時讀不到設定。

- [ ] **F2. rides 加計費欄位**（migration `000012`）
      `fare_amount`（車資）、`commission_amount`（手續費）、
      `driver_net_amount`（司機實得＝車資−手續費）。三者於完成時定格寫入。
      同步更新 `model.Ride` 與 `AdminRideRow` 投影 + json tag（snake_case）。

- [ ] **F3. 完成時計算車資**（`internal/service/tracking.go` 的完成路徑）
      里程 → 讀當前 `fleet_settings` → `fare = base + per_km × (distance_m/1000)`
      （低於 `min_fare` 取 `min_fare`）→ `commission = round(fare × rate)` →
      連同 `driver_net` 一起寫進 ride。
      **退路**：`distance_m == 0`（軌跡稀疏／缺失）時，改用 OSRM 算 pickup→dropoff
      路線距離；仍為 0 則收 `min_fare`。
      **規則**：只有 `COMPLETED` 才計費；`CANCELLED` 不計。

- [ ] **F4. 費率設定 API**（superadmin，沿用 `SettingsService` 模式）
      `GET /api/admin/settings/fees`、`PUT /api/admin/settings/fees`。
      （對照既有 `GET/PUT /api/admin/settings/dispatch`。）

- [ ] **F5. 日報表金額擴充**（`ReportRepository.DailyDriverStats`）
      SELECT 加 `SUM(fare_amount) AS total_revenue`、`SUM(commission_amount) AS total_commission`、
      `SUM(driver_net_amount) AS driver_net`。回傳型別 `DailyDriverReport` 同步加欄位。

- [ ] **F6. 月營運報表 API**（新端點）
      `GET /api/admin/reports/monthly?month=YYYY-MM`：每位司機 →
      趟數、營業額（SUM fare）、手續費（SUM commission）、月會費（讀 `monthly_membership_fee`）、
      **應付總公司＝手續費＋月會費**、司機實得。
      月會費：對「當月有完成行程的司機」各計一筆固定月會費（**先算不落帳**；
      已繳/未繳狀態管理見 F8 Phase 2）。

- [ ] **F7. 司機收入 API**（app 司機端用）
      `GET /api/driver/earnings?month=YYYY-MM`（driver JWT）：回傳自己的
      趟數／營業額／手續費／實得／本月會費／應付總公司。

- [ ] **F8.（Phase 2）會費落帳 `membership_invoices`**
      `driver_id`、`period_year_month`、`amount`、`status`（未繳/已繳）、`paid_at`。
      需要「已繳/未繳」管理與催繳時才開；F6 先以即時計算滿足報表顯示。

### 大資料量預防措施（DB scale，2026-07-11 納入）

> 報表是全表聚合（`SUM/GROUP BY` over `rides`），是最先被資料量拖垮的地方。
> 前置盤點：`rides` 目前只有 `idx_rides_status` 與 pickup GiST 索引，**無 `completed_at` 索引**；
> 現行日報表用 `WHERE r.completed_at::date = ?::date`（函式轉型 → 索引失效、全表掃）；
> 投影 `TotalDistanceM int`（`repository.go:441`）在大量加總下會溢位。這些在建計費/報表時一併修。

- [ ] **F9-1. sargable 範圍查詢 + 複合索引**
      報表 `WHERE` 一律改 `completed_at >= :start AND completed_at < :end`
      （移除 `completed_at::date` 轉型）。新增 migration：
      `idx_rides_completed`（`status, completed_at`）供日/月報表；
      `idx_rides_driver_completed`（`driver_id, completed_at`）供司機收入查詢（F7）。

- [ ] **F9-2. 加總型別防溢位**
      `SUM(fare_amount)`／`SUM(commission_amount)`／里程加總投影改 **int64 / NUMERIC**
      （現行 `int` 在百萬筆量級會溢位）；金額欄位本身用 `BIGINT`（存分）。

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

- [ ] **F9-6. 會費表防重複入帳**（搭配 F8）
      `membership_invoices` 加 `UNIQUE(driver_id, period_year_month)` + 該複合索引，
      避免月結排程重跑造成重複帳。

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
