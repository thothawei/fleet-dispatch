package service

import (
	"errors"
	"testing"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/repository"
)

// TestAuthorizeTrackAccess 驗證軌跡端點的多角色擁有權授權：
// 本趟乘客、被指派司機、admin 皆可；他人乘客/司機被拒；訂單不存在回 NotFound。
func TestAuthorizeTrackAccess(t *testing.T) {
	db := newServiceTestDB(t)
	customers := repository.NewCustomerRepository(db)
	drivers := repository.NewDriverRepository(db)
	tracks := repository.NewTrackRepository(db)
	rides := repository.NewRideRepository(db)

	owner, err := customers.FindOrCreateByLineUserID("U_track_owner", "本人乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	stranger, err := customers.FindOrCreateByLineUserID("U_track_stranger", "他人乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	assignedDriver, err := drivers.FindOrCreate("U_track_driver", "指派司機")
	if err != nil {
		t.Fatalf("建立司機失敗：%v", err)
	}
	otherDriver, err := drivers.FindOrCreate("U_track_driver2", "他人司機")
	if err != nil {
		t.Fatalf("建立司機失敗：%v", err)
	}

	// 以 Requested 建單，再由 AcceptRide 指派司機（其 WHERE 守門僅接受 Requested/Assigned）
	ride := newTestRide(t, rides, owner.ID, constants.RideStatusRequested)
	if err := rides.AcceptRide(ride.ID, assignedDriver.ID, 300); err != nil {
		t.Fatalf("指派司機失敗：%v", err)
	}

	q := NewRideQueryService(tracks, rides)

	cases := []struct {
		name    string
		role    string
		subject int64
		wantErr error
	}{
		{"本人乘客可存取", "customer", owner.ID, nil},
		{"被指派司機可存取", "driver", assignedDriver.ID, nil},
		{"admin可存取", "admin", 999, nil},
		{"他人乘客被拒", "customer", stranger.ID, ErrForbidden},
		{"他人司機被拒", "driver", otherDriver.ID, ErrForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := q.AuthorizeTrackAccess(tc.role, tc.subject, ride.ID)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("預期 %v，得到 %v", tc.wantErr, err)
			}
		})
	}

	t.Run("不存在訂單回NotFound", func(t *testing.T) {
		err := q.AuthorizeTrackAccess("admin", 1, 999999)
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("預期 ErrNotFound，得到 %v", err)
		}
	})
}
