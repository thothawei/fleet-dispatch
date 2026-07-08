CREATE TABLE IF NOT EXISTS ride_events (
    id BIGSERIAL PRIMARY KEY,
    ride_id BIGINT NOT NULL REFERENCES rides(id),
    from_status SMALLINT,
    to_status SMALLINT NOT NULL,
    event_type TEXT NOT NULL,
    actor_role TEXT NOT NULL DEFAULT '',
    actor_id BIGINT,
    note TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ride_events_ride_created
    ON ride_events (ride_id, created_at);
