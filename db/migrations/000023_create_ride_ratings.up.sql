-- 乘客評分司機（B5）：一趟行程一則評分，1–5 星＋選填評論。
--
-- **一趟一評、不可重評**（uq_ride_ratings_ride）：分數一旦送出就是該趟的定論。
-- 允許改分會讓「司機平均分」變成可被事後情緒左右的浮動值，
-- 也讓「評過沒」失去明確答案（App 完成卡要據此決定顯示星星還是評分按鈕）。
--
-- customer_id／driver_id 是**寫入當下的快照**，不靠 rides 反查：
-- 評分屬於「這位乘客給這位司機」的事實，行程資料日後若調整（如改派）不該改寫歷史評分。
CREATE TABLE IF NOT EXISTS ride_ratings (
    id          BIGSERIAL PRIMARY KEY,
    ride_id     BIGINT   NOT NULL REFERENCES rides(id) ON DELETE CASCADE,
    customer_id BIGINT   NOT NULL,
    driver_id   BIGINT   NOT NULL,
    score       SMALLINT NOT NULL,
    -- 評論選填；空字串＝只給星等不留言（不用 NULL，省掉 App 端的三態判斷）。
    comment     TEXT     NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_ride_ratings_score CHECK (score BETWEEN 1 AND 5),
    -- 服務層另有較嚴格的字數上限；這是直打 DB 也擋得住的最後防線（比照 lost_item_requests）。
    CONSTRAINT chk_ride_ratings_comment_len CHECK (char_length(comment) <= 500)
);

-- 一趟一評的真正把關（UNIQUE 索引，服務層先查一次、此為競態下的最後防線）。
CREATE UNIQUE INDEX IF NOT EXISTS uq_ride_ratings_ride_id ON ride_ratings(ride_id);

-- 司機平均分（GET /api/driver/me）是對 driver_id 的聚合，資料量長期只增不減。
CREATE INDEX IF NOT EXISTS idx_ride_ratings_driver_id ON ride_ratings(driver_id);
