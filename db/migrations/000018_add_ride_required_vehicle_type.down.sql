-- DROP COLUMN 會自動連帶刪除依賴該欄位的 CHECK（比照 000017 的記錄）；
-- 這行是明示意圖，IF EXISTS 故無害。
ALTER TABLE rides DROP CONSTRAINT IF EXISTS chk_rides_required_vehicle_type;
ALTER TABLE rides DROP COLUMN IF EXISTS required_vehicle_type;
