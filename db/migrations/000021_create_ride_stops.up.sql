-- 多乘客／多停靠點行程（N1）：一張訂單裡多位同行乘客各自的上／下車點，依序停靠。
-- **不是陌生人拼車**——同一張訂單、不需配對演算法、不需跨訂單分帳。
--
-- 相容性（N1／N3 明確要求）：既有單點訂單**不會有任何 ride_stops 列**。
-- rides.pickup_point / dropoff_point 仍是派單、地圖、報表的讀取來源，
-- 多停靠點訂單也照樣寫（第一個 pickup／最終 dropoff），既有路徑一行都不用改。
-- ride_stops 為空 ＝ 舊行為（含 LINE 建的無目的地訂單）。
CREATE TABLE IF NOT EXISTS ride_stops (
    id              BIGSERIAL PRIMARY KEY,
    ride_id         BIGINT NOT NULL REFERENCES rides(id) ON DELETE CASCADE,
    seq             INT    NOT NULL,
    kind            TEXT   NOT NULL,
    point           geography(Point, 4326) NOT NULL,
    address         TEXT   NOT NULL DEFAULT '',
    -- passenger_label 給司機辨識用（A/B/C…）。刻意不做值域 CHECK：
    -- 它不參與任何邏輯判斷，值髒掉不會算錯錢（不像車種是計費依據）。
    passenger_label TEXT   NOT NULL DEFAULT '',
    -- 停靠點處理狀態（N7）：兩個時間欄天然互斥，同時回答「是否發生」與「何時」，
    -- 不必再維護一個狀態機。兩者皆 NULL ＝ 尚未處理。
    -- skipped_at：乘客沒出現、司機標記跳過（2026-07-17 拍板）。
    -- **已跳過的停靠點不計入 N5 的計費路線**——沒去就沒開那段路。
    arrived_at      TIMESTAMPTZ,
    skipped_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_ride_stops_seq  CHECK (seq >= 1),
    CONSTRAINT chk_ride_stops_kind CHECK (kind IN ('pickup', 'dropoff')),
    -- 同一趟不得有重複順序——seq 是「第幾站」，重複的話「下一站是誰」就沒有答案。
    CONSTRAINT uq_ride_stops_ride_seq UNIQUE (ride_id, seq)
);

-- 依序讀取整趟停靠點（司機端行程卡、N5 計費路線）都是 (ride_id, seq) 範圍掃描。
-- UNIQUE 已隱含建立此索引，故不另建。
