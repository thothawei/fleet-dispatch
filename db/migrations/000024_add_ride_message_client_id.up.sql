-- 訊息送出的冪等鍵（client_msg_id）。
--
-- 為什麼需要它：App 送出訊息逾時後，「後端其實收到了、只是回應遺失」與
-- 「後端沒收到」在客戶端看起來完全一樣。其他寫入路徑（接單／取消／協尋／評分）
-- 都能靠「查一次後端狀態」對帳，因為那些狀態在後端是唯一的；
-- **訊息不是**——「同內容再送一次」本來就是合法行為，
-- 所以 App 端無法只靠補讀分辨「上一次其實送出了」與「使用者真的想再說一次」。
-- 由客戶端產生一個鍵、後端據此去重，是唯一能兩者兼顧的做法。
--
-- 允許 NULL：既有訊息沒有這個鍵，且 LINE webhook 之類非 App 來源也不需要帶。
ALTER TABLE ride_messages ADD COLUMN IF NOT EXISTS client_msg_id VARCHAR(64);

-- 去重範圍是「同一趟行程的同一位發話者」——**不是全表唯一**。
-- 鍵由客戶端產生，跨使用者撞號不該讓後方那位發不出訊息。
-- partial index（WHERE NOT NULL）讓沒帶鍵的訊息完全不受這條約束。
CREATE UNIQUE INDEX IF NOT EXISTS uq_ride_messages_client_msg_id
    ON ride_messages (ride_id, sender_role, sender_id, client_msg_id)
    WHERE client_msg_id IS NOT NULL;
