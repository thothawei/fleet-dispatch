# 後端缺少的 API — 待補清單

> 建立：2026-07-07（line-fleet-dispatch）。承接 [gap-analysis-plan](2026-07-07-gap-analysis-plan.md) 的 D 節，把「後端缺哪些 API」拆到端點層級。
> 依據：實測 `cmd/server/main.go` 路由 + `internal/handler|service` 現有方法。
> **重點結論**：叫車/派單/取消/軌跡的核心 service 幾乎都已存在（`RideService.CreateFromLocation`、`DispatchService.*`、`RideQueryService.TrackGeoJSON`），缺的主要是「對 App / 後台暴露 HTTP 端點」與「寫入類後台功能」，而非重寫邏輯。

---

## 現有端點（基準，勿重複造）

| Method | Path | 認證 | 說明 |
|---|---|---|---|
| POST | `/webhook/line` | LINE 簽章 | LINE 叫車入口（**下單邏輯藏在這**） |
| GET | `/ws` | JWT | 即時位置/事件通道 |
| POST | `/api/{driver,customer,admin}/register`·`/login` | 公開 | 三種身分註冊/登入 |
| POST | `/api/driver/location` | driver JWT | 回報 GPS |
| POST | `/api/rides/:id/{accept,pickup,complete,cancel}` | driver JWT | 司機行程操作 |
| GET | `/api/rides/:id/track` | **無（TODO）** | 軌跡 GeoJSON |
| GET | `/api/reports/daily` | **無（TODO）** | 日報表 |
| GET | `/api/admin/{fleet,drivers,rides,rides/:id,reports/daily}` | admin JWT | 後台唯讀 |

---

## P0 — 擋住乘客 App 端到端 ✅ 全數完成（2026-07-07）

| # | Method | Path | 認證 | 說明 | 複用 |
|---|---|---|---|---|---|
| 1 | POST | `/api/rides` | customer JWT | ✅ 已實作（`RideService.CreateByCustomer` 含進行中訂單守門，`internal/handler/ride.go` `Create`，commit 4f2ec93）。App 直接下單叫車（帶上車/目的地座標） | 複用 `CreateFromLocation` 核心 |
| 2 | GET | `/api/customer/rides/active` | customer JWT | ✅ 已實作（`RideQueryService.GetActiveRideByCustomer`，`internal/handler/ride.go` `ActiveByCustomer`）。乘客當前進行中訂單（App 啟動/重連要靠它取得 ride_id 才能 WS 訂閱） | 新增 `GetActiveRideByCustomer` |
| 3 | GET | `/api/customer/rides/:id` | customer JWT | ✅ 已實作（`RideQueryService.GetRideForCustomer`，`internal/handler/ride.go` `GetByCustomer`，含 owner 檢查）。乘客看自己單一訂單狀態/司機/ETA | 複用 ride repo，加 owner 檢查 |
| 4 | POST | `/api/rides/:id/cancel-by-customer` | customer JWT | ✅ 已實作（`DispatchService.CancelByCustomerID`，`internal/handler/ride.go` `CancelByCustomer`，複用 `cancelActiveRide` 核心）。乘客端 App 取消（現取消只認 LINE 文字「取消」） | `DispatchService.CancelByCustomer`（改吃 customer_id） |

> 註：#1、#4 的 service 已存在但入參綁 `line_user_id`；需讓乘客 JWT 能映射到對應 customer 的 line_user_id（M5-CUSTOMER-AUTH 的 customers 表已存 line_user_id）。

---

## P1 — 司機 App 完整度與可靠性

| # | Method | Path | 認證 | 說明 |
|---|---|---|---|---|
| 5 | GET | `/api/driver/me` | driver JWT | 司機個資/目前狀態（App 首頁顯示，取代信任本地） |
| 6 | POST | `/api/driver/online`·`/api/driver/offline` | driver JWT | 顯式上線/下線（目前上線是「有回報位置」的隱式狀態，下線無明確端點→無法乾淨移出派單池） |
| 7 | GET | `/api/driver/rides/active` | driver JWT | 司機當前進行中訂單（App 中途重啟要能恢復「載客中」狀態，否則行程遺失） |
| 8 | POST | `/api/rides/:id/decline` | driver JWT | 司機明確拒單（現拒單走 LINE Flex 按鈕；App 需 HTTP 版） 複用 `DispatchService.DeclineOffer` |

---

## P2 — 後台寫入 API（現全唯讀，配合前端 C2/C3）

