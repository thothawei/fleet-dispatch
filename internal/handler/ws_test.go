package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"line-fleet-dispatch/internal/auth"
	"line-fleet-dispatch/internal/events"
)

func newTestWSServer(t *testing.T, secret string) (*httptest.Server, *events.Hub) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	hub := events.NewHub()
	go hub.Run()
	h := NewWSHandler(hub, secret, 10, 60, 4096)
	r := gin.New()
	r.GET("/ws", h.Connect)
	return httptest.NewServer(r), hub
}

func dialWS(t *testing.T, srvURL, token string) *websocket.Conn {
	t.Helper()
	u := "ws" + strings.TrimPrefix(srvURL, "http") + "/ws?token=" + token
	conn, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		t.Fatalf("WS 連線失敗: %v", err)
	}
	return conn
}

func TestWS_合法Token可連線並收到事件(t *testing.T) {
	secret := "ws-secret"
	srv, hub := newTestWSServer(t, secret)
	defer srv.Close()

	tok, _ := auth.GenerateToken(events.RoleCustomer, 7, secret, time.Hour)
	conn := dialWS(t, srv.URL, tok)
	defer conn.Close()

	// 等 handler 完成 register
	time.Sleep(50 * time.Millisecond)
	hub.Publish(events.Recipient{Role: events.RoleCustomer, ID: 7},
		events.Event{Type: events.TypeRideAccepted, RideID: 1})

	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("讀取事件失敗: %v", err)
	}
	if !strings.Contains(string(msg), events.TypeRideAccepted) {
		t.Fatalf("事件內容錯誤: %s", msg)
	}
}

func TestWS_無Token回401(t *testing.T) {
	secret := "ws-secret"
	srv, _ := newTestWSServer(t, secret)
	defer srv.Close()

	u := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	_, resp, err := websocket.DefaultDialer.Dial(u, nil)
	if err == nil {
		t.Fatal("無 token 應連線失敗")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("預期 401，得到: %v", resp)
	}
}
