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

func Test通用Token_簽發與解析(t *testing.T) {
	secret := "test-secret"
	tok, err := GenerateToken("customer", 88, secret, time.Hour)
	if err != nil {
		t.Fatalf("簽發失敗: %v", err)
	}
	role, id, err := ParseToken(tok, secret)
	if err != nil {
		t.Fatalf("解析失敗: %v", err)
	}
	if role != "customer" || id != 88 {
		t.Fatalf("解析結果錯誤: role=%s id=%d", role, id)
	}
}

func Test通用ParseToken_相容既有司機Token(t *testing.T) {
	secret := "test-secret"
	// 用既有司機簽發函式產生的 token，ParseToken 也要能解析為 driver
	tok, err := GenerateDriverToken(3, secret, time.Hour)
	if err != nil {
		t.Fatalf("司機簽發失敗: %v", err)
	}
	role, id, err := ParseToken(tok, secret)
	if err != nil {
		t.Fatalf("解析司機 token 失敗: %v", err)
	}
	if role != "driver" || id != 3 {
		t.Fatalf("司機 token 解析錯誤: role=%s id=%d", role, id)
	}
}

func Test通用ParseToken_錯誤密鑰被拒(t *testing.T) {
	tok, _ := GenerateToken("admin", 1, "secret-a", time.Hour)
	if _, _, err := ParseToken(tok, "secret-b"); err == nil {
		t.Fatal("錯誤密鑰應被拒絕")
	}
}
