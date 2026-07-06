package service

import (
	"context"

	"golang.org/x/crypto/bcrypt"

	"line-fleet-dispatch/internal/model"
)

// customerStore 抽象出 CustomerRegistry 需要的 repository 行為（方便測試替身）
type customerStore interface {
	FindByLineUserID(lineUserID string) (*model.Customer, error)
	FindOrCreateByLineUserID(lineUserID, name string) (*model.Customer, error)
	SetPassword(id int64, passwordHash string) error
}

// CustomerRegistry 乘客註冊與登入
type CustomerRegistry struct {
	customers customerStore
}

func NewCustomerRegistry(customers customerStore) *CustomerRegistry {
	return &CustomerRegistry{customers: customers}
}

// Register 建立或取回乘客並設定登入密碼（bcrypt）
func (s *CustomerRegistry) Register(ctx context.Context, lineUserID, name, password string) (*model.Customer, error) {
	customer, err := s.customers.FindOrCreateByLineUserID(lineUserID, name)
	if err != nil {
		return nil, err
	}
	if password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return nil, err
		}
		if err := s.customers.SetPassword(customer.ID, string(hash)); err != nil {
			return nil, err
		}
		customer.PasswordHash = string(hash)
	}
	return customer, nil
}

// Login 以 line_user_id + 密碼驗證
func (s *CustomerRegistry) Login(ctx context.Context, lineUserID, password string) (*model.Customer, error) {
	customer, err := s.customers.FindByLineUserID(lineUserID)
	if err != nil {
		return nil, ErrInvalidCredentials
	}
	if customer.PasswordHash == "" ||
		bcrypt.CompareHashAndPassword([]byte(customer.PasswordHash), []byte(password)) != nil {
		return nil, ErrInvalidCredentials
	}
	return customer, nil
}
