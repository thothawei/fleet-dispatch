package auth

import (
	"testing"
	"time"
)

func TestDriverTokenRoundTrip(t *testing.T) {
	secret := "test-secret"
	tok, err := GenerateDriverToken(42, secret, time.Hour)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	id, err := ParseDriverToken(tok, secret)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if id != 42 {
		t.Fatalf("driver id = %d, want 42", id)
	}
}

func TestDriverTokenWrongSecret(t *testing.T) {
	tok, _ := GenerateDriverToken(1, "secret-a", time.Hour)
	if _, err := ParseDriverToken(tok, "secret-b"); err == nil {
		t.Fatal("以錯誤 secret 驗證應失敗，但通過了")
	}
}

func TestDriverTokenExpired(t *testing.T) {
	tok, _ := GenerateDriverToken(1, "s", -time.Hour)
	if _, err := ParseDriverToken(tok, "s"); err == nil {
		t.Fatal("過期 token 應失敗，但通過了")
	}
}

func TestDriverTokenGarbage(t *testing.T) {
	if _, err := ParseDriverToken("not-a-jwt", "s"); err == nil {
		t.Fatal("亂碼 token 應失敗，但通過了")
	}
}
