package repository

import (
	"testing"
	"time"

	"line-fleet-dispatch/internal/model"
)

func TestAdminRepository_建立與查詢(t *testing.T) {
	db := newMigratedTestDB(t)
	repo := NewAdminRepository(db)

	n0, err := repo.CountAll()
	if err != nil {
		t.Fatalf("CountAll 失敗: %v", err)
	}

	now := time.Now()
	a := &model.Admin{Username: "ops", PasswordHash: "hash", Name: "值班", CreatedAt: now, UpdatedAt: now}
	if err := repo.Create(a); err != nil {
		t.Fatalf("Create 失敗: %v", err)
	}
	if a.ID == 0 {
		t.Fatal("Create 後未回填 ID")
	}

	got, err := repo.FindByUsername("ops")
	if err != nil {
		t.Fatalf("FindByUsername 失敗: %v", err)
	}
	if got.Username != "ops" || got.Name != "值班" {
		t.Fatalf("查詢結果錯誤: %+v", got)
	}

	n1, err := repo.CountAll()
	if err != nil {
		t.Fatalf("CountAll 失敗: %v", err)
	}
	if n1 != n0+1 {
		t.Fatalf("CountAll 預期 %d，得到 %d", n0+1, n1)
	}
}
