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
