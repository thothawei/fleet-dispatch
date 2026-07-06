package service

import (
	"context"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"line-fleet-dispatch/internal/model"
)

type fakeCustomerStore struct {
	byLine map[string]*model.Customer
	nextID int64
}

func newFakeCustomerStore() *fakeCustomerStore {
	return &fakeCustomerStore{byLine: map[string]*model.Customer{}, nextID: 1}
}
func (f *fakeCustomerStore) FindByLineUserID(lineUserID string) (*model.Customer, error) {
	c, ok := f.byLine[lineUserID]
	if !ok {
		return nil, ErrNotFound
	}
	return c, nil
}
func (f *fakeCustomerStore) FindOrCreateByLineUserID(lineUserID, name string) (*model.Customer, error) {
	if c, ok := f.byLine[lineUserID]; ok {
		return c, nil
	}
	c := &model.Customer{ID: f.nextID, LineUserID: lineUserID, Name: name, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	f.nextID++
	f.byLine[lineUserID] = c
	return c, nil
}
func (f *fakeCustomerStore) SetPassword(id int64, hash string) error {
	for _, c := range f.byLine {
		if c.ID == id {
			c.PasswordHash = hash
			return nil
		}
	}
	return ErrNotFound
}

func TestCustomerRegistry_註冊與登入(t *testing.T) {
	reg := NewCustomerRegistry(newFakeCustomerStore())
	ctx := context.Background()

	cust, err := reg.Register(ctx, "U_c1", "小明", "pw123")
	if err != nil {
		t.Fatalf("註冊失敗: %v", err)
	}
	if cust.ID == 0 {
		t.Fatal("註冊後無 ID")
	}
	if bcrypt.CompareHashAndPassword([]byte(cust.PasswordHash), []byte("pw123")) != nil {
		t.Fatal("密碼未以 bcrypt 儲存")
	}

	logged, err := reg.Login(ctx, "U_c1", "pw123")
	if err != nil {
		t.Fatalf("登入失敗: %v", err)
	}
	if logged.ID != cust.ID {
		t.Fatalf("登入回傳錯誤乘客: %+v", logged)
	}

	if _, err := reg.Login(ctx, "U_c1", "wrong"); err == nil {
		t.Fatal("錯誤密碼應登入失敗")
	}
	if _, err := reg.Login(ctx, "U_unknown", "pw"); err == nil {
		t.Fatal("不存在乘客應登入失敗")
	}
}
