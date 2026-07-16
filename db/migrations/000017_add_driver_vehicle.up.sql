-- 司機車輛資訊（O1）：乘客端要顯示「搭的是什麼車」，且司機未填車輛資訊不得被派單／接單（O3 的 gate）。
-- 沿用 drivers 既有慣例：TEXT NOT NULL DEFAULT ''，'' 代表未設定。
-- 既有司機一律為未設定，上線後必須自行填寫（不設寬限期，2026-07-16 拍板）。
ALTER TABLE drivers ADD COLUMN IF NOT EXISTS vehicle_type TEXT NOT NULL DEFAULT '';
ALTER TABLE drivers ADD COLUMN IF NOT EXISTS plate_number TEXT NOT NULL DEFAULT '';

-- 車種白名單（O1 定案）：sedan 轎車／suv 休旅／van7 七人座／accessible 無障礙／pet 寵物用車。
-- 後端只認 code，顯示名稱由前端對應；'' 為未設定。
-- 放 DB CHECK 而不只在 API 驗證：這是清潔費（O6）與派單過濾（P3）的判斷依據，
-- 值一旦髒掉會直接影響計費，最後防線要在資料層。
ALTER TABLE drivers DROP CONSTRAINT IF EXISTS chk_drivers_vehicle_type;
ALTER TABLE drivers ADD CONSTRAINT chk_drivers_vehicle_type
    CHECK (vehicle_type IN ('', 'sedan', 'suv', 'van7', 'accessible', 'pet'));

-- 車牌唯一：同一車牌不該同時掛兩個司機帳號。
-- 必須是 partial index —— 既有司機的 plate_number 都是 ''，
-- 若用一般 UNIQUE，第二個未填車牌的司機就會撞唯一鍵而插不進去。
CREATE UNIQUE INDEX IF NOT EXISTS uq_drivers_plate_number
    ON drivers (plate_number) WHERE plate_number <> '';
