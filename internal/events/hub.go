package events

import "github.com/rs/zerolog/log"

// Client 一條 WebSocket 連線在 Hub 中的代表。
type Client struct {
	Rec  Recipient
	Send chan []byte // Hub → 連線的出站佇列
}

type publishReq struct {
	rec Recipient
	ev  Event
}

// Hub 以單一 goroutine 序列化連線註冊與事件路由，避免 map 競態。
type Hub struct {
	clients    map[*Client]bool
	register   chan *Client
	unregister chan *Client
	publishCh  chan publishReq
}

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		register:   make(chan *Client, 32),
		unregister: make(chan *Client, 32),
		publishCh:  make(chan publishReq, 256),
	}
}

// Run 事件迴圈，需在 goroutine 中啟動並常駐。
func (h *Hub) Run() {
	for {
		select {
		case c := <-h.register:
			h.clients[c] = true
		case c := <-h.unregister:
			if _, ok := h.clients[c]; ok {
				delete(h.clients, c)
				close(c.Send)
			}
		case req := <-h.publishCh:
			h.route(req)
		}
	}
}

// route 比對收件人送出事件；ID==0 為該角色廣播。
func (h *Hub) route(req publishReq) {
	raw, err := req.ev.JSON()
	if err != nil {
		log.Error().Err(err).Msg("事件序列化失敗")
		return
	}
	for c := range h.clients {
		if c.Rec.Role != req.rec.Role {
			continue
		}
		if req.rec.ID != 0 && c.Rec.ID != req.rec.ID {
			continue
		}
		select {
		case c.Send <- raw:
		default:
			// 慢客戶：佇列已滿則丟棄該則，避免拖垮 Hub
			log.Warn().Str("role", c.Rec.Role).Int64("id", c.Rec.ID).Msg("客戶端佇列已滿，丟棄事件")
		}
	}
}

func (h *Hub) Register(c *Client)   { h.register <- c }
func (h *Hub) Unregister(c *Client) { h.unregister <- c }

// Publish 實作 Publisher 介面。
func (h *Hub) Publish(rec Recipient, ev Event) {
	select {
	case h.publishCh <- publishReq{rec: rec, ev: ev}:
	default:
		log.Warn().Msg("Hub 發佈佇列已滿，丟棄事件")
	}
}
