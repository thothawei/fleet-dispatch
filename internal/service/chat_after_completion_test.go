package service

import (
	"errors"
	"testing"
	"time"

	"line-fleet-dispatch/internal/constants"
	"line-fleet-dispatch/internal/events"
	"line-fleet-dispatch/internal/model"
	"line-fleet-dispatch/internal/repository"
)

// TestChat_行程完成很久之後仍可對話 O7 的「留言板」＝沿用既有 ride_messages，不另建一套。
// 規格要求實作前先釘住這個行為：乘客常常是幾天後才發現東西掉了，
// 那時行程早已完成，對話入口必須還能用（遺失物協尋 H2 就靠這條）。
//
// 現行 authorizeRideParticipant 只看「是不是本趟參與者」，不看狀態也不看時間——
// 這個測試把它變成有意的保證，而不是「碰巧沒擋」。
func TestChat_行程完成很久之後仍可對話(t *testing.T) {
	db := newServiceTestDB(t)
	drivers := repository.NewDriverRepository(db)
	rides := repository.NewRideRepository(db)
	customers := repository.NewCustomerRepository(db)
	messages := repository.NewRideMessageRepository(db)
	fp := &fakePublisher{}
	svc := NewChatService(rides, messages, fp)

	cust, err := customers.FindOrCreateByLineUserID("U_chat_old", "乘客")
	if err != nil {
		t.Fatalf("建立乘客失敗：%v", err)
	}
	driver, err := drivers.FindOrCreate("D_chat_old", "司機")
	if err != nil {
		t.Fatalf("建立司機失敗：%v", err)
	}

	// 一趟 90 天前就完成的行程。
	long := time.Now().AddDate(0, 0, -90)
	ride := &model.Ride{
		CustomerID: cust.ID, DriverID: &driver.ID,
		Status:      constants.RideStatusCompleted,
		PickupPoint: model.GeoPoint{Lat: 25.03, Lng: 121.56}, PickupAddress: "台北車站",
		RequestedAt: long, CompletedAt: &long, CreatedAt: long, UpdatedAt: long,
	}
	if err := db.Create(ride).Error; err != nil {
		t.Fatalf("建立行程失敗：%v", err)
	}

	t.Run("乘客仍可發話", func(t *testing.T) {
		msg, err := svc.Send(events.RoleCustomer, cust.ID, ride.ID, "請問有看到我的傘嗎？")
		if err != nil {
			t.Fatalf("完成 90 天後乘客仍應可發話：%v", err)
		}
		if msg.SenderRole != events.RoleCustomer {
			t.Fatalf("sender_role 錯誤：%q", msg.SenderRole)
		}
	})

	t.Run("司機仍可回話", func(t *testing.T) {
		if _, err := svc.Send(events.RoleDriver, driver.ID, ride.ID, "有的，明天拿給你"); err != nil {
			t.Fatalf("完成 90 天後司機仍應可回話：%v", err)
		}
	})

	t.Run("歷史讀得到", func(t *testing.T) {
		list, err := svc.List(events.RoleCustomer, cust.ID, ride.ID, 0, 50)
		if err != nil {
			t.Fatalf("讀歷史失敗：%v", err)
		}
		if len(list) != 2 {
			t.Fatalf("預期 2 則訊息，得到 %d", len(list))
		}
	})

	t.Run("非參與者仍被擋", func(t *testing.T) {
		// 「沒有時間限制」不等於「沒有授權」——路人不得插話或偷看。
		if _, err := svc.Send(events.RoleCustomer, 999999, ride.ID, "路人"); !errors.Is(err, ErrForbidden) {
			t.Fatalf("非本趟乘客預期 ErrForbidden，得到 %v", err)
		}
		if _, err := svc.List(events.RoleDriver, 999999, ride.ID, 0, 50); !errors.Is(err, ErrForbidden) {
			t.Fatalf("非本趟司機預期 ErrForbidden，得到 %v", err)
		}
	})
}
