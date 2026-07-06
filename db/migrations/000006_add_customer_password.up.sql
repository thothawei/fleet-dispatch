-- 乘客登入密碼（bcrypt hash）。既有乘客預設空字串（無法登入，需設定密碼）。
ALTER TABLE customers ADD COLUMN IF NOT EXISTS password_hash text NOT NULL DEFAULT '';
