package repository

import (
	"testing"

	"line-fleet-dispatch/internal/notify"
)

// 同一支裝置 token 換人登入後，**舊主人身上的那一列必須消失**。
//
// 為什麼會有舊列：FCM token 是「這台裝置上的這個 App」的識別，換人登入時它不會變；
// 而 App 在 session 失效（401）那條路徑**刻意不呼叫註銷**（那支 API 只會再回一次 401）。
// 不清的話，下一位登入的人會收到前一位的行程通知與**對話內容預覽**（推播本文帶訊息文字）。
func TestDeviceToken_同一token換人登入要移轉(t *testing.T) {
	db := newMigratedTestDB(t)
	repo := NewDeviceTokenRepository(db)
	const tok = "SAME-DEVICE-TOKEN"

	if err := repo.Upsert(notify.RoleCustomer, 1, notify.PlatformFCM, tok); err != nil {
		t.Fatalf("第一位註冊失敗：%v", err)
	}
	if err := repo.Upsert(notify.RoleCustomer, 2, notify.PlatformFCM, tok); err != nil {
		t.Fatalf("第二位註冊失敗：%v", err)
	}

	first, err := repo.ListBySubject(notify.RoleCustomer, 1)
	if err != nil {
		t.Fatalf("查第一位裝置失敗：%v", err)
	}
	if len(first) != 0 {
		t.Errorf("前一位主人身上不該還有這支 token（會收到別人的推播）：%v", first)
	}

	second, err := repo.ListBySubject(notify.RoleCustomer, 2)
	if err != nil {
		t.Fatalf("查第二位裝置失敗：%v", err)
	}
	if len(second) != 1 || second[0].Token != tok {
		t.Errorf("新主人應拿到這支 token：%v", second)
	}
}

// 跨角色也一樣：同一支手機先登司機、後登乘客（雙 flavor 共機時可能同 token）。
func TestDeviceToken_跨角色也要移轉(t *testing.T) {
	db := newMigratedTestDB(t)
	repo := NewDeviceTokenRepository(db)
	const tok = "CROSS-ROLE-TOKEN"

	if err := repo.Upsert(notify.RoleDriver, 9, notify.PlatformFCM, tok); err != nil {
		t.Fatalf("司機註冊失敗：%v", err)
	}
	if err := repo.Upsert(notify.RoleCustomer, 9, notify.PlatformFCM, tok); err != nil {
		t.Fatalf("乘客註冊失敗：%v", err)
	}

	driverDevices, err := repo.ListBySubject(notify.RoleDriver, 9)
	if err != nil {
		t.Fatalf("查司機裝置失敗：%v", err)
	}
	if len(driverDevices) != 0 {
		t.Errorf("司機身上不該還有這支 token：%v", driverDevices)
	}
}

// 同一位主體重複註冊同一支 token 是冪等的（App 每次登入都會註冊一次）。
func TestDeviceToken_同一人重複註冊冪等(t *testing.T) {
	db := newMigratedTestDB(t)
	repo := NewDeviceTokenRepository(db)
	const tok = "IDEMPOTENT-TOKEN"

	for i := 0; i < 3; i++ {
		if err := repo.Upsert(notify.RoleDriver, 5, notify.PlatformFCM, tok); err != nil {
			t.Fatalf("第 %d 次註冊失敗：%v", i+1, err)
		}
	}
	devices, err := repo.ListBySubject(notify.RoleDriver, 5)
	if err != nil {
		t.Fatalf("查裝置失敗：%v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("重複註冊不該長出多列，得到 %d 列", len(devices))
	}
}

// 一個人可以有多台裝置——移轉只針對「同一支 token」，不可誤刪他其他裝置。
func TestDeviceToken_多裝置不受影響(t *testing.T) {
	db := newMigratedTestDB(t)
	repo := NewDeviceTokenRepository(db)

	if err := repo.Upsert(notify.RoleCustomer, 3, notify.PlatformFCM, "PHONE-A"); err != nil {
		t.Fatalf("註冊 A 失敗：%v", err)
	}
	if err := repo.Upsert(notify.RoleCustomer, 3, notify.PlatformFCM, "PHONE-B"); err != nil {
		t.Fatalf("註冊 B 失敗：%v", err)
	}
	// 另一個人在 PHONE-B 上登入 → 只該搬走 B。
	if err := repo.Upsert(notify.RoleCustomer, 4, notify.PlatformFCM, "PHONE-B"); err != nil {
		t.Fatalf("換人註冊 B 失敗：%v", err)
	}

	devices, err := repo.ListBySubject(notify.RoleCustomer, 3)
	if err != nil {
		t.Fatalf("查裝置失敗：%v", err)
	}
	if len(devices) != 1 || devices[0].Token != "PHONE-A" {
		t.Fatalf("只該搬走 PHONE-B，得到 %v", devices)
	}
}
