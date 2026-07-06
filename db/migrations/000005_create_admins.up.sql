-- 後台管理員帳號（email + bcrypt 密碼）
CREATE TABLE IF NOT EXISTS admins (
    id            bigserial PRIMARY KEY,
    email         text UNIQUE NOT NULL,
    password_hash text NOT NULL DEFAULT '',
    name          text NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);
