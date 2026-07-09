package service

import (
	"context"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"line-fleet-dispatch/internal/model"
)

// fakeAdminStore 以記憶體模擬 AdminRepository 的行為（僅測試 AdminRegistry 邏輯）
type fakeAdminStore struct {
	byUsername map[string]*model.Admin
	nextID     int64
}

func newFakeAdminStore() *fakeAdminStore {
	return &fakeAdminStore{byUsername: map[string]*model.Admin{}, nextID: 1}
}
func (f *fakeAdminStore) FindByUsername(username string) (*model.Admin, error) {
	a, ok := f.byUsername[username]
	if !ok {
		return nil, ErrNotFound
	}
	return a, nil
}
func (f *fakeAdminStore) Create(a *model.Admin) error {
	a.ID = f.nextID
	f.nextID++
	f.byUsername[a.Username] = a
	return nil
}
func (f *fakeAdminStore) CountAll() (int64, error) { return int64(len(f.byUsername)), nil }

func TestAdminRegistry_種子與登入(t *testing.T) {
	store := newFakeAdminStore()
	reg := NewAdminRegistry(store)
	ctx := context.Background()

	// 種子建立一個管理員
	if err := reg.EnsureSeed(ctx, "admin", "s3cret"); err != nil {
		t.Fatalf("EnsureSeed 失敗: %v", err)
	}
	// 再次種子不應重複建立
	if err := reg.EnsureSeed(ctx, "admin", "s3cret"); err != nil {
		t.Fatalf("重複 EnsureSeed 失敗: %v", err)
	}
	if n, _ := store.CountAll(); n != 1 {
		t.Fatalf("種子應只建立 1 個管理員，實際 %d", n)
	}

	// 正確密碼可登入
	admin, err := reg.Login(ctx, "admin", "s3cret")
	if err != nil {
		t.Fatalf("登入失敗: %v", err)
	}
	if admin.Username != "admin" {
		t.Fatalf("登入回傳錯誤: %+v", admin)
	}
	// 驗證密碼確實是 bcrypt 雜湊
	if bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte("s3cret")) != nil {
		t.Fatal("密碼未正確以 bcrypt 儲存")
	}

	// 錯誤密碼被拒
	if _, err := reg.Login(ctx, "admin", "wrong"); err == nil {
		t.Fatal("錯誤密碼應登入失敗")
	}
}

func TestEnsureSeed_角色為superadmin(t *testing.T) {
	store := newFakeAdminStore()
	reg := NewAdminRegistry(store)
	_ = reg.EnsureSeed(context.Background(), "admin", "s3cret")
	a, _ := store.FindByUsername("admin")
	if a.Role != "superadmin" || !a.IsActive {
		t.Fatalf("種子應為 superadmin 且啟用，得 role=%q active=%v", a.Role, a.IsActive)
	}
}
