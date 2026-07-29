package middleware_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"line-fleet-dispatch/internal/middleware"
)

// captureLog 把全域 logger 導向 buffer 並回傳還原函式，
// 讓測試能直接斷言實際輸出的 JSON 欄位，而不只是「有沒有被呼叫」。
func captureLog(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	buf := &bytes.Buffer{}
	orig := log.Logger
	origLevel := zerolog.GlobalLevel()
	log.Logger = zerolog.New(buf)
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	return buf, func() {
		log.Logger = orig
		zerolog.SetGlobalLevel(origLevel)
	}
}

func lastLogLine(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	raw := bytes.TrimSpace(buf.Bytes())
	if len(raw) == 0 {
		t.Fatal("沒有任何 log 輸出")
	}
	lines := bytes.Split(raw, []byte("\n"))
	var m map[string]any
	if err := json.Unmarshal(lines[len(lines)-1], &m); err != nil {
		t.Fatalf("log 不是合法 JSON：%v（原文 %s）", err, lines[len(lines)-1])
	}
	return m
}

func TestRequestLogger_GeneratesAndReturnsRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	buf, restore := captureLog(t)
	defer restore()

	var seenInHandler string
	r := gin.New()
	r.Use(middleware.RequestLogger())
	r.GET("/x", func(c *gin.Context) {
		seenInHandler = middleware.RequestIDFromCtx(c)
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))

	header := w.Header().Get(middleware.HeaderRequestID)
	if header == "" {
		t.Fatal("回應標頭沒有帶 X-Request-Id")
	}
	if seenInHandler != header {
		t.Fatalf("handler 取到的 request_id（%s）與回應標頭（%s）不一致", seenInHandler, header)
	}

	entry := lastLogLine(t, buf)
	if entry["request_id"] != header {
		t.Fatalf("access log 的 request_id 應為 %s，得到 %v", header, entry["request_id"])
	}
	if entry["path"] != "/x" || entry["method"] != http.MethodGet {
		t.Fatalf("access log 未正確記錄路徑／方法：%v", entry)
	}
	if _, ok := entry["duration_ms"]; !ok {
		t.Fatal("access log 缺少 duration_ms，無法用於量測端點耗時")
	}
	if entry["status"] != float64(http.StatusOK) {
		t.Fatalf("status 應為 200，得到 %v", entry["status"])
	}
}

func TestRequestLogger_ReusesUpstreamRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	buf, restore := captureLog(t)
	defer restore()

	r := gin.New()
	r.Use(middleware.RequestLogger())
	r.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set(middleware.HeaderRequestID, "upstream-abc123")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if got := w.Header().Get(middleware.HeaderRequestID); got != "upstream-abc123" {
		t.Fatalf("應沿用上游 request_id，得到 %s", got)
	}
	if entry := lastLogLine(t, buf); entry["request_id"] != "upstream-abc123" {
		t.Fatalf("access log 應沿用上游 request_id，得到 %v", entry["request_id"])
	}
}

func TestRequestLogger_LevelByStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name      string
		status    int
		wantLevel string
	}{
		{"2xx 記 info", http.StatusOK, "info"},
		{"4xx 記 warn", http.StatusBadRequest, "warn"},
		{"5xx 記 error", http.StatusInternalServerError, "error"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buf, restore := captureLog(t)
			defer restore()

			r := gin.New()
			r.Use(middleware.RequestLogger())
			r.GET("/x", func(c *gin.Context) { c.Status(tc.status) })

			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))

			if entry := lastLogLine(t, buf); entry["level"] != tc.wantLevel {
				t.Fatalf("status %d 應記為 %s，得到 %v", tc.status, tc.wantLevel, entry["level"])
			}
		})
	}
}

// RequestLogger 掛在 Recovery 之外層，panic 轉成 500 之後這筆仍要進 access log——
// 否則最該被看到的請求剛好沒有紀錄。
func TestRequestLogger_LogsPanicRecoveredAs500(t *testing.T) {
	gin.SetMode(gin.TestMode)
	buf, restore := captureLog(t)
	defer restore()

	r := gin.New()
	r.Use(middleware.RequestLogger())
	r.Use(gin.Recovery())
	r.GET("/boom", func(c *gin.Context) { panic("測試用 panic") })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/boom", nil))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("panic 應由 Recovery 轉成 500，得到 %d", w.Code)
	}
	entry := lastLogLine(t, buf)
	if entry["level"] != "error" {
		t.Fatalf("500 應記為 error，得到 %v", entry["level"])
	}
	if entry["path"] != "/boom" || entry["status"] != float64(500) {
		t.Fatalf("access log 未正確記錄 panic 請求：%v", entry)
	}
}

func TestRequestLogger_SkipsHealthz(t *testing.T) {
	gin.SetMode(gin.TestMode)
	buf, restore := captureLog(t)
	defer restore()

	r := gin.New()
	r.Use(middleware.RequestLogger())
	r.GET("/healthz", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

	// 先用一般路徑校準：確認這條斷言路徑真的抓得到輸出，
	// 否則「buf 是空的」可能只是因為 logger 根本沒接上，負向斷言會永遠 PASS。
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
	if buf.Len() == 0 {
		t.Fatal("校準失敗：一般路徑也沒寫出 access log，本測試無法證明 /healthz 被跳過")
	}
	buf.Reset()

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if buf.Len() != 0 {
		t.Fatalf("/healthz 不應寫 access log，卻輸出了：%s", buf.String())
	}
	// 仍需回傳 request_id，健康檢查失敗時才追得到。
	if w.Header().Get(middleware.HeaderRequestID) == "" {
		t.Fatal("/healthz 仍應回傳 X-Request-Id")
	}
}
