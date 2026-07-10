package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"line-fleet-dispatch/internal/model"
	"line-fleet-dispatch/internal/repository"
)

// TestCreateByCustomer_WithDropoff 驗證 dropoff 參數正確寫入 DB
func TestCreateByCustomer_WithDropoff(t *testing.T) {
	ctx := context.Background()
	db := newServiceTestDB(t)

	// 建立測試客戶
	customerRepo := repository.NewCustomerRepository(db)
	now := time.Now()
	customer := &model.Customer{
		LineUserID:   "U_test_dropoff_123",
		Name:         "Test User",
		PasswordHash: "dummy",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	err := db.Create(customer).Error
	require.NoError(t, err)

	// 初始化服務
	rideRepo := repository.NewRideRepository(db)
	redis := newServiceTestRedis(t)

	// 建立完整的 dispatch service（雖然下面的測試不驗證派單結果，但非同步 dispatch 會真的執行）
	driverRepo := repository.NewDriverRepository(db)
	dispatchService := NewDispatchService(
		driverRepo, rideRepo, customerRepo, redis, nil, nil,
		1000, 10, 60, 3,
		nil, // hub (可為 nil)
	)
	rideService := NewRideService(customerRepo, rideRepo, redis, dispatchService)

	// 建立帶 dropoff 的訂單（非同步派單會跑，但由於沒有司機在線，會自動逾時取消）
	ride, err := rideService.CreateByCustomer(
		ctx,
		customer.ID,
		25.033, 121.565, "台北火車站",
		25.034, 121.560, "台北 101",
	)
	require.NoError(t, err)
	require.NotNil(t, ride)

	// 等待非同步派單完成
	time.Sleep(100 * time.Millisecond)

	// 驗證 DB 寫入：讀回訂單並檢查 dropoff
	retrieved, err := rideRepo.GetByID(ride.ID)
	require.NoError(t, err)
	require.NotNil(t, retrieved)

	// 驗證 pickup 座標
	require.InDelta(t, 25.033, retrieved.PickupPoint.Lat, 0.001)
	require.InDelta(t, 121.565, retrieved.PickupPoint.Lng, 0.001)
	require.Equal(t, "台北火車站", retrieved.PickupAddress)

	// 驗證 dropoff 座標（關鍵檢查）
	require.NotNil(t, retrieved.DropoffPoint, "dropoff_point 應不為 nil")
	require.InDelta(t, 25.034, retrieved.DropoffPoint.Lat, 0.001)
	require.InDelta(t, 121.560, retrieved.DropoffPoint.Lng, 0.001)
	require.Equal(t, "台北 101", retrieved.DropoffAddress)
}

// TestCreateByCustomer_WithoutDropoff 驗證在無 dropoff 時仍能建立訂單
func TestCreateByCustomer_WithoutDropoff(t *testing.T) {
	ctx := context.Background()
	db := newServiceTestDB(t)

	// 建立測試客戶
	customerRepo := repository.NewCustomerRepository(db)
	now := time.Now()
	customer := &model.Customer{
		LineUserID:   "U_test_no_dropoff_456",
		Name:         "Test User 2",
		PasswordHash: "dummy",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	err := db.Create(customer).Error
	require.NoError(t, err)

	// 初始化服務
	rideRepo := repository.NewRideRepository(db)
	redis := newServiceTestRedis(t)
	driverRepo := repository.NewDriverRepository(db)
	dispatchService := NewDispatchService(
		driverRepo, rideRepo, customerRepo, redis, nil, nil,
		1000, 10, 60, 3,
		nil,
	)
	rideService := NewRideService(customerRepo, rideRepo, redis, dispatchService)

	// 建立無 dropoff 的訂單（座標全為 0）
	ride, err := rideService.CreateByCustomer(
		ctx,
		customer.ID,
		25.033, 121.565, "台北火車站",
		0, 0, "", // 無 dropoff
	)
	require.NoError(t, err)
	require.NotNil(t, ride)

	// 等待非同步派單完成
	time.Sleep(100 * time.Millisecond)

	// 驗證 DB：dropoff 應為 NULL
	retrieved, err := rideRepo.GetByID(ride.ID)
	require.NoError(t, err)
	require.Nil(t, retrieved.DropoffPoint)
	require.Equal(t, "", retrieved.DropoffAddress)
}

// TestCreateFromLocation_WithDropoff 驗證 LINE webhook 路徑的 dropoff 傳遞
func TestCreateFromLocation_WithDropoff(t *testing.T) {
	ctx := context.Background()
	db := newServiceTestDB(t)

	// 初始化服務
	customerRepo := repository.NewCustomerRepository(db)
	rideRepo := repository.NewRideRepository(db)
	redis := newServiceTestRedis(t)
	driverRepo := repository.NewDriverRepository(db)
	dispatchService := NewDispatchService(
		driverRepo, rideRepo, customerRepo, redis, nil, nil,
		1000, 10, 60, 3,
		nil,
	)
	rideService := NewRideService(customerRepo, rideRepo, redis, dispatchService)

	// 模擬 LINE 下單（帶 dropoff）
	req := RideRequest{
		LineUserID:     "U_line_dropoff_789",
		DisplayName:    "LINE User",
		PickupLat:      25.033,
		PickupLng:      121.565,
		PickupAddress:  "台北火車站",
		DropoffLat:     25.034,
		DropoffLng:     121.560,
		DropoffAddress: "台北 101",
	}

	ride, err := rideService.CreateFromLocation(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, ride)

	// 等待非同步派單完成
	time.Sleep(100 * time.Millisecond)

	// 驗證 dropoff 已寫入
	retrieved, err := rideRepo.GetByID(ride.ID)
	require.NoError(t, err)
	require.NotNil(t, retrieved.DropoffPoint)
	require.InDelta(t, 25.034, retrieved.DropoffPoint.Lat, 0.001)
	require.InDelta(t, 121.560, retrieved.DropoffPoint.Lng, 0.001)
	require.Equal(t, "台北 101", retrieved.DropoffAddress)
}
