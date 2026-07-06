-- A1：司機登入密碼（bcrypt hash）。既有司機預設空字串（無法登入，需重設密碼）。
ALTER TABLE drivers ADD COLUMN IF NOT EXISTS password_hash text NOT NULL DEFAULT '';
