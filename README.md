# LINE 叫車派遣系統

Go + PostgreSQL(PostGIS) + Redis + LINE Bot 的叫車派遣學習專案。詳細規格見 [docs/spec.md](docs/spec.md)。

## 目前進度

- [x] **M1** — LINE Bot 收單、migration、webhook 簽章、healthz
- [x] **M2** — Redis GEO 派單、搶單鎖、司機模擬器、LIFF 定位頁
- [x] **M3** — OSRM ETA（預設公開 router，可改自架）、Google Maps 導航連結
- [x] **M4** — PostGIS 圍籬、軌跡分區、GeoJSON 回放、日報表 API

## 快速開始

```bash
cp .env.example .env
# 填入 LINE_CHANNEL_SECRET、LINE_CHANNEL_ACCESS_TOKEN（選填，未填可跑 API 測試）

docker compose up --build -d
curl http://localhost:8080/healthz

# 煙霧測試（完整流程）
sh scripts/smoke_test.sh

# 啟動 20 台模擬司機
docker compose --profile simulator up -d simulator
```

## API 端點

| 方法 | 路徑 | 說明 |
|------|------|------|
| GET | `/healthz` | 健康檢查 |
| POST | `/webhook/line` | LINE webhook |
| POST | `/api/driver/register` | 註冊司機 |
| POST | `/api/driver/location` | 司機回報位置 |
| POST | `/api/rides/:id/accept` | 接單 |
| POST | `/api/rides/:id/pickup` | 客戶上車 |
| POST | `/api/rides/:id/complete` | 完成行程 |
| GET | `/api/rides/:id/track` | 軌跡 GeoJSON |
| GET | `/api/reports/daily?date=YYYY-MM-DD` | 日報表 |
| GET | `/liff/` | 司機 LIFF 定位頁 |

## 已知限制

1. **LIFF 背景定位**：網頁 `watchPosition` 需頁面在前景；導航中位置更新會變慢。
2. **LINE push 月額度**：設計上盡量用 reply token；未設定 token 時 push 略過。
3. **OSRM**：預設用 `router.project-osrm.org` demo；自架請改 `OSRM_URL` 並參考 spec §4.2。

## 技術棧

Go 1.25 · Gin · GORM · golang-migrate · PostgreSQL 16 + PostGIS · Redis 7 · LINE Messaging API
