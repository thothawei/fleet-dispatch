package middleware_test

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"line-fleet-dispatch/internal/middleware"
)

func TestLineSignature_Valid(t *testing.T) {
	gin.SetMode(gin.TestMode)
	secret := "test_channel_secret"
	body := []byte(`{"events":[]}`)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	r := gin.New()
	r.POST("/webhook/line", middleware.LineSignature(secret), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/webhook/line", bytes.NewReader(body))
	req.Header.Set("X-Line-Signature", sig)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，得到 %d", w.Code)
	}
}

func TestLineSignature_Invalid(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"events":[]}`)

	r := gin.New()
	r.POST("/webhook/line", middleware.LineSignature("secret"), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/webhook/line", bytes.NewReader(body))
	req.Header.Set("X-Line-Signature", "invalid")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("期望 401，得到 %d", w.Code)
	}
}
