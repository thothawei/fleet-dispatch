package service

import (
	"errors"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"line-fleet-dispatch/internal/auth"
	"line-fleet-dispatch/internal/model"
	"line-fleet-dispatch/internal/repository"
)

// AdminUsers 帳號管理（列表／建立／更新），防自我鎖死與移除最後一個 superadmin
type AdminUsers struct {
	repo *repository.AdminRepository
}

func NewAdminUsers(repo *repository.AdminRepository) *AdminUsers { return &AdminUsers{repo: repo} }

// List 列出所有管理員帳號
func (s *AdminUsers) List() ([]model.Admin, error) { return s.repo.ListAll() }

// Create 新增管理員帳號
func (s *AdminUsers) Create(username, password, role string) (*model.Admin, error) {
	if _, ok := auth.ParseAdminRole(role); !ok {
		return nil, ErrBadRole
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	a := &model.Admin{Username: username, PasswordHash: string(hash), Role: role,
		IsActive: true, Name: username, CreatedAt: now, UpdatedAt: now}
	if err := s.repo.Create(a); err != nil {
		return nil, err
	}
	return a, nil
}

// Update 改角色／重設密碼／啟停；防自我鎖死與移除最後一個 superadmin（交易內檢查）
func (s *AdminUsers) Update(actorID, targetID int64, newRole, newPassword *string, active *bool) error {
	if newRole != nil {
		if _, ok := auth.ParseAdminRole(*newRole); !ok {
			return ErrBadRole
		}
	}
	// 對自己的降級/停用一律擋（避免把自己鎖在外面）
	if actorID == targetID {
		if (active != nil && !*active) || (newRole != nil && *newRole != string(auth.RoleSuperadmin)) {
			return ErrSelfLockout
		}
	}
	return s.repo.Tx(func(tx *gorm.DB) error {
		var target model.Admin
		if err := tx.First(&target, targetID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		demoting := newRole != nil && *newRole != string(auth.RoleSuperadmin) && target.Role == string(auth.RoleSuperadmin)
		deactivating := active != nil && !*active && target.IsActive
		if (demoting || deactivating) && target.Role == string(auth.RoleSuperadmin) {
			n, err := s.repo.LockActiveSuperadmins(tx)
			if err != nil {
				return err
			}
			if n <= 1 {
				return ErrLastSuperadmin
			}
		}
		if newRole != nil {
			target.Role = *newRole
		}
		if active != nil {
			target.IsActive = *active
		}
		if newPassword != nil {
			hash, err := bcrypt.GenerateFromPassword([]byte(*newPassword), bcrypt.DefaultCost)
			if err != nil {
				return err
			}
			target.PasswordHash = string(hash)
		}
		target.UpdatedAt = time.Now()
		return tx.Model(&target).
			Select("Role", "IsActive", "PasswordHash", "UpdatedAt").Updates(&target).Error
	})
}
