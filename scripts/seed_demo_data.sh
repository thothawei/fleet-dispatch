#!/bin/sh
# 常用地點＋預約行程的示範資料。供 App 端測試「住家／公司快速帶入」與「我的預約」。
#
# 主路徑刻意走**真實 HTTP API**（不是直接寫 DB）：這樣塞資料的同時也端到端驗過了那些端點，
# 而不是塞出一份「只有 SQL 造得出來、API 其實不接受」的資料。
# 只有 dispatched／failed 這兩種狀態非走 SQL 不可——它們是排程器到點後才會產生的結果，
# 沒有任何 API 能直接建出來，而 App 的預約清單要看得到這兩種狀態長什麼樣。
#
# 用法：
#   前提：後端已啟動（docker compose up -d，或本機 go run ./cmd/server）
#   sh scripts/seed_demo_data.sh
#
# 可覆寫：
#   API_URL     後端位址（預設 http://localhost:8080）
#   SEED_USER   示範乘客的 line_user_id（預設 demo-customer-1）
#   SEED_PASS   密碼（預設 demo123456）
#   PSQL_DSN    psql 連線字串；未設時跳過 dispatched／failed 那兩筆
set -e

API="${API_URL:-http://localhost:8080}"
USER_ID="${SEED_USER:-demo-customer-1}"
PASS="${SEED_PASS:-demo123456}"

fail() { echo "✗ $1"; exit 1; }
json_str() { echo "$1" | grep -o "\"$2\":\"[^\"]*" | cut -d'"' -f4; }
json_num() { echo "$1" | grep -o "\"$2\":[0-9]*" | head -1 | cut -d: -f2; }

echo "== 後端健康檢查 =="
curl -sf "$API/healthz" >/dev/null || fail "後端未就緒（$API）"

echo "== 示範乘客：註冊（已存在則改登入）=="
REG=$(curl -s -X POST "$API/api/customer/register" \
  -H 'Content-Type: application/json' \
  -d "{\"line_user_id\":\"$USER_ID\",\"name\":\"示範乘客\",\"password\":\"$PASS\"}")
TOKEN=$(json_str "$REG" token)
if [ -z "$TOKEN" ]; then
  LOGIN=$(curl -s -X POST "$API/api/customer/login" \
    -H 'Content-Type: application/json' \
    -d "{\"line_user_id\":\"$USER_ID\",\"password\":\"$PASS\"}")
  TOKEN=$(json_str "$LOGIN" token)
  [ -n "$TOKEN" ] || fail "註冊與登入都失敗：$REG / $LOGIN"
  echo "（帳號已存在，改用登入）"
fi
CUSTOMER_ID=$(json_num "$REG" customer_id)
[ -n "$CUSTOMER_ID" ] || CUSTOMER_ID=$(json_num "$LOGIN" customer_id)
echo "customer_id=$CUSTOMER_ID"

AUTH="Authorization: Bearer $TOKEN"

# ---------- 常用地點 ----------
# home／work 是覆蓋語意，這支腳本因此可以重複執行而不會越跑越多筆。
place() {
  RES=$(curl -s -X POST "$API/api/customer/places" \
    -H "$AUTH" -H 'Content-Type: application/json' \
    -d "{\"kind\":\"$1\",\"label\":\"$2\",\"address\":\"$3\",\"lat\":$4,\"lng\":$5}")
  ID=$(json_num "$RES" id)
  [ -n "$ID" ] || fail "建立地點失敗（$2）：$RES"
  echo "  ✓ $2（id=$ID）"
}

echo "== 常用地點 =="
place home   "住家"     "台北市大安區和平東路二段106號" 25.0261 121.5435
place work   "公司"     "台北市內湖區瑞光路513巷"       25.0797 121.5750
place custom "健身房"   "台北市信義區松高路12號"        25.0400 121.5680
place custom "媽媽家"   "新北市板橋區文化路一段188號"    25.0210 121.4700

echo "== 常用地點現況 =="
curl -s -H "$AUTH" "$API/api/customer/places" | head -c 400; echo

# ---------- 預約行程（pending，走 API）----------
# 時間一律用「相對現在」算，腳本才不會過幾天就因為時間變成過去而全部被擋下來。
iso_in() {
  # $1 = 幾分鐘後。
  # BSD date（macOS）走 -v，GNU date（Linux）走 -d。
  # **只在 macOS 上實跑過**，GNU 那條退路是照文件寫的、未實測。
  if date -u -v+"$1"M '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null; then :; else
    date -u -d "+$1 minutes" '+%Y-%m-%dT%H:%M:%SZ'
  fi
}

schedule() {
  AT=$(iso_in "$1")
  RES=$(curl -s -X POST "$API/api/customer/scheduled-rides" \
    -H "$AUTH" -H 'Content-Type: application/json' \
    -d "{\"scheduled_at\":\"$AT\",\"pickup_lat\":$2,\"pickup_lng\":$3,\"pickup_address\":\"$4\",\"dropoff_lat\":$5,\"dropoff_lng\":$6,\"dropoff_address\":\"$7\",\"required_vehicle_type\":\"$8\",\"note\":\"$9\"}")
  ID=$(json_num "$RES" id)
  [ -n "$ID" ] || fail "建立預約失敗（$4 → $7）：$RES"
  echo "  ✓ $AT｜$4 → $7（id=$ID）"
  LAST_SCHEDULE_ID="$ID"
}

