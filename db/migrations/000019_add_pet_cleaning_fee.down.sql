-- DROP COLUMN 會自動連帶刪除依賴該欄位的 CHECK；DROP CONSTRAINT 是明示意圖（IF EXISTS，無害）。
ALTER TABLE daily_driver_earnings DROP COLUMN IF EXISTS cleaning_fee_cents;
ALTER TABLE rides DROP COLUMN IF EXISTS cleaning_fee_cents;
ALTER TABLE fleet_settings DROP CONSTRAINT IF EXISTS chk_fleet_settings_pet_cleaning_fee_bps;
ALTER TABLE fleet_settings DROP COLUMN IF EXISTS pet_cleaning_fee_bps;
