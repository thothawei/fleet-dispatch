CREATE TABLE IF NOT EXISTS ride_tracks (
    id BIGSERIAL,
    ride_id BIGINT NOT NULL,
    driver_id BIGINT NOT NULL,
    location GEOGRAPHY(POINT, 4326) NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id, recorded_at)
) PARTITION BY RANGE (recorded_at);

CREATE TABLE IF NOT EXISTS ride_tracks_2026_07 PARTITION OF ride_tracks
    FOR VALUES FROM ('2026-07-01') TO ('2026-08-01');

CREATE TABLE IF NOT EXISTS ride_tracks_2026_08 PARTITION OF ride_tracks
    FOR VALUES FROM ('2026-08-01') TO ('2026-09-01');

CREATE INDEX IF NOT EXISTS idx_tracks_ride ON ride_tracks(ride_id, recorded_at);
CREATE INDEX IF NOT EXISTS idx_tracks_gix ON ride_tracks USING GIST(location);
