ALTER TABLE drivers DROP CONSTRAINT IF EXISTS chk_drivers_vehicle_review_status;
ALTER TABLE drivers DROP COLUMN IF EXISTS vehicle_review_note;
ALTER TABLE drivers DROP COLUMN IF EXISTS vehicle_review_status;
