-- 預約司機：乘客先把「什麼時候、從哪到哪」記下來，到點前由背景排程器轉成真訂單進派單池。
--
-- **為什麼是獨立一張表、而不是在 rides 加一個 scheduled 狀態**：
-- rides 的既有查詢散布在派單池（status=requested）、乘客 active、歷史、報表、admin 列表，
-- 多塞一個「還不該被派單」的狀態進去，得逐一稽核每一支查詢有沒有把它排除掉——
-- 漏一支就會出現「司機看到一張三天後才要出發的單」。獨立表對既有路徑是純新增、零風險。
CREATE TABLE IF NOT EXISTS scheduled_rides (
    id             BIGSERIAL PRIMARY KEY,
    customer_id    BIGINT NOT NULL REFERENCES customers(id),
    -- 乘客希望「上車」的時間。派單會比它更早發動（見 lead_time_minutes）。
    scheduled_at   TIMESTAMPTZ NOT NULL,
    pickup_point   geography(Point,4326) NOT NULL,
    pickup_address TEXT NOT NULL DEFAULT '',
    dropoff_point  geography(Point,4326),
    dropoff_address TEXT NOT NULL DEFAULT '',
    -- 與 rides.required_vehicle_type 同一組 code；'' ＝不指定。
    required_vehicle_type TEXT NOT NULL DEFAULT '',
    note           TEXT NOT NULL DEFAULT '',
    -- pending：等待到點；dispatched：已轉成 ride_id 那張真訂單；
    -- cancelled：乘客取消；failed：到點後重試多次仍建不出訂單（last_error 寫原因）。
    status         TEXT NOT NULL DEFAULT 'pending',
    ride_id        BIGINT REFERENCES rides(id),
    -- 轉單重試次數：乘客到點時可能還在別的行程上，這種情況要等下一輪再試，不是立刻失敗。
    attempt_count  INT NOT NULL DEFAULT 0,
    last_error     TEXT NOT NULL DEFAULT '',
    dispatched_at  TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT scheduled_ride_status_check
        CHECK (status IN ('pending', 'dispatched', 'cancelled', 'failed')),
    CONSTRAINT scheduled_ride_note_check CHECK (char_length(note) <= 200),
    CONSTRAINT scheduled_ride_address_check CHECK (char_length(pickup_address) <= 200),
    -- dispatched 一定要指得出是哪張訂單，否則乘客點進去看不到任何東西。
    CONSTRAINT scheduled_ride_dispatched_has_ride
        CHECK (status <> 'dispatched' OR ride_id IS NOT NULL)
);

-- 排程器每輪的掃描條件：status='pending' AND scheduled_at <= 界線。
CREATE INDEX IF NOT EXISTS idx_scheduled_ride_due
    ON scheduled_rides (status, scheduled_at);

-- 乘客端「我的預約」清單（近的在前）。
CREATE INDEX IF NOT EXISTS idx_scheduled_ride_customer
    ON scheduled_rides (customer_id, scheduled_at DESC);
