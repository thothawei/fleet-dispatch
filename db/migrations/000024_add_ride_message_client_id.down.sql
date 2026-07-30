DROP INDEX IF EXISTS uq_ride_messages_client_msg_id;
ALTER TABLE ride_messages DROP COLUMN IF EXISTS client_msg_id;
