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

	RedisAddr string

	LineChannelSecret      string
	LineChannelAccessToken string
	LiffID                 string

	OSRMURL string

	DispatchRadiusM    int
	DispatchMaxDrivers int
	DriverOfflineSec   int
}

// Load 讀取環境變數，缺省值對應本地 docker-compose
func Load() (*Config, error) {
	cfg := &Config{
		AppPort:                getEnvInt("APP_PORT", 8080),
		AppEnv:                 getEnv("APP_ENV", "local"),
		DBHost:                 getEnv("DB_HOST", "localhost"),
		DBPort:                 getEnvInt("DB_PORT", 5432),
		DBName:                 getEnv("DB_NAME", "fleet"),
		DBUser:                 getEnv("DB_USER", "fleet"),
		DBPassword:             getEnv("DB_PASSWORD", "change_me"),
		RedisAddr:              getEnv("REDIS_ADDR", "localhost:6379"),
		LineChannelSecret:      getEnv("LINE_CHANNEL_SECRET", ""),
		LineChannelAccessToken: getEnv("LINE_CHANNEL_ACCESS_TOKEN", ""),
		LiffID:                 getEnv("LIFF_ID", ""),
		OSRMURL:                getEnv("OSRM_URL", "http://localhost:5000"),
		DispatchRadiusM:        getEnvInt("DISPATCH_RADIUS_M", 3000),
		DispatchMaxDrivers:     getEnvInt("DISPATCH_MAX_DRIVERS", 5),
		DriverOfflineSec:       getEnvInt("DRIVER_OFFLINE_SEC", 60),
	}
	return cfg, nil
}

func (c *Config) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=disable TimeZone=Asia/Taipei",
		c.DBHost, c.DBPort, c.DBUser, c.DBPassword, c.DBName,
	)
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
