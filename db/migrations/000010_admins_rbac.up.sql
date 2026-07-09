-- 後台管理員導入 RBAC：新增 role（viewer/dispatcher/superadmin）與 is_active 欄位。
-- 既有 admin 一律預設為 superadmin 且啟用中，維持既有登入行為不受影響。
ALTER TABLE admins ADD COLUMN role text NOT NULL DEFAULT 'superadmin';
ALTER TABLE admins ADD COLUMN is_active boolean NOT NULL DEFAULT true;
ALTER TABLE admins ADD CONSTRAINT admins_role_check
    CHECK (role IN ('viewer', 'dispatcher', 'superadmin'));
