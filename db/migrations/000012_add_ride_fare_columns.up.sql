-- 行程計費欄位（F2）：完成時定格寫入（費率快照制），一律以「分」儲存。
-- 未完成/取消的行程為 NULL，報表以 COALESCE(SUM(...),0) 聚合。
ALTER TABLE rides ADD COLUMN IF NOT EXISTS fare_amount_cents       BIGINT;
ALTER TABLE rides ADD COLUMN IF NOT EXISTS commission_amount_cents BIGINT;
ALTER TABLE rides ADD COLUMN IF NOT EXISTS driver_net_amount_cents BIGINT;

-- 報表大資料量索引（F9-1）：
--   日/月報表依 (status, completed_at) 走範圍掃描（搭配 sargable 的半開區間查詢）；
--   司機收入查詢依 (driver_id, completed_at)。
CREATE INDEX IF NOT EXISTS idx_rides_status_completed ON rides (status, completed_at);
CREATE INDEX IF NOT EXISTS idx_rides_driver_completed ON rides (driver_id, completed_at);
