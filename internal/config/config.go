package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config 從環境變數載入應用設定
type Config struct {
	AppPort int
	AppEnv  string

	DBHost     string
	DBPort     int
	DBName     string
	DBUser     string
	DBPassword string
	// DBStatementTimeoutMs 每條 SQL 的執行上限（毫秒），交由 Postgres 逾時中止，
	// 防跑飛的全表掃描拖垮線上（F9-4）。0 表示不設限。僅套用於 app 連線，migrations 不受影響。
	DBStatementTimeoutMs int

	RedisAddr string

	LineChannelSecret      string
	LineChannelAccessToken string
	LiffID                 string

	OSRMURL string

	JWTSecret      string
	JWTExpiryHours int

	DispatchRadiusM         int
	DispatchMaxDrivers      int
	DispatchOfferTimeoutSec int
	DispatchMaxAttempts     int
	DriverOfflineSec        int

	ETAPushMinIntervalSec int
	ETAPushDistThresholdM int

	TrackPartitionMonthsAhead int
	TrackRetentionMonths      int

	WSWriteWaitSec    int
	WSPongWaitSec     int
	WSMaxMessageBytes int

	AdminSeedUsername string
	AdminSeedPassword string

	// FCMCredentialsFile Firebase 服務帳戶 JSON 路徑（A2 真推播）。
	// 空＝不啟用 FCM，App 推播走 LogPusher stub（派單路徑照走，只是不真的推）。
	FCMCredentialsFile string
}

// Load 讀取環境變數，缺省值對應本地 docker-compose
func Load() (*Config, error) {
	cfg := &Config{
		AppPort:                 getEnvInt("APP_PORT", 8080),
		AppEnv:                  getEnv("APP_ENV", "local"),
		DBHost:                  getEnv("DB_HOST", "localhost"),
		DBPort:                  getEnvInt("DB_PORT", 5432),
		DBName:                  getEnv("DB_NAME", "fleet"),
		DBUser:                  getEnv("DB_USER", "fleet"),
		DBPassword:              getEnv("DB_PASSWORD", "change_me"),
		DBStatementTimeoutMs:    getEnvInt("DB_STATEMENT_TIMEOUT_MS", 10000),
		RedisAddr:               getEnv("REDIS_ADDR", "localhost:6379"),
		LineChannelSecret:       getEnv("LINE_CHANNEL_SECRET", ""),
		LineChannelAccessToken:  getEnv("LINE_CHANNEL_ACCESS_TOKEN", ""),
		LiffID:                  getEnv("LIFF_ID", ""),
		OSRMURL:                 getEnv("OSRM_URL", "http://localhost:5000"),
		JWTSecret:               getEnv("JWT_SECRET", "dev-secret-change-me"),
		JWTExpiryHours:          getEnvInt("JWT_EXPIRY_HOURS", 72),
		DispatchRadiusM:         getEnvInt("DISPATCH_RADIUS_M", 3000),
		DispatchMaxDrivers:      getEnvInt("DISPATCH_MAX_DRIVERS", 5),
		DispatchOfferTimeoutSec: getEnvInt("DISPATCH_OFFER_TIMEOUT_SEC", 20),
		DispatchMaxAttempts:     getEnvInt("DISPATCH_MAX_ATTEMPTS", 3),
		DriverOfflineSec:        getEnvInt("DRIVER_OFFLINE_SEC", 60),
		ETAPushMinIntervalSec:   getEnvInt("ETA_PUSH_MIN_INTERVAL_SEC", 30),
		ETAPushDistThresholdM:   getEnvInt("ETA_PUSH_DIST_THRESHOLD_M", 300),

		TrackPartitionMonthsAhead: getEnvInt("TRACK_PARTITION_MONTHS_AHEAD", 2),
		TrackRetentionMonths:      getEnvInt("TRACK_RETENTION_MONTHS", 0),

		WSWriteWaitSec:    getEnvInt("WS_WRITE_WAIT_SEC", 10),
		WSPongWaitSec:     getEnvInt("WS_PONG_WAIT_SEC", 60),
		WSMaxMessageBytes: getEnvInt("WS_MAX_MESSAGE_BYTES", 4096),

		AdminSeedUsername: getEnv("ADMIN_SEED_USERNAME", ""),
		AdminSeedPassword: getEnv("ADMIN_SEED_PASSWORD", ""),

		FCMCredentialsFile: getEnv("FCM_CREDENTIALS_FILE", ""),
	}
	return cfg, nil
}

func (c *Config) DSN() string {
	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=disable TimeZone=Asia/Taipei",
		c.DBHost, c.DBPort, c.DBUser, c.DBPassword, c.DBName,
	)
	// statement_timeout 為未知連線參數，pgx 會當成 runtime GUC 於連線建立時套用，
	// 使每條 SQL 超時即被 Postgres 中止（F9-4）。migrations 走 MigrateDSN 不受影響。
	if c.DBStatementTimeoutMs > 0 {
		dsn += fmt.Sprintf(" statement_timeout=%d", c.DBStatementTimeoutMs)
	}
	return dsn
}

// MigrateDSN golang-migrate 使用的 postgres URL 格式
func (c *Config) MigrateDSN() string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=disable",
		c.DBUser, c.DBPassword, c.DBHost, c.DBPort, c.DBName,
	)
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}
