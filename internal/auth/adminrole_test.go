package auth

import "testing"

func TestParseAdminRole(t *testing.T) {
	if r, ok := ParseAdminRole("dispatcher"); !ok || r != RoleDispatcher {
		t.Fatalf("dispatcher 應解析成功，得 %v %v", r, ok)
	}
	if _, ok := ParseAdminRole("dispatcherr"); ok {
		t.Fatal("打錯字的 role 應被拒")
	}
	if _, ok := ParseAdminRole(""); ok {
		t.Fatal("空字串應被拒")
	}
}

func TestAdminRoleAtLeast(t *testing.T) {
	if !RoleSuperadmin.AtLeast(RoleDispatcher) {
		t.Fatal("superadmin 應 >= dispatcher")
	}
	if RoleViewer.AtLeast(RoleDispatcher) {
		t.Fatal("viewer 不應 >= dispatcher")
	}
	if !RoleDispatcher.AtLeast(RoleDispatcher) {
		t.Fatal("同級應成立")
	}
}
