package database

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const startupRetries = 30

// Connect 建立 GORM 連線（含重試，等 docker-compose 依賴就緒）
func Connect(dsn string, appEnv string) (*gorm.DB, error) {
	logLevel := logger.Info
	if appEnv == "production" {
		logLevel = logger.Warn
	}

	var db *gorm.DB
	var err error
	for i := 0; i < startupRetries; i++ {
		db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{
			Logger: logger.Default.LogMode(logLevel),
		})
		if err == nil {
			sqlDB, pingErr := db.DB()
			if pingErr == nil {
				pingErr = sqlDB.Ping()
			}
			if pingErr == nil {
				return db, nil
			}
			err = pingErr
		}
		time.Sleep(time.Second)
	}
	return nil, fmt.Errorf("連線 PostgreSQL 失敗: %w", err)
}

// RunMigrations 執行 golang-migrate（含重試）
func RunMigrations(dsn, migrationsPath string) error {
	var lastErr error
	for i := 0; i < startupRetries; i++ {
		lastErr = runMigrationsOnce(dsn, migrationsPath)
		if lastErr == nil {
			return nil
		}
		time.Sleep(time.Second)
	}
	return lastErr
}

func runMigrationsOnce(dsn, migrationsPath string) error {
	m, err := migrate.New("file://"+migrationsPath, dsn)
	if err != nil {
		return fmt.Errorf("建立 migrate 失敗: %w", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migration 失敗: %w", err)
	}
	return nil
}
