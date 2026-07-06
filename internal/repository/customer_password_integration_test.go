package repository

import (
	"testing"
)

func TestCustomerRepository_設密碼與依ID查詢(t *testing.T) {
	db := newMigratedTestDB(t)
	repo := NewCustomerRepository(db)

	cust, err := repo.FindOrCreateByLineUserID("U_cust_pw", "乘客A")
	if err != nil {
		t.Fatalf("建立乘客失敗: %v", err)
	}
	if err := repo.SetPassword(cust.ID, "hashed"); err != nil {
		t.Fatalf("SetPassword 失敗: %v", err)
	}
	got, err := repo.FindByID(cust.ID)
	if err != nil {
		t.Fatalf("FindByID 失敗: %v", err)
	}
	if got.PasswordHash != "hashed" {
		t.Fatalf("密碼未寫入: %q", got.PasswordHash)
	}
}
