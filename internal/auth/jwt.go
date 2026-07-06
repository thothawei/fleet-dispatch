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
