# 後端缺少的 API — 待補清單

> 建立：2026-07-07（line-fleet-dispatch）。承接 [gap-analysis-plan](2026-07-07-gap-analysis-plan.md) 的 D 節，把「後端缺哪些 API」拆到端點層級。
> 依據：實測 `cmd/server/main.go` 路由 + `internal/handler|service` 現有方法。
> **重點結論**：叫車/派單/取消/軌跡的核心 service 幾乎都已存在（`RideService.CreateFromLocation`、`DispatchService.*`、`RideQueryService.TrackGeoJSON`），缺的主要是「寫入類後台功能」與「推播/Phase C」，而非重寫邏輯。

---

## 現有端點（基準，勿重複造）

| Method | Path | 認證 | 說明 |
|---|---|---|---|
| POST | `/webhook/line` | LINE 簽章 | LINE 叫車入口（只帶 pickup，無目的地） |
| GET | `/ws` | JWT | 即時位置/事件通道 |
| POST | `/api/{driver,customer,admin}/register`·`/login` | 公開 | 三種身分註冊/登入 |
| POST | `/api/rides` | customer JWT | 乘客 App 下單（可帶 dropoff） |
| GET | `/api/customer/rides/active`·`/api/customer/rides/:id` | customer JWT | 乘客查進行中/單筆訂單 |
| POST | `/api/rides/:id/cancel-by-customer` | customer JWT | 乘客 App 取消 |
| GET | `/api/driver/me` | driver JWT | 司機個人資料 |
| POST | `/api/driver/online`·`/api/driver/offline` | driver JWT | 司機上線/下線 |
| GET | `/api/driver/rides/active` | driver JWT | 司機進行中訂單（含 dropoff） |
| POST | `/api/driver/location` | driver JWT | 回報 GPS |
| POST | `/api/rides/:id/{accept,pickup,complete,cancel,decline}` | driver JWT | 司機行程操作 |
| GET | `/api/rides/:id/track` | MultiAuth JWT | 軌跡 GeoJSON（本趟乘客/司機/admin） |
| GET | `/api/admin/{fleet,drivers,rides,rides/:id,reports/daily}` | admin JWT | 後台唯讀 |
| PATCH | `/api/admin/drivers/:id/status` | admin JWT | 司機啟用/停用 |
| GET·PUT | `/api/admin/settings/dispatch` | admin JWT | 派單參數（執行期可調） |
| POST | `/api/admin/rides/:id/cancel` | admin JWT | 後台強制取消 |

> 已下架：`GET /api/reports/daily`（公開版，2026-07-08 移除）。

---

## P0 — 擋住乘客 App 端到端 ✅ 全數完成（2026-07-07）

| # | Method | Path | 認證 | 說明 | 複用 |
|---|---|---|---|---|---|
| 1 | POST | `/api/rides` | customer JWT | ✅ 已實作（`RideService.CreateByCustomer` 含進行中訂單守門，`internal/handler/ride.go` `Create`，commit 4f2ec93）。App 直接下單叫車（帶上車/目的地座標） | 複用 `CreateFromLocation` 核心 |
| 2 | GET | `/api/customer/rides/active` | customer JWT | ✅ 已實作（`RideQueryService.GetActiveRideByCustomer`，`internal/handler/ride.go` `ActiveByCustomer`）。乘客當前進行中訂單（App 啟動/重連要靠它取得 ride_id 才能 WS 訂閱） | 新增 `GetActiveRideByCustomer` |
| 3 | GET | `/api/customer/rides/:id` | customer JWT | ✅ 已實作（`RideQueryService.GetRideForCustomer`，`internal/handler/ride.go` `GetByCustomer`，含 owner 檢查）。乘客看自己單一訂單狀態/司機/ETA | 複用 ride repo，加 owner 檢查 |
| 4 | POST | `/api/rides/:id/cancel-by-customer` | customer JWT | ✅ 已實作（`DispatchService.CancelByCustomerID`，`internal/handler/ride.go` `CancelByCustomer`，複用 `cancelActiveRide` 核心）。乘客端 App 取消（現取消只認 LINE 文字「取消」） | `DispatchService.CancelByCustomer`（改吃 customer_id） |

> 註：#1 已支援選填 `dropoff_address`/`dropoff_lat`/`dropoff_lng`（commit 345b7ad）。

---

## P1 — 司機 App 完整度與可靠性 ✅ 全數完成（2026-07-08）

