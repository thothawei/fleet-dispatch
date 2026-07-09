package auth

// AdminRole 後台角色（rank 由小到大，高階含低階全部權限）
type AdminRole string

const (
	RoleViewer     AdminRole = "viewer"
	RoleDispatcher AdminRole = "dispatcher"
	RoleSuperadmin AdminRole = "superadmin"
)

var adminRoleRank = map[AdminRole]int{
	RoleViewer:     1,
	RoleDispatcher: 2,
	RoleSuperadmin: 3,
}

// ParseAdminRole 驗證並轉型；非白名單值回 ok=false
func ParseAdminRole(s string) (AdminRole, bool) {
	r := AdminRole(s)
	if _, ok := adminRoleRank[r]; ok {
		return r, true
	}
	return "", false
}

// Rank 角色等級；未知角色回 0
func (r AdminRole) Rank() int { return adminRoleRank[r] }

// AtLeast 是否具備至少 min 的權限
func (r AdminRole) AtLeast(min AdminRole) bool { return r.Rank() >= min.Rank() && r.Rank() > 0 }
