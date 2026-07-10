-- 費率設定：單列全域設定，供計費/報表使用（F1）。
-- 金額一律以「分」儲存（顯示除 100）；手續費以基點 bps 儲存（1500 = 15%），
-- 用整數避免浮點誤差。單列約束：id 固定為 1。
CREATE TABLE IF NOT EXISTS fleet_settings (
    id                           SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    base_fare_cents              BIGINT NOT NULL DEFAULT 8500,     -- 起步價（分），預設 85 元
    per_km_fare_cents            BIGINT NOT NULL DEFAULT 2000,     -- 每公里費率（分），預設 20 元
    min_fare_cents               BIGINT NOT NULL DEFAULT 8500,     -- 最低車資（分）
    commission_bps               INT    NOT NULL DEFAULT 1500,     -- 手續費基點（1500 = 15%）
    monthly_membership_fee_cents BIGINT NOT NULL DEFAULT 300000,   -- 月會費（分），預設 3000 元
    updated_by                   BIGINT,
    updated_at                   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fleet_settings_nonneg CHECK (
        base_fare_cents >= 0 AND per_km_fare_cents >= 0 AND min_fare_cents >= 0
        AND commission_bps >= 0 AND commission_bps <= 10000
        AND monthly_membership_fee_cents >= 0
    )
);

-- 種一列預設設定（冪等）。計費在行程完成時讀這列，故必須永遠存在。
INSERT INTO fleet_settings (id) VALUES (1) ON CONFLICT (id) DO NOTHING;