| # | Method | Path | 認證 | 說明 |
|---|---|---|---|---|
| 5 | GET | `/api/driver/me` | driver JWT | ✅ `DriverHandler.Me`→`DriverRegistry.Me`。回 driver_id/name/phone/status（不含密碼雜湊） |
| 6 | POST | `/api/driver/online`·`/api/driver/offline` | driver JWT | ✅ `DriverRegistry.GoOnline/GoOffline`。online→Idle（載客中不降級）、offline→Offline（乾淨移出派單池，dispatch 以 status 過濾）；**載客中下線回 409**（`ErrDriverOnTrip`） |
| 7 | GET | `/api/driver/rides/active` | driver JWT | ✅ `DriverHandler.ActiveRide`→`RideQueryService.GetActiveRideByDriver`（複用 `FindActiveByDriver`，回 Accepted/PickedUp）。無則 `{"ride":null}`。訂單 JSON 含 dropoff 欄位 |
| 8 | POST | `/api/rides/:id/decline` | driver JWT | ✅ `RideHandler.Decline`→`DispatchService.DeclineOffer`（複用既有）。記錄拒接，重派跳過此司機，司機仍待命 |

> 驗證：service 整合測試（上下線狀態轉移＋載客中守門、GetActiveRideByDriver）＋對真 server 實跑
> 全部端點（me/online/offline/rides·active/decline 皆回正確狀態；track owner 200／他人 403）。
>
> **✅ P1 小尾巴——dropoff 資料鏈補全（2026-07-10）**
> - 下單流程：App `/api/rides` POST + LINE webhook 均支援 dropoff_lat/lng/address 參數傳入
> - 資料層：RideRepository.Create() 條件式存 dropoff_point (PostGIS geography) 與 dropoff_address
> - 派單事件：DispatchService.pushOffer() Payload 包含完整 ride object（含 dropoff），司機 WS 訂閱即可取得導航資訊
> - 驗收：單元測試 (ride_dropoff_test.go with/without cases)、整合測試 (testcontainers PostGIS)、煙霧測試 (真實伺服器端點驗證 DB 寫入)
> - 結果：司機「上車後導航去目的地」資料鏈已通暢

---

## P2 — 後台寫入 API ✅ 全數完成（2026-07-08）

| # | Method | Path | 認證 | 說明 |
|---|---|---|---|---|
| 9 | PATCH | `/api/admin/drivers/:id/status` | admin JWT | ✅ `AdminHandler.PatchDriverStatus`→`AdminOperations.SetDriverEnabled`。body `{"enabled":true/false}`；停用→`DriverStatusDisabled(3)`+移出 Redis GEO；載客中停用回 409；啟用→Offline |
| 10 | GET·PUT | `/api/admin/settings/dispatch` | admin JWT | ✅ 執行期 `DispatchSettings`（記憶體，重啟還原 env）：`radius_m`/`max_drivers`/`offer_timeout_sec`/`max_attempts`/`rate_limit_per_min` |
| 11 | POST | `/api/admin/rides/:id/cancel` | admin JWT | ✅ `AdminHandler.CancelRide`→`CancelRideByAdmin`，複用 `cancelActiveRide`（已上車不可取消） |
| 12 | POST·GET·DELETE | `/api/admin/admins` | admin JWT | 後台使用者管理（開/停後台帳號）；配 RBAC 才有意義（優先度依需求） |

---

## P3 — 推播（撐 App 被殺也收單，配合 App A2）

| # | Method | Path | 認證 | 說明 |
|---|---|---|---|---|
| 13 | POST·DELETE | `/api/driver/device-token` | driver JWT | ✅ 2026-07-08（`DeviceTokenHandler`，存 `device_tokens`；推播走 `notify.LogPusher` stub，可換真 FCM） |
| 14 | POST·DELETE | `/api/customer/device-token` | customer JWT | ✅ 同上 |

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

- [x] `GET /api/rides/:id/track`：✅ 已補認證（2026-07-08）。新增 `middleware.MultiAuth`（接受 driver/customer/admin 任一合法 token），授權在 `RideQueryService.AuthorizeTrackAccess`——admin 全放行、本趟乘客、被指派司機皆可，其餘 403、訂單不存在 404。後台前端不受影響（其軌跡走 `/admin/rides/:id` 內嵌的 `track_geojson`，非此端點）。`scripts/smoke_test.sh` 已同步（2026-07-08）。
- [x] `GET /api/reports/daily`：✅ 已下架公開版（2026-07-08）。移除路由與未用的 `reportHandler`，刪除 `internal/handler/report.go`（死碼）；只留 admin 版 `/api/admin/reports/daily`（reportRepo 仍供 admin handler 使用）。`smoke_test.sh` 改走 admin JWT。

