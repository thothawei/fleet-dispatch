package config

import (
	"strings"
	"testing"
)

func baseConfig() *Config {
	return &Config{DBHost: "h", DBPort: 5432, DBUser: "u", DBPassword: "p", DBName: "d"}
}

func TestDSN_含statement_timeout(t *testing.T) {
	c := baseConfig()
	c.DBStatementTimeoutMs = 10000
	dsn := c.DSN()
	if !strings.Contains(dsn, "statement_timeout=10000") {
		t.Fatalf("DSN 應含 statement_timeout=10000：%s", dsn)
	}
}

func TestDSN_逾時為0時不加參數(t *testing.T) {
	c := baseConfig()
	c.DBStatementTimeoutMs = 0
	if strings.Contains(c.DSN(), "statement_timeout") {
		t.Fatalf("逾時為 0 不應加 statement_timeout：%s", c.DSN())
	}
}

func TestMigrateDSN_不受statement_timeout影響(t *testing.T) {
	c := baseConfig()
	c.DBStatementTimeoutMs = 10000
	if strings.Contains(c.MigrateDSN(), "statement_timeout") {
		t.Fatalf("migrations 連線不應套用 statement_timeout：%s", c.MigrateDSN())
	}
}

// 沒設 REDIS_ADDR 時的缺省值刻意是 6380，不是 redis 的標準埠 6379。
//
// 這個缺省值只有「服務跑在主機、又忘了設 REDIS_ADDR」時才會用到，而那正是會靜默連到
// 開發機自己那台 redis-server 的情境：兩台都 ping 得通，所以不會有任何錯誤，
// 只會讓限流／派單狀態寫進另一台，在容器裡怎麼查都查不到（見 docker-compose 的說明）。
// 這條測試釘的是那個決定——看到 6380 覺得「怪，改回標準埠吧」的人會在這裡被擋下來。
func TestLoad_Redis缺省埠是6380而非6379(t *testing.T) {
	t.Setenv("REDIS_ADDR", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load 失敗：%v", err)
	}
	if cfg.RedisAddr != "localhost:6380" {
		t.Fatalf("缺省 RedisAddr 應為 localhost:6380（避開本機 redis-server 的 6379），實際：%s", cfg.RedisAddr)
	}
}

func TestLoad_有設REDIS_ADDR就照用(t *testing.T) {
	t.Setenv("REDIS_ADDR", "redis:6379")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load 失敗：%v", err)
	}
	// docker compose 內就是這個值——缺省值的改動不可以影響已明設的情況。
	if cfg.RedisAddr != "redis:6379" {
		t.Fatalf("明設 REDIS_ADDR 應照用，實際：%s", cfg.RedisAddr)
	}
}
