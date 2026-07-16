DROP INDEX IF EXISTS uq_drivers_plate_number;
ALTER TABLE drivers DROP CONSTRAINT IF EXISTS chk_drivers_vehicle_type;
ALTER TABLE drivers DROP COLUMN IF EXISTS plate_number;
ALTER TABLE drivers DROP COLUMN IF EXISTS vehicle_type;
