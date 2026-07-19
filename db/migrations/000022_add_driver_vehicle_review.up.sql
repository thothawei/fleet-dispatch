-- 車輛審核（O5 定案 2026-07-19）：司機填/改車輛後需 admin 核准才能接單。
-- O3 gate 從「有填車種車牌」升級為「已審核」；App 端多一種「待審核」等待狀態。
-- 沿用 drivers 既有慣例：TEXT NOT NULL DEFAULT ''。
ALTER TABLE drivers ADD COLUMN IF NOT EXISTS vehicle_review_status TEXT NOT NULL DEFAULT '';
ALTER TABLE drivers ADD COLUMN IF NOT EXISTS vehicle_review_note TEXT NOT NULL DEFAULT '';

-- 值域最後防線放 DB CHECK：gate 直接讀它，值髒掉會讓不該接單的司機接到單。
ALTER TABLE drivers DROP CONSTRAINT IF EXISTS chk_drivers_vehicle_review_status;
ALTER TABLE drivers ADD CONSTRAINT chk_drivers_vehicle_review_status
    CHECK (vehicle_review_status IN ('', 'pending', 'approved', 'rejected'));

-- 祖父化：導入審核前既有已填車輛的司機一律視為已核准，不因導入審核而被鎖出
-- （O5 導入決策 2026-07-19；未填車輛者維持 ''，仍走強制設定頁）。
UPDATE drivers SET vehicle_review_status = 'approved'
    WHERE vehicle_type <> '' AND plate_number <> '' AND vehicle_review_status = '';
