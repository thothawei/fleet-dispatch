package service

import "testing"

func TestDeviceTokenService_Register驗證(t *testing.T) {
	svc := NewDeviceTokenService(nil)

	if err := svc.Register("driver", 1, "fcm", ""); err != ErrInvalidDeviceToken {
		t.Fatalf("空 token 預期 ErrInvalidDeviceToken，得到 %v", err)
	}
	if err := svc.Register("driver", 1, "web", "abc"); err != ErrInvalidDeviceToken {
		t.Fatalf("非法 platform 預期 ErrInvalidDeviceToken，得到 %v", err)
	}
	if err := svc.Register("admin", 1, "fcm", "abc"); err != ErrInvalidDeviceToken {
		t.Fatalf("非法 role 預期 ErrInvalidDeviceToken，得到 %v", err)
	}
}
