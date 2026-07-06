package service

import (
	"context"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"line-fleet-dispatch/internal/model"
)

// fakeAdminStore 以記憶體模擬 AdminRepository 的行為（僅測試 AdminRegistry 邏輯）
type fakeAdminStore struct {
	byEmail map[string]*model.Admin
	nextID  int64
}

func newFakeAdminStore() *fakeAdminStore {
	return &fakeAdminStore{byEmail: map[string]*model.Admin{}, nextID: 1}
}
func (f *fakeAdminStore) FindByEmail(email string) (*model.Admin, error) {
	a, ok := f.byEmail[email]
	if !ok {
		return nil, ErrNotFound
	}
	return a, nil
}
func (f *fakeAdminStore) Create(a *model.Admin) error {
	a.ID = f.nextID
	f.nextID++
	f.byEmail[a.Email] = a
	return nil
}
func (f *fakeAdminStore) CountAll() (int64, error) { return int64(len(f.byEmail)), nil }

func TestAdminRegistry_種子與登入(t *testing.T) {
	store := newFakeAdminStore()
	reg := NewAdminRegistry(store)
	ctx := context.Background()

	// 種子建立一個管理員
	if err := reg.EnsureSeed(ctx, "ops@example.com", "s3cret"); err != nil {
		t.Fatalf("EnsureSeed 失敗: %v", err)
	}
	// 再次種子不應重複建立
	if err := reg.EnsureSeed(ctx, "ops@example.com", "s3cret"); err != nil {
		t.Fatalf("重複 EnsureSeed 失敗: %v", err)
	}
	if n, _ := store.CountAll(); n != 1 {
		t.Fatalf("種子應只建立 1 個管理員，實際 %d", n)
	}

	// 正確密碼可登入
	admin, err := reg.Login(ctx, "ops@example.com", "s3cret")
	if err != nil {
		t.Fatalf("登入失敗: %v", err)
	}
	if admin.Email != "ops@example.com" {
		t.Fatalf("登入回傳錯誤: %+v", admin)
	}
	// 驗證密碼確實是 bcrypt 雜湊
	if bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte("s3cret")) != nil {
		t.Fatal("密碼未正確以 bcrypt 儲存")
	}

	// 錯誤密碼被拒
	if _, err := reg.Login(ctx, "ops@example.com", "wrong"); err == nil {
		t.Fatal("錯誤密碼應登入失敗")
	}
}
