# 技術決策紀錄

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
