#!/bin/sh
# 推播送出路徑的 docker 實跑驗證（U／U2／U3）。
#
# 沒有 Firebase 憑證時後端走 LogPusher stub，log 會印出「App 推播（stub）」與 data——
# 這足以證明**送出路徑真的被呼叫**（單元測試證明的是 service 層，這裡證明整條 HTTP 路徑）。
#
# 怎麼起後端（本機 Docker 拉不到 registry 時 `docker compose up --build` 會卡在
# load metadata for golang:1.25-alpine，改用這條）：
#   docker compose up -d postgis redis
#   DB_HOST=localhost DB_PORT=5433 DB_NAME=fleet DB_USER=fleet DB_PASSWORD=change_me \
#     REDIS_ADDR=localhost:6379 ADMIN_SEED_USERNAME=admin ADMIN_SEED_PASSWORD=admin \
#     go run ./cmd/server > /tmp/push_server.log 2>&1 &
# 跑完看證據：
#   grep 'App 推播（stub）' /tmp/push_server.log
set -e
API="${API_URL:-http://localhost:8080}"
SUFFIX="${SUFFIX:-$(date +%s)}"
PLATE="PS-$(echo "$SUFFIX" | tail -c 5)"  # 車牌有長度上限（寬鬆驗證但 <=10）

fail() { echo "✗ $1"; exit 1; }
jsonval() { grep -o "\"$2\":[^,}]*" | head -1 | cut -d: -f2- | tr -d '" '; }

echo "== healthz =="
curl -sf "$API/healthz" >/dev/null || fail "healthz 未就緒"

echo "== 司機註冊／登入 =="
curl -sf -X POST "$API/api/driver/register" -H 'Content-Type: application/json' \
  -d "{\"line_user_id\":\"push-d-$SUFFIX\",\"name\":\"推播司機\",\"password\":\"pw123456\"}" >/dev/null
DLOGIN=$(curl -sf -X POST "$API/api/driver/login" -H 'Content-Type: application/json' \
  -d "{\"line_user_id\":\"push-d-$SUFFIX\",\"password\":\"pw123456\"}")
DTOKEN=$(echo "$DLOGIN" | jsonval x token)
DID=$(echo "$DLOGIN" | jsonval x driver_id)
[ -n "$DTOKEN" ] || fail "司機登入失敗: $DLOGIN"
echo "driver_id=$DID"

echo "== 司機註冊裝置 token =="
curl -sf -X POST "$API/api/driver/device-token" -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $DTOKEN" -d '{"platform":"fcm","token":"DRIVER-FCM-TOKEN"}' >/dev/null

