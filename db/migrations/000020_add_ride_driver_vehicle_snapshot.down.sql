-- DROP COLUMN 會自動連帶刪除依賴該欄位的 CHECK；DROP CONSTRAINT 是明示意圖（IF EXISTS，無害）。
ALTER TABLE rides DROP CONSTRAINT IF EXISTS chk_rides_driver_vehicle_type;
ALTER TABLE rides DROP COLUMN IF EXISTS driver_plate_number;
ALTER TABLE rides DROP COLUMN IF EXISTS driver_vehicle_type;
