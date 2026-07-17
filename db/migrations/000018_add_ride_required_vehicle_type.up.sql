-- 乘客指定車種（P1）：乘客叫車時可要求特定車種（寵物用車／無障礙／七人座…）。
-- 必須存在 ride 上而不只是派單當下的參數——清潔費（O6）、報表與稽核都要回頭看
-- 「這趟乘客要的是什麼車」；派單參數不留痕，事後無從對帳。
--
-- 沿用 drivers.vehicle_type 的慣例（O1）：TEXT NOT NULL DEFAULT ''，'' 代表「不指定」。
-- 刻意不用 NULL——否則「未指定」會有 NULL 與 '' 兩種表示法，過濾條件得同時處理兩者，
-- 遲早有一條路徑漏掉。既有訂單一律為「不指定」，維持現行行為（任何車種都可派）。
ALTER TABLE rides ADD COLUMN IF NOT EXISTS required_vehicle_type TEXT NOT NULL DEFAULT '';

-- 值域同 O1 的車種白名單；'' ＝不指定。
-- 與 chk_drivers_vehicle_type 放同一層的理由相同：這是清潔費（O6）加收與否的判斷依據，
-- 值髒掉直接影響計費，最後防線要在資料層而非 API 驗證。
ALTER TABLE rides DROP CONSTRAINT IF EXISTS chk_rides_required_vehicle_type;
ALTER TABLE rides ADD CONSTRAINT chk_rides_required_vehicle_type
    CHECK (required_vehicle_type IN ('', 'sedan', 'suv', 'van7', 'accessible', 'pet'));
