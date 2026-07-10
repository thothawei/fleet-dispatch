# 技術決策紀錄

## 2026-07-06 · 後台改帳號登入 + 超級帳號種子

- **admin 登入由 email 改為帳號（username）**：migration 000007 將 `admins.email` 改名 `username`（沿用原 UNIQUE）；
  model/repository/service/handler/config 全數 email→username。登入 body 改 `{username,password}`。
- **超級帳號種子 admin/admin**：docker-compose `environment` 帶 `ADMIN_SEED_USERNAME/PASSWORD` 預設 admin/admin
  （`EnsureSeed` 僅在尚無任何 admin 時建立一次）。⚠️ 踩雷：專案 `.env`（env_file）若殘留舊 `ADMIN_SEED_PASSWORD`
  會被 compose `${VAR:-admin}` 插值取用而**覆蓋預設**，導致實際密碼非 admin——已清 .env 對齊。
- 目前後台單一 admin 角色即全權限（尚無 RBAC）。

## 2026-07-03 · M1 骨架

- **分層**：handler → service → repository，對照 Laravel 慣例。
- **Migration**：server 啟動時自動 `Up()`，含 30 次重試等 postgis 就緒。
- **PostGIS 座標**：`ST_MakePoint` + raw SQL 寫入 geography。
- **LINE 簽章**：未設定 secret 時跳過，方便本地 curl 測試。

## 2026-07-03 · M2 派單

- **Redis GEO**：`drivers:geo` + hash 時間戳過濾 60s 離線司機。
- **搶單鎖**：`SETNX ride:{id}:lock` TTL 30s。
- **模擬器**：獨立 `cmd/simulator`，docker compose profile 可選啟動。
- **派單觸發**：叫車後 goroutine 非同步 Dispatch，不阻塞 webhook 回覆。

## 2026-07-03 · M3 ETA

- **OSRM**：優先 `OSRM_URL`；失敗 fallback 直線距離 × 1.4。
- **預設 OSRM**：compose 預設 `router.project-osrm.org`，免下載台灣圖資即可 demo。
- **Google Maps**：deep link 嵌入司機接單訊息。

## 2026-07-03 · M4 軌跡與報表

- **ride_tracks**：PostgreSQL 宣告式分區（2026-07、2026-08）。
- **圍籬**：`ST_DWithin` 100m 觸發「司機已抵達」。
- **報表**：window function 彙總每日趟數、里程、平均接客時間。

## 2026-07-06 · M5-WS WebSocket 即時通道

- **選 gorilla/websocket**：Go 生態最成熟穩定的 WS 函式庫；作品階段夠用。
- **Hub 單 goroutine 序列化**：register/unregister/publish 全走 channel 進單一迴圈，
  免鎖、無 map 競態（`-race` 驗證通過）。慢客戶端佇列滿則丟該則事件，不阻塞 Hub。
- **事件發佈與 LINE 推播並存**：在既有 `line.Push*` 呼叫旁「多發一份」結構化事件，
  派單/訂單業務邏輯零改動；未接 Hub 時 Publisher 為 nil，服務照常運作（既有測試不回歸）。
- **通用角色 JWT**：新增 `SubjectClaims{Role,SubjectID}` 同時支援 driver/customer/admin，
  並相容既有司機 `DriverClaims` token（`ParseToken` 退回機制）。
- **邊界**：WS 目前僅單向下推（伺服器 → 前端）；上行（如司機定位）仍走既有 REST。
  乘客/後台 WS 訂閱需各自的認證（乘客認證、admin 認證為 M5 後續 sub-plan）。

## 2026-07-06 · M5-ADMIN 後台認證與唯讀 API

- **admin 認證**：新 `admins` 表 + bcrypt 密碼，沿用通用角色 JWT（role=admin）。
- **不開放公開註冊**：管理員由啟動時環境變數 `ADMIN_SEED_EMAIL/PASSWORD` 種子建立，
  且僅在系統尚無任何 admin 時建立一次。
- **唯讀優先**：本階段只做讀取端點（車隊/司機/訂單/軌跡/報表）。司機停用等寫入操作
  刻意延後——真正的停用需派單邏輯配合，否則是假功能。
- **車隊快照**：`OnlineDriverLocations` 讀 `drivers:geo` zset 全體再依 updated_at 過濾離線；
  即時更新由已完成的 WS `driver.location`（admin 廣播）承擔。
- **測試 helper**：新增 `newMigratedTestDB`（對 testcontainer 跑真 db/migrations，得完整 schema），
  取代既有 `newTestDB`（只建最小 rides 表）供需要完整 schema 的整合測試使用。

## 2026-07-06 · M5-CUSTOMER-AUTH 乘客認證

- **line_user_id + 密碼 JWT**：鏡射司機（DriverRegistry）；乘客 token 帶 `customers.id`，
  與 dispatch/tracking 發佈的 `Recipient{RoleCustomer, ride.CustomerID}` 對齊，
  登入後即可用該 token 連 WS 收自己這趟的事件（WS handler 已支援 role=customer，無需改）。
- **前向相容 LINE Login**：身分鍵仍是 line_user_id；日後接 LINE Login 只換驗證方式，
  不動身分模型（此為設計 §12.4「可改決策」的踏腳石實作）。
- **測試**：CustomerRegistry 用假 store 做純邏輯單元測試；repo 層用 newMigratedTestDB。

## 2026-07-10 · dropoff 座標鏈路修復（P1 尾巴）

- **踩坑：commit message 不等於 diff**。`21e031d` 的訊息列了 5 項改動（RideService/RideRepository 參數、
  派單事件、單元＋整合＋煙霧測試），但實際只提交了 `line_webhook.go` 與 `ride_dropoff_test.go`。
  `service.RideRequest` 從未新增 `DropoffLat/Lng/Address`，於是 **main 從 21e031d 起連續三個 commit `go build ./...` 失敗**，
  而且壞的版本已推上 origin。`ride_dropoff_test.go` 對著不存在的 API 撰寫（`NewDispatchService` 11 參數、
  `CreateByCustomer` 位置參數版），從未編譯過——「測試已通過」的宣稱是假的。
  go-ci 其實兩次都轉紅（run `29082655288`／`29082686314`），但被無視、照樣 push。
  → **教訓：commit 前必跑 `go build ./... && go vet ./internal/...`；聲稱跑過的測試要貼實際輸出。
  CI 抓得到不代表擋得住——main 需要 branch protection。**
  → ✅ 2026-07-10 已補上：三個 repo 的 main 都開了 branch protection（`enforce_admins: true`，
  owner 也擋）。實測直推會被 `protected branch hook declined` 拒絕。詳見 STATUS.md「Git 工作流」。
- **LINE 叫車不塞預設目的地**：webhook 只收得到位置訊息（＝上車點），流程中沒有目的地輸入來源。
  `21e031d` 硬編「台北 101」當預設 dropoff，會讓每張 LINE 訂單的司機上車後導航到 101。已移除；
  LINE 訂單 dropoff 維持 NULL（設計取捨），帶目的地的訂單來自乘客 App 的 `POST /api/rides`。
- **座標優先於地址**：`PickUp` 改回傳 `PickUpResult{DropoffAddress, DropoffLat/Lng, HasDropoffPoint}`，
  `ride.assigned` / `ride.accepted` 共用 `putDropoff()` 帶同一組欄位。地址字串在 Google Maps 上可能解析到
  同名的錯誤地點，`dropoff_point` 才是原始資料；司機端有座標時一律用座標導航。
- **`HasDropoffPoint` 而非零值判斷**：(0,0) 是合法座標，不能拿零值當「未指定」。
