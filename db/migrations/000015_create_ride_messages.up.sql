-- 乘客↔司機行程內對話（每則訊息綁一趟行程）。
-- 即時遞送走 WebSocket Hub（chat.message 事件），本表為歷史紀錄與離線補讀的真源。
CREATE TABLE IF NOT EXISTS ride_messages (
    id          BIGSERIAL PRIMARY KEY,
    ride_id     BIGINT NOT NULL REFERENCES rides(id),
    sender_role TEXT NOT NULL,
    sender_id   BIGINT NOT NULL,
    body        TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ride_messages_role_check CHECK (sender_role IN ('customer', 'driver')),
    CONSTRAINT ride_messages_body_check CHECK (char_length(body) BETWEEN 1 AND 1000)
);

-- 依行程拉歷史／增量補讀（WHERE ride_id = ? AND id > ? ORDER BY id）走這個索引。
CREATE INDEX IF NOT EXISTS idx_ride_messages_ride_id ON ride_messages (ride_id, id);
