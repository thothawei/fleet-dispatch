-- 後台管理員改用「帳號」登入：email 欄位改名為 username（沿用原 UNIQUE 約束）。
ALTER TABLE admins RENAME COLUMN email TO username;
