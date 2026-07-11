DROP INDEX IF EXISTS idx_rides_driver_completed;
DROP INDEX IF EXISTS idx_rides_status_completed;

ALTER TABLE rides DROP COLUMN IF EXISTS driver_net_amount_cents;
ALTER TABLE rides DROP COLUMN IF EXISTS commission_amount_cents;
ALTER TABLE rides DROP COLUMN IF EXISTS fare_amount_cents;
