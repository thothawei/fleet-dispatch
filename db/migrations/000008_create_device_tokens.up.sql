CREATE TABLE IF NOT EXISTS device_tokens (
    id BIGSERIAL PRIMARY KEY,
    role TEXT NOT NULL CHECK (role IN ('driver', 'customer')),
    subject_id BIGINT NOT NULL,
    platform TEXT NOT NULL CHECK (platform IN ('fcm', 'apns')),
    token TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (role, subject_id, token)
);

CREATE INDEX IF NOT EXISTS idx_device_tokens_subject
    ON device_tokens (role, subject_id);
