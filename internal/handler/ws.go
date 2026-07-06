package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"

	"line-fleet-dispatch/internal/auth"
	"line-fleet-dispatch/internal/events"
)

// WSHandler 處理 WebSocket 升級與連線生命週期。
type WSHandler struct {
	hub         *events.Hub
	jwtSecret   string
	writeWait   time.Duration
	pongWait    time.Duration
	pingPeriod  time.Duration
	maxMsgBytes int64
	upgrader    websocket.Upgrader
}

func NewWSHandler(hub *events.Hub, jwtSecret string, writeWaitSec, pongWaitSec, maxMsgBytes int) *WSHandler {
	pong := time.Duration(pongWaitSec) * time.Second
	return &WSHandler{
		hub:         hub,
		jwtSecret:   jwtSecret,
		writeWait:   time.Duration(writeWaitSec) * time.Second,
		pongWait:    pong,
		pingPeriod:  (pong * 9) / 10,
		maxMsgBytes: int64(maxMsgBytes),
		upgrader: websocket.Upgrader{
			// 作品階段允許跨來源（App/後台不同網域）；正式環境應收斂白名單
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

// Connect GET /ws?token=<jwt>：驗證後升級連線，訂閱屬於自己角色/ID 的事件。
func (h *WSHandler) Connect(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		if hdr := c.GetHeader("Authorization"); len(hdr) > 7 && hdr[:7] == "Bearer " {
			token = hdr[7:]
		}
	}
	role, id, err := auth.ParseToken(token, h.jwtSecret)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "token 無效或已過期"})
		return
	}

	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Error().Err(err).Msg("WebSocket 升級失敗")
		return
	}

	client := &events.Client{
		Rec:  events.Recipient{Role: role, ID: id},
		Send: make(chan []byte, 64),
	}
	h.hub.Register(client)
	log.Info().Str("role", role).Int64("id", id).Msg("WebSocket 已連線")

	go h.writePump(conn, client)
	h.readPump(conn, client)
}

// readPump 讀取（僅處理 pong / close），連線結束則註銷。
func (h *WSHandler) readPump(conn *websocket.Conn, client *events.Client) {
	defer func() {
		h.hub.Unregister(client)
		_ = conn.Close()
	}()
	conn.SetReadLimit(h.maxMsgBytes)
	_ = conn.SetReadDeadline(time.Now().Add(h.pongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(h.pongWait))
	})
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}

// writePump 送出 Hub 事件並定期 ping 保活。
func (h *WSHandler) writePump(conn *websocket.Conn, client *events.Client) {
	ticker := time.NewTicker(h.pingPeriod)
	defer ticker.Stop()
	for {
		select {
		case msg, ok := <-client.Send:
			_ = conn.SetWriteDeadline(time.Now().Add(h.writeWait))
			if !ok { // Hub 已關閉此連線
				_ = conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = conn.SetWriteDeadline(time.Now().Add(h.writeWait))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
