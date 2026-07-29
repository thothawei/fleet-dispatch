#!/bin/sh
# 取消／重新派車那三條路徑的實跑驗證（承 push_e2e.sh）：
#   1. 乘客自己取消 → **不該有推播**（他就在前景，剛按完）
#   2. admin 取消    → 應推 ride.cancelled 給乘客
#   3. 司機放棄      → 應推 ride.redispatched 給乘客
set -e
API="${API_URL:-http://localhost:8080}"
SUFFIX="${SUFFIX:-$(date +%s)}"
PLATE="PC-$(echo "$SUFFIX" | tail -c 5)"

fail() { echo "✗ $1"; exit 1; }
jsonval() { grep -o "\"$2\":[^,}]*" | head -1 | cut -d: -f2- | tr -d '" '; }

# --- 準備司機（已核准車輛、上線）與乘客 ---
curl -sf -X POST "$API/api/driver/register" -H 'Content-Type: application/json' \
  -d "{\"line_user_id\":\"cx-d-$SUFFIX\",\"name\":\"取消測試司機\",\"password\":\"pw123456\"}" >/dev/null
DLOGIN=$(curl -sf -X POST "$API/api/driver/login" -H 'Content-Type: application/json' \
  -d "{\"line_user_id\":\"cx-d-$SUFFIX\",\"password\":\"pw123456\"}")
DTOKEN=$(echo "$DLOGIN" | jsonval x token); DID=$(echo "$DLOGIN" | jsonval x driver_id)
curl -sf -X POST "$API/api/driver/device-token" -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $DTOKEN" -d '{"platform":"fcm","token":"DRIVER-FCM-TOKEN"}' >/dev/null
curl -sf -X PUT "$API/api/driver/vehicle" -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $DTOKEN" -d "{\"vehicle_type\":\"sedan\",\"plate_number\":\"$PLATE\"}" >/dev/null
ATOKEN=$(curl -sf -X POST "$API/api/admin/login" -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin"}' | jsonval x token)
curl -sf -X POST "$API/api/admin/drivers/$DID/vehicle-review" -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $ATOKEN" -d '{"approve":true}' >/dev/null
curl -sf -X POST "$API/api/driver/online" -H "Authorization: Bearer $DTOKEN" >/dev/null
curl -sf -X POST "$API/api/driver/location" -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $DTOKEN" -d '{"lat":25.0330,"lng":121.5650}' >/dev/null

curl -sf -X POST "$API/api/customer/register" -H 'Content-Type: application/json' \
  -d "{\"line_user_id\":\"cx-c-$SUFFIX\",\"name\":\"取消測試乘客\",\"password\":\"pw123456\"}" >/dev/null
CTOKEN=$(curl -sf -X POST "$API/api/customer/login" -H 'Content-Type: application/json' \
  -d "{\"line_user_id\":\"cx-c-$SUFFIX\",\"password\":\"pw123456\"}" | jsonval x token)
curl -sf -X POST "$API/api/customer/device-token" -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $CTOKEN" -d '{"platform":"fcm","token":"CUSTOMER-FCM-TOKEN"}' >/dev/null

newride() {
  curl -sf -X POST "$API/api/rides" -H 'Content-Type: application/json' -H "Authorization: Bearer $CTOKEN" \
    -d '{"pickup_lat":25.0335,"pickup_lng":121.5655,"pickup_address":"台北101","dropoff_lat":25.0478,"dropoff_lng":121.5170,"dropoff_address":"台北車站"}' \
    | jsonval x ride_id
}

echo "== 1) 乘客自己取消（預期：沒有推播）=="
R1=$(newride); sleep 2
curl -sf -X POST "$API/api/rides/$R1/cancel-by-customer" -H "Authorization: Bearer $CTOKEN" >/dev/null
echo "ride_id=$R1 已由乘客取消"

echo "== 2) admin 取消（預期：推 ride.cancelled 給乘客）=="
R2=$(newride); sleep 2
curl -sf -X POST "$API/api/admin/rides/$R2/cancel" -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $ATOKEN" -d '{"note":"客服取消"}' >/dev/null
echo "ride_id=$R2 已由 admin 取消"

echo "== 3) 司機接單後放棄（預期：推 ride.redispatched 給乘客）=="
R3=$(newride); sleep 2
curl -sf -X POST "$API/api/rides/$R3/accept" -H "Authorization: Bearer $DTOKEN" >/dev/null
curl -sf -X POST "$API/api/rides/$R3/cancel" -H "Authorization: Bearer $DTOKEN" >/dev/null
echo "ride_id=$R3 已由司機放棄"

echo
echo "✓ 三種取消路徑跑完：R1=$R1（乘客自撤）R2=$R2（admin）R3=$R3（司機放棄）"
