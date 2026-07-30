-- 常用地點：乘客把住家／公司／其他常去的地方存起來，叫車與預約時一鍵帶入。
-- 座標與 rides 用同一套 geography(Point,4326)，讓帶入後的建單路徑與地圖選點完全一致。
CREATE TABLE IF NOT EXISTS customer_saved_places (
    id          BIGSERIAL PRIMARY KEY,
    customer_id BIGINT NOT NULL REFERENCES customers(id),
    -- kind：home／work／custom。前兩者是「語意化插槽」，UI 會給它們固定圖示與排序；
    -- custom 則是使用者自訂的其他地點，數量不限。
    kind        TEXT NOT NULL DEFAULT 'custom',
    label       TEXT NOT NULL,
    address     TEXT NOT NULL,
    point       geography(Point,4326) NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT saved_place_kind_check CHECK (kind IN ('home', 'work', 'custom')),
    CONSTRAINT saved_place_label_check CHECK (char_length(label) BETWEEN 1 AND 40),
    CONSTRAINT saved_place_address_check CHECK (char_length(address) BETWEEN 1 AND 200)
);

-- 住家與公司**各只能有一筆**（部分唯一索引，custom 不受限）。
-- 這是 DB 層的最後防線：服務層改成「同 kind 已存在就覆蓋」，競態下才會撞到這裡。
CREATE UNIQUE INDEX IF NOT EXISTS uq_saved_place_customer_kind
    ON customer_saved_places (customer_id, kind) WHERE kind <> 'custom';

-- 乘客端「我的常用地點」清單。
CREATE INDEX IF NOT EXISTS idx_saved_place_customer
    ON customer_saved_places (customer_id, id);
