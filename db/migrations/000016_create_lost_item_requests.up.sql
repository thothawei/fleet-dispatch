-- 遺失物協尋：乘客對「已完成行程」建立協尋單，聯絡司機找回物品。
-- 處理費＝該趟車資 × lost_item_fee_bps（於建立當下定格快照，與車資/手續費同一套快照制）。
ALTER TABLE fleet_settings
    ADD COLUMN IF NOT EXISTS lost_item_fee_bps INT NOT NULL DEFAULT 1000
        CHECK (lost_item_fee_bps >= 0 AND lost_item_fee_bps <= 10000);

CREATE TABLE IF NOT EXISTS lost_item_requests (
    id          BIGSERIAL PRIMARY KEY,
    ride_id     BIGINT NOT NULL REFERENCES rides(id),
    customer_id BIGINT NOT NULL REFERENCES customers(id),
    driver_id   BIGINT NOT NULL REFERENCES drivers(id),
    description TEXT NOT NULL,
    fee_cents   BIGINT NOT NULL, -- 建立當下快照：round(車資 × fee_bps / 10000)
    fee_bps     INT NOT NULL,    -- 快照留痕（審計：當時的處理費%）
    status      TEXT NOT NULL DEFAULT 'open', -- open→found→paid→returned；open/found 可 closed
    paid_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT lost_item_status_check CHECK (status IN ('open', 'found', 'paid', 'returned', 'closed')),
    CONSTRAINT lost_item_fee_check CHECK (fee_cents >= 0 AND fee_bps >= 0 AND fee_bps <= 10000),
    CONSTRAINT lost_item_desc_check CHECK (char_length(description) BETWEEN 1 AND 500)
);

-- 一趟行程同時間只能有一張「未結案」協尋單（returned/closed 結案後可再開新單）。
CREATE UNIQUE INDEX IF NOT EXISTS uq_lost_item_ride_active
    ON lost_item_requests (ride_id) WHERE status NOT IN ('returned', 'closed');

-- 司機端「待處理協尋」列表。
CREATE INDEX IF NOT EXISTS idx_lost_item_driver_status ON lost_item_requests (driver_id, status);
-- 乘客端「我的協尋」列表。
CREATE INDEX IF NOT EXISTS idx_lost_item_customer ON lost_item_requests (customer_id, id);
