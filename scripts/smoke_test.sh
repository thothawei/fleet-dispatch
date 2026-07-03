#!/bin/sh
# M1-M4 煙霧測試（需 docker compose 已啟動）
set -e
API="${API_URL:-http://localhost:8080}"

echo "== healthz =="
curl -sf "$API/healthz" | grep -q '"status":"ok"'

echo "== 註冊模擬司機 =="
DRIVER=$(curl -sf -X POST "$API/api/driver/register" \
  -H 'Content-Type: application/json' \
  -d '{"line_user_id":"smoke-driver-1","name":"煙霧測試司機"}')
DRIVER_ID=$(echo "$DRIVER" | grep -o '"driver_id":[0-9]*' | cut -d: -f2)
echo "driver_id=$DRIVER_ID"

echo "== 回報司機位置 =="
curl -sf -X POST "$API/api/driver/location" \
  -H 'Content-Type: application/json' \
  -d "{\"driver_id\":$DRIVER_ID,\"lat\":25.033,\"lng\":121.565}"

echo "== 模擬 LINE 叫車 webhook =="
curl -sf -X POST "$API/webhook/line" \
  -H 'Content-Type: application/json' \
  -d "{\"events\":[{\"type\":\"message\",\"replyToken\":\"test\",\"source\":{\"userId\":\"smoke-customer-1\",\"type\":\"user\"},\"message\":{\"type\":\"location\",\"latitude\":25.034,\"longitude\":121.566,\"address\":\"台北101\"}}]}"

sleep 2

echo "== 司機接單 =="
# ride_id 假設為 1（首次叫車）
curl -sf -X POST "$API/api/rides/1/accept" \
  -H 'Content-Type: application/json' \
  -d "{\"driver_id\":$DRIVER_ID}"

echo "== 客戶上車 =="
curl -sf -X POST "$API/api/rides/1/pickup" \
  -H 'Content-Type: application/json' \
  -d "{\"driver_id\":$DRIVER_ID}"

echo "== 回報軌跡 =="
curl -sf -X POST "$API/api/driver/location" \
  -H 'Content-Type: application/json' \
  -d "{\"driver_id\":$DRIVER_ID,\"lat\":25.035,\"lng\":121.567}"

echo "== 完成行程 =="
curl -sf -X POST "$API/api/rides/1/complete" \
  -H 'Content-Type: application/json' \
  -d "{\"driver_id\":$DRIVER_ID}"

echo "== 軌跡回放 =="
curl -sf "$API/api/rides/1/track" | grep -q '"type":"Feature"'

echo "== 日報表 =="
curl -sf "$API/api/reports/daily?date=$(date +%Y-%m-%d)"

echo ""
echo "全部煙霧測試通過 ✓"
