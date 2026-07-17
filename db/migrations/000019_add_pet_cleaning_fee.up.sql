-- 寵物用車清潔費（O6）：乘客指定 pet 車種的行程，在車資之上加收清潔費。
-- 判斷依據是 rides.required_vehicle_type（乘客要什麼車），**不是** drivers.vehicle_type
-- （司機開什麼車）——乘客沒指定寵物車卻剛好被派到寵物車司機時不得加收。

-- 費率：比照 lost_item_fee_bps 的 bps 慣例。上限 30% 寫進 CHECK，不只靠 API 驗證：
-- 這是乘客實際被收的錢，設定寫錯的後果是超收。
ALTER TABLE fleet_settings ADD COLUMN IF NOT EXISTS pet_cleaning_fee_bps INT NOT NULL DEFAULT 0;
ALTER TABLE fleet_settings DROP CONSTRAINT IF EXISTS chk_fleet_settings_pet_cleaning_fee_bps;
ALTER TABLE fleet_settings ADD CONSTRAINT chk_fleet_settings_pet_cleaning_fee_bps
    CHECK (pet_cleaning_fee_bps >= 0 AND pet_cleaning_fee_bps <= 3000);

-- 快照：完成時定格寫入（比照 fare/commission/net 的費率快照制，000012）。
-- nullable——未完成／取消／舊資料為 NULL，與既有計費欄位語意一致。
ALTER TABLE rides ADD COLUMN IF NOT EXISTS cleaning_fee_cents BIGINT;

-- 報表分項（F5/F6/F7 的一致性延伸）：
-- 「營業額」(revenue_cents) 維持只含車資，清潔費另立分項——它是清潔成本補償、不是營收，
-- 且不計入抽成（拍板 2026-07-16）。NOT NULL DEFAULT 0：彙總欄位一律有值，
-- 報表 SUM 不必處理 NULL。
ALTER TABLE daily_driver_earnings ADD COLUMN IF NOT EXISTS cleaning_fee_cents BIGINT NOT NULL DEFAULT 0;