echo "== 預約行程（pending）=="
# 90 分鐘後：住家 → 公司，不指定車種
schedule 90   25.0261 121.5435 "台北市大安區和平東路二段106號" 25.0797 121.5750 "台北市內湖區瑞光路513巷" "" "明早上班"
# 明天同時間：公司 → 住家
schedule 1500 25.0797 121.5750 "台北市內湖區瑞光路513巷" 25.0261 121.5435 "台北市大安區和平東路二段106號" "" "下班回家"
# 三天後：帶寵物（指定寵物車，驗清潔費那條路徑）
schedule 4320 25.0261 121.5435 "台北市大安區和平東路二段106號" 25.0330 121.5654 "台北市信義區市府路1號" "pet" "帶柴犬看醫生"
# 一週後：要拿來示範「已取消」
schedule 10080 25.0400 121.5680 "台北市信義區松高路12號" 25.0210 121.4700 "新北市板橋區文化路一段188號" "" "週末回娘家"
TO_CANCEL="$LAST_SCHEDULE_ID"

echo "== 取消其中一筆（示範 cancelled 狀態）=="
curl -sf -X POST "$API/api/customer/scheduled-rides/$TO_CANCEL/cancel" -H "$AUTH" >/dev/null \
  && echo "  ✓ 已取消 id=$TO_CANCEL"

# ---------- dispatched／failed（只能走 SQL）----------
if [ -z "$PSQL_DSN" ]; then
  echo
  echo "== 略過 dispatched／failed 兩筆 =="
  echo "   這兩種狀態是排程器到點後才產生的，沒有 API 可以直接建。"
  echo "   要一併塞的話設 PSQL_DSN 再跑一次，例如："
  echo "   PSQL_DSN='postgres://fleet:change_me@127.0.0.1:5433/fleet?sslmode=disable' sh scripts/seed_demo_data.sh"
else
  echo
  echo "== dispatched／failed（走 SQL）=="
  psql "$PSQL_DSN" -v ON_ERROR_STOP=1 -q <<SQL
-- 已轉單：造一張真訂單再把預約指過去，狀態機才對得起來
-- （scheduled_ride_dispatched_has_ride 這條 CHECK 會擋掉沒有 ride_id 的 dispatched）。
WITH new_ride AS (
  INSERT INTO rides (customer_id, status, pickup_point, pickup_address,
                     dropoff_point, dropoff_address, requested_at, created_at, updated_at)
  VALUES ($CUSTOMER_ID, 0,
          ST_SetSRID(ST_MakePoint(121.5435, 25.0261), 4326)::geography,
          '台北市大安區和平東路二段106號',
          ST_SetSRID(ST_MakePoint(121.5750, 25.0797), 4326)::geography,
          '台北市內湖區瑞光路513巷',
          NOW(), NOW(), NOW())
  RETURNING id
)
INSERT INTO scheduled_rides (customer_id, scheduled_at, pickup_point, pickup_address,
                             dropoff_point, dropoff_address, note, status, ride_id,
                             attempt_count, dispatched_at, created_at, updated_at)
SELECT $CUSTOMER_ID, NOW() + INTERVAL '10 minutes',
       ST_SetSRID(ST_MakePoint(121.5435, 25.0261), 4326)::geography,
       '台北市大安區和平東路二段106號',
       ST_SetSRID(ST_MakePoint(121.5750, 25.0797), 4326)::geography,
       '台北市內湖區瑞光路513巷',
       '已轉為訂單，司機正在指派中', 'dispatched', new_ride.id,
       1, NOW(), NOW(), NOW()
FROM new_ride;

-- 重試用盡：讓 App 看得到 last_error 要怎麼呈現給乘客。
INSERT INTO scheduled_rides (customer_id, scheduled_at, pickup_point, pickup_address,
                             dropoff_point, dropoff_address, note, status,
                             attempt_count, last_error, created_at, updated_at)
VALUES ($CUSTOMER_ID, NOW() - INTERVAL '2 hours',
        ST_SetSRID(ST_MakePoint(121.5680, 25.0400), 4326)::geography,
        '台北市信義區松高路12號',
        ST_SetSRID(ST_MakePoint(121.4700, 25.0210), 4326)::geography,
        '新北市板橋區文化路一段188號',
        '示範：到點時一直有進行中訂單', 'failed',
        10, '已有進行中的訂單', NOW(), NOW());
SQL
  echo "  ✓ 已補上 dispatched／failed 各一筆"
fi

echo
echo "== 預約現況 =="
curl -s -H "$AUTH" "$API/api/customer/scheduled-rides" | head -c 800; echo
echo
echo "✓ 完成。App 端以 $USER_ID / $PASS 登入即可看到這些資料。"
