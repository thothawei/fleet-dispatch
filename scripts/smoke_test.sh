#!/bin/sh
# M1-M4 端到端煙霧測試。
# 前提：docker compose 已啟動、且為乾淨資料庫（建議先 docker compose down -v）。
# 請勿在 simulator 同時運行時執行，避免搶單造成干擾。
set -e
API="${API_URL:-http://localhost:8080}"

fail() { echo "✗ $1"; exit 1; }

echo "== healthz =="
curl -sf "$API/healthz" | grep -q '"status":"ok"' || fail "healthz 未就緒"

echo "== 註冊司機 =="
DRIVER=$(curl -sf -X POST "$API/api/driver/register" \
  -H 'Content-Type: application/json' \
  -d '{"line_user_id":"smoke-driver-1","name":"煙霧測試司機"}')
DRIVER_ID=$(echo "$DRIVER" | grep -o '"driver_id":[0-9]*' | cut -d: -f2)
[ -n "$DRIVER_ID" ] || fail "註冊司機失敗: $DRIVER"
echo "driver_id=$DRIVER_ID"

echo "== 司機回報位置（台北 101 附近）=="
curl -sf -X POST "$API/api/driver/location" \
  -H 'Content-Type: application/json' \
  -d "{\"driver_id\":$DRIVER_ID,\"lat\":25.033,\"lng\":121.565}" >/dev/null

echo "== 客戶 LINE 叫車 =="
RIDE=$(curl -sf -X POST "$API/webhook/line" \
  -H 'Content-Type: application/json' \
  -d "{\"events\":[{\"type\":\"message\",\"replyToken\":\"test\",\"source\":{\"userId\":\"smoke-customer-1\",\"type\":\"user\"},\"message\":{\"type\":\"location\",\"latitude\":25.034,\"longitude\":121.566,\"address\":\"台北101\"}}]}")
RIDE_ID=$(echo "$RIDE" | grep -o '"ride_ids":\[[0-9]*' | grep -o '[0-9]*$')
[ -n "$RIDE_ID" ] || fail "建立訂單失敗: $RIDE"
echo "ride_id=$RIDE_ID"

sleep 2  # 等非同步派單將訂單置為 Assigned

echo "== 司機接單（斷言真的成功）=="
ACCEPT=$(curl -sf -X POST "$API/api/rides/$RIDE_ID/accept" \
  -H 'Content-Type: application/json' \
  -d "{\"driver_id\":$DRIVER_ID}")
echo "$ACCEPT" | grep -q '接單成功' || fail "接單未成功: $ACCEPT"

echo "== 客戶上車 =="
curl -sf -X POST "$API/api/rides/$RIDE_ID/pickup" \
  -H 'Content-Type: application/json' \
  -d "{\"driver_id\":$DRIVER_ID}" >/dev/null

echo "== 行程中回報軌跡 =="
curl -sf -X POST "$API/api/driver/location" \
  -H 'Content-Type: application/json' \
  -d "{\"driver_id\":$DRIVER_ID,\"lat\":25.035,\"lng\":121.567}" >/dev/null

echo "== 完成行程 =="
curl -sf -X POST "$API/api/rides/$RIDE_ID/complete" \
  -H 'Content-Type: application/json' \
  -d "{\"driver_id\":$DRIVER_ID}" >/dev/null

echo "== 軌跡回放（GeoJSON Feature）=="
curl -sf "$API/api/rides/$RIDE_ID/track" | grep -q '"type":"Feature"' || fail "軌跡回放格式錯誤"

echo "== 日報表（斷言含本次司機）=="
REPORT=$(curl -sf "$API/api/reports/daily?date=$(date +%Y-%m-%d)")
echo "$REPORT" | grep -q "\"driver_id\":$DRIVER_ID" || fail "日報表未含本次司機: $REPORT"

echo ""
echo "全部煙霧測試通過 ✓ (ride_id=$RIDE_ID, driver_id=$DRIVER_ID)"
