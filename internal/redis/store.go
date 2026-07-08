package redisstore

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	driversGeoKey = "drivers:geo"
)

// Store Redis GEO、搶單鎖、限流封裝
type Store struct {
	client           *redis.Client
	driverOfflineSec int
}

func NewStore(client *redis.Client, driverOfflineSec int) *Store {
	return &Store{client: client, driverOfflineSec: driverOfflineSec}
}

// UpdateDriverLocation 更新司機即時位置
func (s *Store) UpdateDriverLocation(ctx context.Context, driverID int64, lat, lng float64) error {
	idStr := strconv.FormatInt(driverID, 10)
	pipe := s.client.Pipeline()
	pipe.GeoAdd(ctx, driversGeoKey, &redis.GeoLocation{
		Name:      idStr,
		Longitude: lng,
		Latitude:  lat,
	})
	pipe.HSet(ctx, fmt.Sprintf("driver:%s:loc", idStr), map[string]interface{}{
		"lat":        lat,
		"lng":        lng,
		"updated_at": time.Now().Unix(),
	})
	_, err := pipe.Exec(ctx)
	return err
}

// NearbyDriverIDs 找 pickup 半徑內最近的待命司機（過濾離線）
func (s *Store) NearbyDriverIDs(ctx context.Context, lat, lng float64, radiusM, count int) ([]int64, error) {
	results, err := s.client.GeoSearch(ctx, driversGeoKey, &redis.GeoSearchQuery{
		Longitude:  lng,
		Latitude:   lat,
		Radius:     float64(radiusM),
		RadiusUnit: "m",
		Sort:       "ASC",
		Count:      count * 3, // 多取一些再過濾離線
	}).Result()
	if err != nil {
		return nil, err
	}

	cutoff := time.Now().Add(-time.Duration(s.driverOfflineSec) * time.Second).Unix()
	var ids []int64
	for _, idStr := range results {
		if len(ids) >= count {
			break
		}
		updatedAt, err := s.client.HGet(ctx, fmt.Sprintf("driver:%s:loc", idStr), "updated_at").Int64()
		if err != nil || updatedAt < cutoff {
			continue
		}
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			continue
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// TryLockRide 搶單鎖，成功回傳 true
func (s *Store) TryLockRide(ctx context.Context, rideID, driverID int64) (bool, error) {
	key := fmt.Sprintf("ride:%d:lock", rideID)
	val := strconv.FormatInt(driverID, 10)
	ok, err := s.client.SetNX(ctx, key, val, 30*time.Second).Result()
	return ok, err
}

// ReleaseRideLock 釋放搶單鎖
func (s *Store) ReleaseRideLock(ctx context.Context, rideID int64) {
	s.client.Del(ctx, fmt.Sprintf("ride:%d:lock", rideID))
}

// RejectRideDriver 記錄「此司機拒接本單」，重派時會被跳過
func (s *Store) RejectRideDriver(ctx context.Context, rideID, driverID int64) error {
	key := fmt.Sprintf("ride:%d:rejected", rideID)
	if err := s.client.SAdd(ctx, key, driverID).Err(); err != nil {
		return err
	}
	return s.client.Expire(ctx, key, 30*time.Minute).Err()
}

// RejectedDrivers 取得本單已拒接的司機集合
func (s *Store) RejectedDrivers(ctx context.Context, rideID int64) map[int64]bool {
	m := map[int64]bool{}
	vals, err := s.client.SMembers(ctx, fmt.Sprintf("ride:%d:rejected", rideID)).Result()
	if err != nil {
		return m
	}
	for _, v := range vals {
		if id, e := strconv.ParseInt(v, 10, 64); e == nil {
			m[id] = true
		}
	}
	return m
}

// ClearRejected 清掉本單的拒接集合（取消/完成時）
func (s *Store) ClearRejected(ctx context.Context, rideID int64) {
	s.client.Del(ctx, fmt.Sprintf("ride:%d:rejected", rideID))
}

// GetDriverLocation 取得司機最新位置
func (s *Store) GetDriverLocation(ctx context.Context, driverID int64) (lat, lng float64, ok bool) {
	idStr := strconv.FormatInt(driverID, 10)
	m, err := s.client.HGetAll(ctx, fmt.Sprintf("driver:%s:loc", idStr)).Result()
	if err != nil || len(m) == 0 {
		return 0, 0, false
	}
	lat, _ = strconv.ParseFloat(m["lat"], 64)
	lng, _ = strconv.ParseFloat(m["lng"], 64)
	return lat, lng, lat != 0 || lng != 0
}

// DriverLoc 後台車隊快照用：單一司機的即時座標
type DriverLoc struct {
	DriverID  int64   `json:"driver_id"`
	Lat       float64 `json:"lat"`
	Lng       float64 `json:"lng"`
	UpdatedAt int64   `json:"updated_at"`
}

// OnlineDriverLocations 列出所有「未離線」司機的即時座標（後台車隊快照）。
// 從 drivers:geo（zset）取全體成員，再以各司機 hash 的 updated_at 過濾離線。
func (s *Store) OnlineDriverLocations(ctx context.Context) ([]DriverLoc, error) {
	ids, err := s.client.ZRange(ctx, driversGeoKey, 0, -1).Result()
	if err != nil {
		return nil, err
	}
	cutoff := time.Now().Add(-time.Duration(s.driverOfflineSec) * time.Second).Unix()
	var out []DriverLoc
	for _, idStr := range ids {
		m, err := s.client.HGetAll(ctx, fmt.Sprintf("driver:%s:loc", idStr)).Result()
		if err != nil || len(m) == 0 {
			continue
		}
		updatedAt, _ := strconv.ParseInt(m["updated_at"], 10, 64)
		if updatedAt < cutoff {
			continue
		}
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			continue
		}
		lat, _ := strconv.ParseFloat(m["lat"], 64)
		lng, _ := strconv.ParseFloat(m["lng"], 64)
		out = append(out, DriverLoc{DriverID: id, Lat: lat, Lng: lng, UpdatedAt: updatedAt})
	}
	return out, nil
}

// RemoveDriverLocation 從 GEO 與位置 hash 移除司機（後台停用時移出派單池）。
func (s *Store) RemoveDriverLocation(ctx context.Context, driverID int64) error {
	idStr := strconv.FormatInt(driverID, 10)
	pipe := s.client.Pipeline()
	pipe.ZRem(ctx, driversGeoKey, idStr)
	pipe.Del(ctx, fmt.Sprintf("driver:%s:loc", idStr))
	_, err := pipe.Exec(ctx)
	return err
}

// AllowRateLimit 叫車限流，回傳是否允許
func (s *Store) AllowRateLimit(ctx context.Context, lineUserID string, maxPerMin int) (bool, error) {
	key := fmt.Sprintf("ratelimit:%s", lineUserID)
	n, err := s.client.Incr(ctx, key).Result()
	if err != nil {
		return false, err
	}
	if n == 1 {
		s.client.Expire(ctx, key, 60*time.Second)
	}
	return n <= int64(maxPerMin), nil
}
