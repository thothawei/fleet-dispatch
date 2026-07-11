-- 會費落帳（F8）：每月一列/司機，記錄應收月會費與繳費狀態。
-- amount_cents 於產生當下自費率設定定格（快照制），日後改月會費不影響歷史帳單。
CREATE TABLE IF NOT EXISTS membership_invoices (
    id           BIGSERIAL PRIMARY KEY,
    driver_id    BIGINT NOT NULL REFERENCES drivers(id),
    period       TEXT NOT NULL,                  -- YYYY-MM
    amount_cents BIGINT NOT NULL,                -- 產生時定格的月會費（分）
    status       TEXT NOT NULL DEFAULT 'unpaid', -- unpaid / paid
    paid_at      TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT membership_invoices_status_check CHECK (status IN ('unpaid', 'paid')),
    CONSTRAINT membership_invoices_period_check CHECK (period ~ '^[0-9]{4}-[0-9]{2}$'),
    CONSTRAINT membership_invoices_amount_nonneg CHECK (amount_cents >= 0)
);

-- F9-6：同一司機同一月份只能有一筆，防月結排程重跑造成重複入帳。
CREATE UNIQUE INDEX IF NOT EXISTS uq_membership_driver_period
    ON membership_invoices (driver_id, period);

-- 依 period + status 列帳單（未繳查詢/催繳）走這個索引。
CREATE INDEX IF NOT EXISTS idx_membership_period_status
    ON membership_invoices (period, status);
