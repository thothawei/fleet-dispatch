package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ErrInvalidToken token 無效或過期
var ErrInvalidToken = errors.New("token 無效或已過期")

// DriverClaims 司機身分的 JWT claims
type DriverClaims struct {
	DriverID int64 `json:"driver_id"`
	jwt.RegisteredClaims
}

// GenerateDriverToken 簽發司機 JWT
func GenerateDriverToken(driverID int64, secret string, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := DriverClaims{
		DriverID: driverID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "driver",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// ParseDriverToken 驗證並取出 driver_id
func ParseDriverToken(tokenStr, secret string) (int64, error) {
	claims := &DriverClaims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return []byte(secret), nil
	})
	if err != nil || !token.Valid || claims.DriverID == 0 {
		return 0, ErrInvalidToken
	}
	return claims.DriverID, nil
}

// SubjectClaims 通用角色身分 claims（driver/customer/admin 共用）
type SubjectClaims struct {
	Role      string `json:"role"`
	SubjectID int64  `json:"sub_id"`
	jwt.RegisteredClaims
}

// GenerateToken 簽發通用角色 JWT。
func GenerateToken(role string, id int64, secret string, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := SubjectClaims{
		Role:      role,
		SubjectID: id,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   role,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// ParseToken 解析通用角色 JWT；為相容既有司機 token（DriverClaims），
// 若通用解析取不到 role/sub_id，退回以 ParseDriverToken 視為 driver。
func ParseToken(tokenStr, secret string) (string, int64, error) {
	claims := &SubjectClaims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return []byte(secret), nil
	})
	if err == nil && token.Valid && claims.Role != "" && claims.SubjectID != 0 {
		return claims.Role, claims.SubjectID, nil
	}
	// 相容既有司機 token（同一把密鑰簽發；錯誤密鑰會在此一併被拒）
	if driverID, derr := ParseDriverToken(tokenStr, secret); derr == nil {
		return "driver", driverID, nil
	}
	return "", 0, ErrInvalidToken
}
