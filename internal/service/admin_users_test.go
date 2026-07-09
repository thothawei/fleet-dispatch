package service

import (
	"errors"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"line-fleet-dispatch/internal/model"
	"line-fleet-dispatch/internal/repository"
)

// newAdminUsersWithRepo 起真 DB，回 service 與其 repo（供需直接操作 repo 的測試使用）
// （newServiceTestDB 已含 t.Skipf 優雅跳過邏輯，見 testdb_test.go）
func newAdminUsersWithRepo(t *testing.T) (*AdminUsers, *repository.AdminRepository) {
	t.Helper()
	db := newServiceTestDB(t)
	repo := repository.NewAdminRepository(db)
	return NewAdminUsers(repo), repo
}

// newAdminUsersWithSeed 起真 DB 並種一個 superadmin，回 service 與該 seed 的 id
func newAdminUsersWithSeed(t *testing.T) (*AdminUsers, int64) {
	t.Helper()
	svc, repo := newAdminUsersWithRepo(t)
	now := time.Now()
	seed := &model.Admin{
		Username: "root", PasswordHash: "x", Name: "系統管理員",
		Role: "superadmin", IsActive: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.Create(seed); err != nil {
		t.Fatalf("seed 失敗: %v", err)
	}
	return svc, seed.ID
}

func TestUpdate_不可降級最後一個superadmin(t *testing.T) {
	svc, seedID := newAdminUsersWithSeed(t)
	viewer := "viewer"
	err := svc.Update(seedID, seedID, &viewer, nil, nil)
	if !errors.Is(err, ErrSelfLockout) && !errors.Is(err, ErrLastSuperadmin) {
		t.Fatalf("降級唯一 superadmin（且是自己）應被擋，得 %v", err)
	}
}

func TestUpdate_不可停用最後一個superadmin(t *testing.T) {
	svc, repo := newAdminUsersWithRepo(t)
	now := time.Now()
	seed := &model.Admin{
		Username: "root", PasswordHash: "x", Name: "系統管理員",
		Role: "superadmin", IsActive: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.Create(seed); err != nil {
		t.Fatalf("seed 失敗: %v", err)
	}
	// 換一個不同的 actor，避免觸發「對自己」的鎖死擋法，專測「最後一個 superadmin」擋法
	other := &model.Admin{
		Username: "other", PasswordHash: "x", Name: "其他管理員",
		Role: "viewer", IsActive: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.Create(other); err != nil {
		t.Fatalf("建立 other 失敗: %v", err)
	}
	inactive := false
	err := svc.Update(other.ID, seed.ID, nil, nil, &inactive)
	if !errors.Is(err, ErrLastSuperadmin) {
		t.Fatalf("停用最後一個 superadmin 應回 ErrLastSuperadmin，得 %v", err)
	}
}

func TestCreate_壞role被拒(t *testing.T) {
	svc, _ := newAdminUsersWithSeed(t)
	if _, err := svc.Create("bob", "pw123456", "root"); !errors.Is(err, ErrBadRole) {
		t.Fatalf("壞 role 應回 ErrBadRole，得 %v", err)
	}
}

func TestCreate_成功(t *testing.T) {
	svc, _ := newAdminUsersWithSeed(t)
	a, err := svc.Create("bob", "pw123456", "viewer")
	if err != nil {
		t.Fatalf("建立失敗: %v", err)
	}
	if a.Username != "bob" || a.Role != "viewer" || !a.IsActive {
		t.Fatalf("建立結果不符預期: %+v", a)
	}
	if bcrypt.CompareHashAndPassword([]byte(a.PasswordHash), []byte("pw123456")) != nil {
		t.Fatalf("密碼雜湊無法驗證")
	}
}

func TestUpdate_停用非最後一個superadmin成功(t *testing.T) {
	svc, seedID := newAdminUsersWithSeed(t)
	// 種第二個 superadmin，seed 就不是「最後一個」了
	second, err := svc.Create("second-admin", "pw123456", "superadmin")
	if err != nil {
		t.Fatalf("建立第二個 superadmin 失敗: %v", err)
	}
	inactive := false
	if err := svc.Update(second.ID, seedID, nil, nil, &inactive); err != nil {
		t.Fatalf("停用非最後一個 superadmin 應成功，得 %v", err)
	}
	list, err := svc.List()
	if err != nil {
		t.Fatalf("List 失敗: %v", err)
	}
	var found bool
	for _, a := range list {
		if a.ID == seedID {
			found = true
			if a.IsActive {
				t.Fatalf("seed 應已被停用")
			}
		}
	}
	if !found {
		t.Fatalf("找不到 seed")
	}
}

func TestUpdate_不存在的id回ErrNotFound(t *testing.T) {
	svc, _ := newAdminUsersWithSeed(t)
	role := "viewer"
	if err := svc.Update(999999, 888888, &role, nil, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在的 target 應回 ErrNotFound，得 %v", err)
	}
}

// TestUpdate_降級已停用的superadmin不誤觸發最後保護 回歸測試：
// 一個「已停用」的 superadmin 本來就不計入啟用名額，把它降級成 viewer
// 不會減少啟用中 superadmin 的數量，因此即使場上只剩一個「啟用中」的
// superadmin，也不該被 ErrLastSuperadmin 擋下。
func TestUpdate_降級已停用的superadmin不誤觸發最後保護(t *testing.T) {
	svc, seedID := newAdminUsersWithSeed(t)
	// 種第二個 superadmin 後先停用它 → 它成為「已停用的 superadmin」
	second, err := svc.Create("second-admin", "pw123456", "superadmin")
	if err != nil {
		t.Fatalf("建立第二個 superadmin 失敗: %v", err)
	}
	inactive := false
	if err := svc.Update(seedID, second.ID, nil, nil, &inactive); err != nil {
		t.Fatalf("停用第二個 superadmin 應成功，得 %v", err)
	}
	// 此刻只剩 seed 一個啟用中 superadmin；把已停用的 second 降級成 viewer 應成功
	viewer := "viewer"
	if err := svc.Update(seedID, second.ID, &viewer, nil, nil); err != nil {
		t.Fatalf("降級已停用的 superadmin 不該被最後保護擋下，得 %v", err)
	}
	list, err := svc.List()
	if err != nil {
		t.Fatalf("List 失敗: %v", err)
	}
	for _, a := range list {
		if a.ID == second.ID && a.Role != "viewer" {
			t.Fatalf("second 應已降級為 viewer，得 role=%s", a.Role)
		}
		if a.ID == seedID && (a.Role != "superadmin" || !a.IsActive) {
			t.Fatalf("seed 應仍為啟用中 superadmin，得 role=%s active=%v", a.Role, a.IsActive)
		}
	}
}

// TestUpdate_並發降級停用不會造成零superadmin 驗證 write-skew 防護：
// 種兩個 active superadmin，同時對其中一個「降級」、對另一個「停用」，
// 靠 LockActiveSuperadmins 的 FOR UPDATE row lock 讓兩個交易序列化，
// 使後執行者重讀到真實計數而擋下，最終至少留一個 active superadmin。
func TestUpdate_並發降級停用不會造成零superadmin(t *testing.T) {
	svc, repo := newAdminUsersWithRepo(t)
	now := time.Now()
	sa1 := &model.Admin{
		Username: "sa1", PasswordHash: "x", Name: "sa1",
		Role: "superadmin", IsActive: true, CreatedAt: now, UpdatedAt: now,
	}
	sa2 := &model.Admin{
		Username: "sa2", PasswordHash: "x", Name: "sa2",
		Role: "superadmin", IsActive: true, CreatedAt: now, UpdatedAt: now,
	}
	// 中立第三方 actor（非本次操作的兩個 superadmin 本人），避免觸發自我鎖死擋法
	actor := &model.Admin{
		Username: "actor", PasswordHash: "x", Name: "actor",
		Role: "viewer", IsActive: true, CreatedAt: now, UpdatedAt: now,
	}
	for _, a := range []*model.Admin{sa1, sa2, actor} {
		if err := repo.Create(a); err != nil {
			t.Fatalf("seed 失敗: %v", err)
		}
	}

	var wg sync.WaitGroup
	var err1, err2 error
	wg.Add(2)
	go func() {
		defer wg.Done()
		viewer := "viewer"
		err1 = svc.Update(actor.ID, sa1.ID, &viewer, nil, nil) // T1：降級 sa1
	}()
	go func() {
		defer wg.Done()
		inactive := false
		err2 = svc.Update(actor.ID, sa2.ID, nil, nil, &inactive) // T2：停用 sa2
	}()
	wg.Wait()

	n, err := repo.CountActiveSuperadmins(nil)
	if err != nil {
		t.Fatalf("查詢 active superadmin 數失敗: %v", err)
	}
	if n < 1 {
		t.Fatalf("並發降級/停用後 active superadmin 數應 >= 1，得 %d（err1=%v err2=%v）", n, err1, err2)
	}
	if !errors.Is(err1, ErrLastSuperadmin) && !errors.Is(err2, ErrLastSuperadmin) {
		t.Fatalf("兩個並發操作中應至少一個回 ErrLastSuperadmin（序列化後偵測到只剩一個），得 err1=%v err2=%v", err1, err2)
	}
}