---

## 資料層缺口（2026-07-08 實測確認，影響既有回傳）

- [x] **`GeoPoint.Scan` no-op** → ✅ 已補（2026-07-08）。實作 EWKB 解析（支援大小端、SRID 旗標、hex 字串/位元組/原始位元組三種 pgx 回傳型態，NULL 為 no-op）。單元測試 `internal/model/models_test.go` + 對真 PostGIS 的 round-trip 整合測試 `internal/service/geopoint_roundtrip_test.go`（pickup_point 由 (0,0) 修為正確 25.03/121.56）。
- [x] `model.Ride` JSON tag → ✅ 已加 snake_case tag（含 `dropoff_point`/`dropoff_address` 暴露）；`GeoPoint` 加 `lat`/`lng` tag。
- [x] **App 下單寫入 dropoff** → ✅ 已補（2026-07-08，commit 345b7ad）。`CreateByCustomer` 持久化 `dropoff_address/lat/lng`；`rides/active` 與 `ride.accepted` WS 事件回傳；司機 App 上車後導航去目的地已通。
- [x] **`ride.assigned` 派單事件 dropoff** → ✅ 已補（2026-07-08）。`rideAssignedPayload` 帶 `dropoff_address/lat/lng`；司機接單前可預覽。LINE 叫車仍無目的地。

---

## 建議實作順序

```
~~P0(#1→#2→#3→#4)~~ ✅ 已完成（2026-07-07），乘客 App 端到端已解鎖
~~安全洞(track 補認證 / reports 下架)~~ + ~~資料層(GeoPoint.Scan / JSON tag / dropoff)~~ ✅ 已完成（2026-07-08）
~~P1(#5~#8 司機 App 可靠性)~~ ✅ 已完成（2026-07-08）

  → ~~P2(#9,#10,#11 後台寫入)~~ ✅ 已完成（2026-07-08），可對接前端 C2/C3
  → P3(#13,#14 推播) 配合 App A2/FCM
  → P4 Phase C 依商業需求
```

## 新增缺口（2026-07-10 由後台前端反推）

| 編號 | 端點 | 缺什麼 | 狀態 |
|---|---|---|---|
| #15 | `GET /api/admin/rides` | `offset`、`from`/`to`（依 `requested_at`）、`q`（上車點／訂單 ID） | ✅ 2026-07-10 完成 |

### #15 完成後的契約

```
GET /api/admin/rides?status=&limit=&offset=&from=&to=&q=
```

| 參數 | 型別 | 說明 |
|---|---|---|
| `status` | int16 | 訂單狀態；省略＝全部 |
| `limit` | int | 1~500，預設 100；超出範圍回 400 |
| `offset` | int | ≥0，預設 0 |
| `from` / `to` | `YYYY-MM-DD` | 依 `requested_at` 的日期篩選，**含頭尾**；沿用 `DailyDriverStats` 的 `::date` 慣例。`from > to` 回 400 |
| `q` | string | 上車點地址 `ILIKE` 模糊比對，或訂單 ID 的子字串比對；前後空白會去除 |

回應：

```json
{ "rides": [...], "total": 42, "limit": 100, "offset": 0 }
```

- `total` 是**符合條件的總筆數**，不受 `limit`/`offset` 影響，供前端分頁器使用。
- 空結果的 `rides` 是 `[]` 而非 `null`。
- 排序固定為 `id DESC`（新到舊）。
- 參數格式錯誤一律回 400 並帶中文 `error` 訊息（原本是靜默忽略）。

實作：`RideRepository.List(RideListFilter)` 回 `(rows, total, err)`；`ListRecent` 保留為薄包裝供既有呼叫端使用。
Query 解析抽成純函式 `parseRideListFilter(url.Values)`，故 CI 無 Docker 也測得到。

## 落地備忘

- 全部沿用既有 `Handler → Service → Repository` 分層；下單/取消優先包裝既有 service，不重寫派單。
- 寫入端點的 `ride_events` 審計（gap-plan D4）✅ 2026-07-08（migration `000009` + 狀態轉換寫入 + admin 詳情 `events`）。整合測試（testcontainers）仍可後補。
- 完成一段 → 對照驗收實跑 → commit + push（main，thothawei 金鑰）→ 回填本清單勾選框。
- 煙霧測試：`bash scripts/smoke_test.sh`（track 帶司機 JWT、日報帶 admin JWT）。
