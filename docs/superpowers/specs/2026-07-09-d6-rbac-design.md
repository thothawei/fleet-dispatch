# D6 RBAC 多角色 — 設計規格

> 建立：2026-07-09。跨 repo：後端 `line-fleet-dispatch`、後台前端 `line-fleet-admin`。
> 對應 roadmap：[STATUS.md](../../STATUS.md) §延後 D6、[gap-analysis-plan](../../2026-07-07-gap-analysis-plan.md) D6。
> 現況：後台單一 `admin` 角色 = 全權限（`middleware.AdminAuth` 僅驗 JWT `role=="admin"` 即放行所有 `/admin/*`）。

## 目標

後台使用者分三層權限，並提供 superadmin 管理後台帳號（建立／指派角色／啟停）的能力。含後端強制層、帳號管理 API、前端 UI 與守衛。

## 1. 角色模型

三層，rank 由小到大，權限向下包含（高階含低階全部）：

| 角色 | rank | 涵蓋 |
|---|---|---|
| `viewer` | 1 | 只讀 |
| `dispatcher` | 2 | 讀 + 派單操作 |
| `superadmin` | 3 | 全部 + 後台帳號管理 |

路由 → 最低角色對應：

| 路由（`/api/admin/*`） | 動作 | 最低角色 |
|---|---|---|
| `GET fleet / drivers / rides / rides/:id / reports/daily / settings/dispatch` | 唯讀 | viewer |
| `PATCH drivers/:id/status`、`PUT settings/dispatch`、`POST rides/:id/cancel` | 派單操作 | dispatcher |
| `GET/POST/PATCH /api/admin/admins*`（帳號管理，新增） | 帳號管理 | superadmin |
| `GET /api/admin/me`（whoami，新增） | 讀自己 | 任一有效 admin |

## 2. 資料層（後端 migration `000010`）

`admins` 表加兩欄，**向後相容**：

- `role text NOT NULL DEFAULT 'superadmin'` — 現有 `admin/admin` 自動變 superadmin，維持現在的全權行為。
- `is_active boolean NOT NULL DEFAULT true` — 供停用帳號。
- `CHECK (role IN ('viewer','dispatcher','superadmin'))` — 擋掉打錯字或壞輸入造成的「幽靈零權限帳號」。

`down` migration 移除兩欄與 CHECK 約束。

## 3. 認證強制層（後端）

- **`AdminAuth` 改為驗 JWT 後查 DB**：載入該 admin 的 `role` + `is_active`。
  - `is_active=false` → 403（停用即時生效，不必等 token 過期）。
  - 找不到帳號（已刪，理論上不會發生因為改用停用）→ 401。
  - role 放進 context（`CtxAdminRole`）。
  - JWT 格式與簽發不動（`role="admin"`、`SubjectID=admins.id`），維持相容。
- **`RequireAdminRole(min AdminRole)` 中介層工廠**：用 rank 比較，掛在對應路由群組後面（`AdminAuth` 之後）。低於 min → 403「權限不足」。
- **簽名變更連鎖**：`AdminAuth` 從 `AdminAuth(secret)` 變成需注入 admin 查詢依賴（repository 或 lookup func）。所有呼叫點必須同步改：
  - `cmd/server/main.go` 路由註冊。
  - 測試：`internal/handler/admin_test.go`、`internal/middleware/admin_auth_test.go`、`internal/middleware/multi_auth_test.go`。

## 4. 帳號管理 API（後端，superadmin 限定）

掛在 `RequireAdminRole(superadmin)` 之後：

- `GET /api/admin/admins` — 列表（id / username / role / is_active / 時間戳；不回 password_hash）。
- `POST /api/admin/admins` — 建帳號（username / password / role）。**重用 `internal/service/admin_registry.go` 的 bcrypt 路徑**，不自刻雜湊。role 需通過白名單驗證。
- `PATCH /api/admin/admins/:id` — 改 role／重設密碼／啟停（`is_active`）。
- **不提供硬刪除 `DELETE`**：`ride_events` 稽核以 `actor_id` 指向 admin，硬刪會讓稽核變孤兒。要「移除」就 `is_active=false`。

**防鎖死規則（在單一 transaction 內做，含 count 檢查 + 更新）**：

- 不可停用／降級／（未來若有刪除）自己。
- 不可讓系統剩零個 active superadmin（降級或停用最後一個 superadmin → 400）。

## 5. 認證回應與 whoami

- `POST /api/admin/login`：查到 `is_active=false` → 403「帳號已停用」（不簽發 token）。成功時回應加上 `role` 欄位。
- `GET /api/admin/me`：回 `{ id, username, role }`。前端 app 啟動（含刷新頁面、token 仍在 storage）時呼叫，取得權威角色——這是前端知道自己角色的唯一可靠來源。

## 6. 前端（line-fleet-admin）

- **auth context**：登入後存 role；app 啟動時打 `GET /api/admin/me` 補回 role（刷新後 token 在但 role 遺失的情境）。role 以後端回應為準，前端 storage 僅快取。
- **路由守衛**：非 superadmin 看不到「使用者管理」頁（直接導頁時也擋）。
- **寫入控制項降級**：viewer 進到操作頁時，司機啟停、派單參數存檔、強制取消等按鈕 **disabled + 提示**。
- **使用者管理頁（superadmin）**：列表 + 建立（username/密碼/role）+ 改角色 + 啟停。呼叫 §4 API。
- **403 統一處理**：後端回 403 時前端顯示「權限不足」。（後端仍是唯一權威——前端隱藏只是 UX，即使被繞過後端照擋。）

## 7. 測試

**後端**：
- middleware rank 比較與 `RequireAdminRole` 各角色 × 各類路由的 200/403 矩陣。
- 停用帳號即時 403（middleware 查 DB）。
- Login 擋停用帳號、回應帶 role。
- `GET /api/admin/me` 回正確 role。
- 帳號管理 CRUD：建立（bcrypt）、改 role（白名單驗證）、啟停。
- 防鎖死：不可停用/降級自己、不可移除最後一個 superadmin（含並發交易正確性）。
- migration 向後相容：既有 admin → superadmin、is_active=true。
- `EnsureSeed` 明確建 superadmin。

**前端**：
- viewer 隱藏/禁用寫入控制。
- 非 superadmin 看不到使用者管理頁。
- 使用者管理 API 呼叫與 UI。
- app 啟動呼叫 `/admin/me` 補 role。

## YAGNI 取捨（不做）

- 自訂細粒度 permission 旗標（用固定三角色）。
- 帳號操作 audit log（`ride_events` 只稽核行程；帳號管理暫不稽核）。
- 密碼複雜度策略、鎖定重試。
- 硬刪除帳號。
- 前端角色以 JWT 攜帶（改角色需即時生效，故走 DB + `/admin/me`）。

## 影響檔案概覽

**line-fleet-dispatch**：
- `db/migrations/000010_admins_rbac.{up,down}.sql`（新）
- `internal/middleware/auth.go`（AdminAuth 改、RequireAdminRole 新）
- `internal/service/admin_registry.go`（EnsureSeed 設 role、建帳號/改帳號）
- `internal/service/admin_operations.go` 或新 `admin_users.go`（CRUD 邏輯 + 防鎖死交易）
- `internal/handler/admin.go`（Login 擋停用 + 回 role、`/me`、`/admins` handlers）
- `cmd/server/main.go`（路由 + 依賴注入）
- 對應測試檔。

**line-fleet-admin**：
- auth context / api 層（role、`/me`、`/admins`）
- 路由守衛、使用者管理頁、操作頁寫入控制降級
- 對應測試。