echo "== 司機填車輛 → admin 核准（O5 gate）=="
curl -sf -X PUT "$API/api/driver/vehicle" -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $DTOKEN" -d "{\"vehicle_type\":\"sedan\",\"plate_number\":\"$PLATE\"}" >/dev/null
ALOGIN=$(curl -sf -X POST "$API/api/admin/login" -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin"}')
ATOKEN=$(echo "$ALOGIN" | jsonval x token)
[ -n "$ATOKEN" ] || fail "admin 登入失敗: $ALOGIN"
REVIEW=$(curl -s -w '\n%{http_code}' -X POST "$API/api/admin/drivers/$DID/vehicle-review" -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $ATOKEN" -d '{"approve":true}')
echo "  vehicle-review: $(echo "$REVIEW" | tail -1)"

echo "== 司機上線＋回報位置 =="
ONLINE=$(curl -s -w '\n%{http_code}' -X POST "$API/api/driver/online" -H "Authorization: Bearer $DTOKEN")
echo "  online: $(echo "$ONLINE" | tail -1) $(echo "$ONLINE" | head -1 | head -c 120)"
curl -sf -X POST "$API/api/driver/location" -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $DTOKEN" -d '{"lat":25.0330,"lng":121.5650}' >/dev/null

echo "== 乘客註冊／登入＋裝置 token =="
curl -sf -X POST "$API/api/customer/register" -H 'Content-Type: application/json' \
  -d "{\"line_user_id\":\"push-c-$SUFFIX\",\"name\":\"推播乘客\",\"password\":\"pw123456\"}" >/dev/null
CLOGIN=$(curl -sf -X POST "$API/api/customer/login" -H 'Content-Type: application/json' \
  -d "{\"line_user_id\":\"push-c-$SUFFIX\",\"password\":\"pw123456\"}")
CTOKEN=$(echo "$CLOGIN" | jsonval x token)
[ -n "$CTOKEN" ] || fail "乘客登入失敗: $CLOGIN"
curl -sf -X POST "$API/api/customer/device-token" -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $CTOKEN" -d '{"platform":"fcm","token":"CUSTOMER-FCM-TOKEN"}' >/dev/null

echo "== 乘客叫車 =="
RIDE=$(curl -sf -X POST "$API/api/rides" -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $CTOKEN" \
  -d '{"pickup_lat":25.0335,"pickup_lng":121.5655,"pickup_address":"台北101","dropoff_lat":25.0478,"dropoff_lng":121.5170,"dropoff_address":"台北車站"}')
RID=$(echo "$RIDE" | jsonval x ride_id)
[ -n "$RID" ] || fail "建單失敗: $RIDE"
echo "ride_id=$RID"
sleep 3

echo "== 司機接單（→ 應推 ride.accepted 給乘客）=="
ACC=$(curl -s -w '\n%{http_code}' -X POST "$API/api/rides/$RID/accept" -H "Authorization: Bearer $DTOKEN")
echo "  accept: $(echo "$ACC" | tail -1) $(echo "$ACC" | head -1 | head -c 160)"

echo "== 司機開到上車點（→ 圍籬 driver.arrived 推乘客）=="
curl -sf -X POST "$API/api/driver/location" -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $DTOKEN" -d '{"lat":25.0335,"lng":121.5655}' >/dev/null
sleep 1

echo "== 對話：乘客 → 司機、司機 → 乘客（→ 各推對方一則）=="
curl -sf -X POST "$API/api/rides/$RID/messages" -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $CTOKEN" -d '{"body":"我在門口"}' >/dev/null
curl -sf -X POST "$API/api/rides/$RID/messages" -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $DTOKEN" -d '{"body":"我到了"}' >/dev/null

echo "== 上車 → 完成（→ ride.completed 推乘客）=="
curl -sf -X POST "$API/api/rides/$RID/pickup" -H "Authorization: Bearer $DTOKEN" >/dev/null
curl -sf -X POST "$API/api/driver/location" -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $DTOKEN" -d '{"lat":25.0478,"lng":121.5170}' >/dev/null
curl -sf -X POST "$API/api/rides/$RID/complete" -H "Authorization: Bearer $DTOKEN" >/dev/null

echo "== 遺失物：建單 → 尋獲 → 付款 → 歸還（→ 每步推對方）=="
ITEM=$(curl -sf -X POST "$API/api/rides/$RID/lost-items" -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $CTOKEN" -d '{"description":"黑色錢包"}')
IID=$(echo "$ITEM" | jsonval x id)
[ -n "$IID" ] || fail "建立協尋單失敗: $ITEM"
curl -sf -X POST "$API/api/lost-items/$IID/found" -H "Authorization: Bearer $DTOKEN" >/dev/null
curl -sf -X POST "$API/api/lost-items/$IID/pay" -H "Authorization: Bearer $CTOKEN" >/dev/null
curl -sf -X POST "$API/api/lost-items/$IID/return" -H "Authorization: Bearer $DTOKEN" >/dev/null

echo
echo "✓ 流程跑完。ride_id=$RID lost_item_id=$IID"
echo "接著用：docker compose logs app | grep 'App 推播（stub）' 看送出路徑是否真的被呼叫"
