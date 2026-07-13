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
