package middleware

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
)

// LineSignature 驗證 LINE Webhook X-Line-Signature
func LineSignature(channelSecret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if channelSecret == "" {
			// 本地未設定 secret 時跳過，方便 curl 測試
			c.Next()
			return
		}

		signature := c.GetHeader("X-Line-Signature")
		if signature == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "缺少 X-Line-Signature"})
			return
		}

		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "讀取 body 失敗"})
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewBuffer(body))

		mac := hmac.New(sha256.New, []byte(channelSecret))
		mac.Write(body)
		expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))

		if !hmac.Equal([]byte(signature), []byte(expected)) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "簽章驗證失敗"})
			return
		}

		c.Next()
	}
}
