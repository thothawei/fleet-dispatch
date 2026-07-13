-- F9-3 預聚合彙總表：每司機每日的計費彙總，供月報表/司機收入避免每次即時 GROUP BY 全表 rides。
-- rides 仍是稽核真源；本表為「可重算」的讀取最佳化（完成行程時重算該 (司機,日) 桶，冪等）。
-- day 以 Asia/Taipei 日界計算，與報表的月界（app 連線 TimeZone=Asia/Taipei）一致。
CREATE TABLE IF NOT EXISTS daily_driver_earnings (
    driver_id        BIGINT NOT NULL REFERENCES drivers(id),
    day              DATE   NOT NULL,
    trip_count       INT    NOT NULL DEFAULT 0,
    revenue_cents    BIGINT NOT NULL DEFAULT 0,
    commission_cents BIGINT NOT NULL DEFAULT 0,
    net_cents        BIGINT NOT NULL DEFAULT 0,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (driver_id, day)
);

-- 跨司機的月彙總（月報表）依 day 篩選走此索引。
CREATE INDEX IF NOT EXISTS idx_daily_driver_earnings_day ON daily_driver_earnings (day);

-- 回填：自既有已完成行程重算（與完成時的增量 rollup 同一公式、同一 Taipei 日界）。
-- status=4 為 COMPLETED；金額欄位可能為 NULL（F2 之前的舊行程），以 COALESCE 視為 0。
INSERT INTO daily_driver_earnings (driver_id, day, trip_count, revenue_cents, commission_cents, net_cents)
SELECT r.driver_id,
       (r.completed_at AT TIME ZONE 'Asia/Taipei')::date AS day,
       COUNT(*)::int,
       COALESCE(SUM(r.fare_amount_cents), 0)::bigint,
       COALESCE(SUM(r.commission_amount_cents), 0)::bigint,
       COALESCE(SUM(r.driver_net_amount_cents), 0)::bigint
FROM rides r
WHERE r.status = 4 AND r.driver_id IS NOT NULL AND r.completed_at IS NOT NULL
GROUP BY r.driver_id, (r.completed_at AT TIME ZONE 'Asia/Taipei')::date
ON CONFLICT (driver_id, day) DO NOTHING;
