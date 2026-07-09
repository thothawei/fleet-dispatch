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

// Test既有Admin_migration後為superadmin且啟用 驗證 000010 migration 的回填預設值：
// 未指定 Role/IsActive 時（模擬既有帳號），新建的 admin 應自動落在 role='superadmin'、is_active=true，
// 確保既有登入流程不受 RBAC 欄位新增影響。
func Test既有Admin_migration後為superadmin且啟用(t *testing.T) {
	db := newMigratedTestDB(t)
	repo := NewAdminRepository(db)

	_ = repo.Create(&model.Admin{Username: "legacy", PasswordHash: "x", Name: "舊帳號"})
	got, err := repo.FindByUsername("legacy")
	if err != nil {
		t.Fatalf("查詢失敗: %v", err)
	}
	if got.Role != "superadmin" || !got.IsActive {
		t.Fatalf("既有 admin 應為 superadmin 且啟用，得 role=%q active=%v", got.Role, got.IsActive)
	}
}
