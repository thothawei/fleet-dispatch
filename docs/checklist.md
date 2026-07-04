# 動工前準備清單（Pre-flight Checklist）

> 額度重置後開工前，把這份清單勾完，能讓 M1~M4 一路不卡在「等憑證/等圖資/等環境」。
> 打勾方式：完成後把 `[ ]` 改成 `[x]`。
> 對應建置規格：見 [`spec.md`](spec.md)。

---

## A. 本機開發環境（一次性）

- [ ] 安裝 Docker Desktop，且 `docker compose version` 可正常執行
- [ ] 安裝 Go 1.23+（`go version` 確認）
- [ ] 安裝 golang-migrate CLI（`migrate -version`）
- [ ] 安裝 sqlc（`sqlc version`）
- [ ] （選）安裝 `make`，方便用 Makefile 跑常用指令
- [ ] 準備一個可對外的 HTTPS 通道給 LINE webhook：ngrok 或 cloudflared 擇一
  - 免費 ngrok：`ngrok http 8080` → 拿到 `https://xxxx.ngrok.io`

---

## B. LINE 服務（M1 就會用到，務必先備）

- [ ] 到 [LINE Developers Console](https://developers.line.biz/) 登入、建立一個 Provider
- [ ] 建立 **Messaging API channel**
- [ ] 記下 **Channel secret**（填入 `.env` 的 `LINE_CHANNEL_SECRET`）
- [ ] 發行並記下 **Channel access token**（填入 `LINE_CHANNEL_ACCESS_TOKEN`）
- [ ] 關閉「自動回覆訊息」、開啟「Webhook」
- [ ] 設定 Webhook URL 為 `https://<你的 ngrok 網域>/webhook/line`（動工起服務後再填）
- [ ] 建立一個 **LIFF app**（司機定位頁用），Endpoint URL 先隨意填、拿到 **LIFF ID**（填 `LIFF_ID`）
- [ ] 用手機把這個官方帳號加為好友，準備當測試客戶
- [ ] （選）另一支手機或帳號當測試司機

> 額度限制提醒：LINE 主動 push 有月額度、reply token 回覆免費——demo 夠用，設計上盡量用 reply。

---

## C. OSRM 路徑引擎圖資（M3 才需要，但預處理耗時可提早做）

- [ ] 下載台灣圖資：`wget http://download.geofabrik.de/asia/taiwan-latest.osm.pbf`
- [ ] 預處理（三步，會跑一段時間）：
  ```bash
  docker run -t -v "$PWD:/data" osrm/osrm-backend osrm-extract -p /opt/car.lua /data/taiwan-latest.osm.pbf
  docker run -t -v "$PWD:/data" osrm/osrm-backend osrm-partition /data/taiwan-latest.osrm
  docker run -t -v "$PWD:/data" osrm/osrm-backend osrm-customize /data/taiwan-latest.osrm
  ```
- [ ] 確認產出 `taiwan-latest.osrm*` 一系列檔案（compose 的 osrm 服務會掛這個目錄）

---

## D. Google Maps 導航（免準備）

- [x] 不需要 API key、不需要付費——用 deep link URL（`https://www.google.com/maps/dir/?api=1&...`）即可。此項已確認無須準備。

---

## E. 專案初始化（開工第一批動作，執行者會做，可先確認）

- [ ] 確認要放的路徑：`/Users/mac/Documents/line-fleet-dispatch/`
- [ ] `git init`（目前只有 docs/，尚未 init）
- [ ] `go mod init line-fleet-dispatch`
- [ ] 依 spec 第 8 節建立 `.env`（複製 `.env.example` 填入 B 的真實值）

---

## F. 決策確認（開工前使用者拍板，避免中途改方向）

- [ ] 前台只做 LINE Bot，暫不做獨立 App/網站？（預設：是）
- [ ] ETA 用自架 OSRM（免費、無即時路況）而非 Google 付費 API？（預設：OSRM）
- [ ] 儀表板用 Grafana 接 PostGIS，還是自寫 WebSocket 頁？（預設：M4 先做 Grafana，行有餘力再自寫）
- [ ] 司機定位先用「模擬器」驗證完整管線，真手機 LIFF 當加分？（預設：是）

---

## 最低啟動門檻（想先跑起來看到東西的話）

只要 **A 全部 + B 的 channel secret/access token/LIFF ID** 備好，就能開工 M1（LINE 收單建單）。
C（OSRM）可以等做到 M3 再補，不擋 M1/M2。