| # | Method | Path | 認證 | 說明 |
|---|---|---|---|---|
| 9 | PATCH | `/api/admin/drivers/:id/status` | admin JWT | 停用/啟用司機。**須配合派單池**：停用者不得上線、不被派單，否則是假按鈕 |
| 10 | GET·PUT | `/api/admin/settings/dispatch` | admin JWT | 讀/改派單參數（逾時秒數、搜尋半徑、節流門檻），現多為 env/常數 |
| 11 | POST | `/api/admin/rides/:id/cancel` | admin JWT | 後台強制取消訂單 複用 `DispatchService.CancelBy*` |
| 12 | POST·GET·DELETE | `/api/admin/admins` | admin JWT | 後台使用者管理（開/停後台帳號）；配 RBAC 才有意義（優先度依需求） |

---

## P3 — 推播（撐 App 被殺也收單，配合 App A2）

| # | Method | Path | 認證 | 說明 |
|---|---|---|---|---|
| 13 | POST·DELETE | `/api/driver/device-token` | driver JWT | 註冊/註銷 FCM/APNs 裝置 token（存 `device_tokens` 表） |
| 14 | POST·DELETE | `/api/customer/device-token` | customer JWT | 同上，乘客端 |

> 端點只是入口；核心是後端推播抽象層（LINE / FCM / APNs 可切換）+ `device_tokens` 表（見 gap-plan D1）。

---

## P4 — Phase C 產品功能（依商業需求插入）

| # | Method | Path | 認證 | 說明 |
|---|---|---|---|---|
| 15 | POST | `/api/rides/estimate` | customer JWT | 下單前試算車資（里程 PostGIS 已能算 + 費率表） |
| 16 | POST | `/api/rides/:id/rating` | customer JWT | 乘客評分司機 1~5 星 + 評論 |
| 17 | GET | `/api/customer/rides` | customer JWT | 乘客歷史訂單列表（分頁） |
| 18 | GET | `/api/driver/rides` | driver JWT | 司機歷史/收入列表 |
| 19 | POST | `/api/rides/:id/payment` | customer JWT | 金流請款（預授權→行程後扣款） |
| 20 | GET | `/metrics` | 內網/無 | Prometheus 指標（派單成功率、接單耗時、在線司機數、API 延遲） |

---

## 安全洞（既有端點，需補認證，非新增）

- [ ] `GET /api/rides/:id/track`：目前**無認證**（`main.go:167` 註解「暫不保護，未來加」）。應限本趟乘客/司機或 admin。
- [ ] `GET /api/reports/daily`：**無認證**且與 `/api/admin/reports/daily` 重複。建議下架公開版，只留 admin 版。

---

## 資料層缺口（2026-07-08 實測確認，影響既有回傳）

- [ ] **`GeoPoint.Scan` 是 no-op**（`internal/model/models.go:22`，註解「M1 僅寫入，讀取留待 M4」）：任何經 GORM 讀出的 `pickup_point`/`dropoff_point` 都是零值 → 乘客 `GET /api/customer/rides/:id` 回傳的 pickup_point 為 (0,0)。要補 EWKB/WKT 解析。
- [ ] **司機端拿不到目的地**：`Ride.DropoffAddress/DropoffPoint` 存在於 model，但 handler/events 全域 grep 無任何 dropoff 輸出 → 司機 App「上車後導航去目的地」做不到。派單事件或 P1 #7（driver rides/active）回應需帶 dropoff。
- [ ] `model.Ride` 無 JSON tag（回傳欄位名為 Go 大寫欄位）；`internal/handler/ride.go` import 排序未過 gofmt。

---

## 建議實作順序

```
~~P0(#1→#2→#3→#4)~~ ✅ 已完成（2026-07-07），乘客 App 端到端已解鎖
  → 安全洞(track/reports 補認證，順手) ← 下一步，2026-07-08 複查仍未補

  → P1(#5~#8 司機 App 可靠性)
  → P2(#9,#10 後台寫入) 與前端 C2/C3 對接
  → P3(#13,#14 推播) 配合 App A2/FCM
  → P4 Phase C 依商業需求
```

## 落地備忘

- 全部沿用既有 `Handler → Service → Repository` 分層；下單/取消優先包裝既有 service，不重寫派單。
- 寫入端點記得補 `ride_events` 審計（gap-plan D4）與整合測試（testcontainers 既有模式）。
- 完成一段 → 對照驗收實跑 → commit + push（main，thothawei 金鑰）→ 回填本清單勾選框。
