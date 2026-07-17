-- 車輛快照（O7）：接單時把司機當下的車種／車牌定格寫進 ride。
--
-- 為什麼要快照而不是 JOIN drivers：司機會換車（O2 的 PUT /api/driver/vehicle 隨時可改），
-- 但歷史行程不能跟著變——乘客三個月後回頭找遺失物時，要知道「當時搭的是哪台車」。
-- 與費率／會費／遺失物處理費的快照制一致。
ALTER TABLE rides ADD COLUMN IF NOT EXISTS driver_vehicle_type TEXT NOT NULL DEFAULT '';
ALTER TABLE rides ADD COLUMN IF NOT EXISTS driver_plate_number TEXT NOT NULL DEFAULT '';

-- 值域同 O1 白名單；'' ＝尚未接單（或舊資料）。
-- 注意：這裡**不加**車牌唯一索引——快照是歷史紀錄，同一台車本來就會出現在很多趟行程上。
ALTER TABLE rides DROP CONSTRAINT IF EXISTS chk_rides_driver_vehicle_type;
ALTER TABLE rides ADD CONSTRAINT chk_rides_driver_vehicle_type
    CHECK (driver_vehicle_type IN ('', 'sedan', 'suv', 'van7', 'accessible', 'pet'));
