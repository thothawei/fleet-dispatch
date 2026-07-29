package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

// CtxRequestID 存放本次請求的識別碼
const CtxRequestID = "request_id"

// HeaderRequestID 請求識別碼的傳遞標頭。上游若已帶入則沿用，讓同一條鏈路跨服務仍是同一個 id。
const HeaderRequestID = "X-Request-Id"

// skipAccessLog 不記 access log 的路徑。/healthz 由容器健康檢查每幾秒打一次，
// 記下來只會把真正有價值的請求洗掉。
var skipAccessLog = map[string]bool{
	"/healthz": true,
}

// RequestLogger 產生 request_id 並記錄每筆 HTTP 請求的結果與耗時。
//
// 沒有這層時，API 變慢或回 5xx 只剩業務層零星的 log，無從得知是哪支端點、花了多久、
// 回了什麼狀態碼——連「先量測再優化」的第一步都做不到。
//
// request_id 同時寫回應標頭，使用者回報問題時可直接拿它對到這筆 access log。
// 注意：業務層目前仍使用全域 logger，其 log 尚未自動帶上 request_id，
// 跨層關聯目前靠業務鍵（多數 log 已帶 ride_id）。
func RequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		rid := c.GetHeader(HeaderRequestID)
		if rid == "" {
			rid = newRequestID()
		}
		c.Set(CtxRequestID, rid)
		c.Header(HeaderRequestID, rid)

		start := time.Now()
		c.Next()

		if skipAccessLog[c.Request.URL.Path] {
			return
		}

		status := c.Writer.Status()
		// 未寫任何 body 時 gin 回 -1，log 裡出現負數位元組數只會讓讀的人困惑。
		size := c.Writer.Size()
		if size < 0 {
			size = 0
		}

		ev := log.Info()
		switch {
		case status >= http.StatusInternalServerError:
			// 5xx 是伺服器自己的問題，一定要有人看。
			ev = log.Error()
		case status >= http.StatusBadRequest:
			// 4xx 多半是呼叫端的問題，但突然變多通常代表某端串接壞了。
			ev = log.Warn()
		}

		ev.Str("request_id", rid).
			Str("method", c.Request.Method).
			Str("path", c.Request.URL.Path).
			Int("status", status).
			Int64("duration_ms", time.Since(start).Milliseconds()).
			Str("client_ip", c.ClientIP()).
			Int("bytes", size).
			Msg("http request")
	}
}

// RequestIDFromCtx 取出中介層放入的 request_id，沒有則回空字串
func RequestIDFromCtx(c *gin.Context) string {
	if v, ok := c.Get(CtxRequestID); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// newRequestID 產生 16 字元隨機 id。取亂數失敗時退回時間戳，
// 讓 access log 一定有識別碼可寫，不因此中斷請求。
func newRequestID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "ts" + time.Now().Format("20060102150405.000")
	}
	return hex.EncodeToString(b)
}
