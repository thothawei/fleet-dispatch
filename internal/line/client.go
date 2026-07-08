package lineclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// Client LINE Messaging API 輕量封裝
type Client struct {
	accessToken string
	httpClient  *http.Client
}

func NewClient(accessToken string) *Client {
	return &Client{
		accessToken: accessToken,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

type replyRequest struct {
	ReplyToken string        `json:"replyToken"`
	Messages   []interface{} `json:"messages"`
}

type pushRequest struct {
	To       string        `json:"to"`
	Messages []interface{} `json:"messages"`
}

type textMessage struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type templateMessage struct {
	Type     string      `json:"type"`
	AltText  string      `json:"altText"`
	Template interface{} `json:"template"`
}

type buttonsTemplate struct {
	Type    string   `json:"type"`
	Text    string   `json:"text"`
	Actions []action `json:"actions"`
}

type action struct {
	Type        string `json:"type"`
	Label       string `json:"label"`
	Data        string `json:"data,omitempty"`
	URI         string `json:"uri,omitempty"`
	DisplayText string `json:"displayText,omitempty"`
}

// ReplyText 使用 reply token 回覆文字
func (c *Client) ReplyText(ctx context.Context, replyToken, text string) error {
	return c.reply(ctx, replyToken, []interface{}{
		textMessage{Type: "text", Text: text},
	})
}

// PushText 主動推播文字
func (c *Client) PushText(ctx context.Context, to, text string) error {
	if c == nil || c.accessToken == "" || to == "" {
		return nil
	}
	return c.push(ctx, to, []interface{}{
		textMessage{Type: "text", Text: text},
	})
}

// PushRideOffer 推播接單邀請（含接受按鈕）
func (c *Client) PushRideOffer(ctx context.Context, to string, rideID int64, body string) error {
	if c == nil || c.accessToken == "" || to == "" {
		return nil
	}
	msg := templateMessage{
		Type:    "template",
		AltText: fmt.Sprintf("新派單 #%d", rideID),
		Template: buttonsTemplate{
			Type: "buttons",
			Text: body,
			Actions: []action{
				{
					Type:        "postback",
					Label:       "接受派單",
					Data:        fmt.Sprintf("action=accept&ride_id=%d", rideID),
					DisplayText: "接受派單",
				},
				{
					Type:        "postback",
					Label:       "拒絕",
					Data:        fmt.Sprintf("action=decline&ride_id=%d", rideID),
					DisplayText: "拒絕派單",
				},
			},
		},
	}
	return c.push(ctx, to, []interface{}{msg})
}

func (c *Client) reply(ctx context.Context, replyToken string, messages []interface{}) error {
	if c.accessToken == "" {
		return nil
	}
	body, err := json.Marshal(replyRequest{ReplyToken: replyToken, Messages: messages})
	if err != nil {
		return err
	}
	return c.doPOST(ctx, "https://api.line.me/v2/bot/message/reply", body)
}

func (c *Client) push(ctx context.Context, to string, messages []interface{}) error {
	body, err := json.Marshal(pushRequest{To: to, Messages: messages})
	if err != nil {
		return err
	}
	return c.doPOST(ctx, "https://api.line.me/v2/bot/message/push", body)
}

func (c *Client) doPOST(ctx context.Context, url string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.accessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("LINE API 失敗: HTTP %d", resp.StatusCode)
	}
	return nil
}

// ParsePostback 解析 postback data（格式：action=accept&ride_id=123 或 action=decline&ride_id=123）
func ParsePostback(data string) (action string, rideID int64, ok bool) {
	for _, part := range splitAmp(data) {
		if len(part) > 7 && part[:7] == "action=" {
			action = part[7:]
		} else if len(part) > 8 && part[:8] == "ride_id=" {
			if id, err := strconv.ParseInt(part[8:], 10, 64); err == nil {
				rideID = id
			}
		}
	}
	return action, rideID, action != "" && rideID > 0
}

func splitAmp(s string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '&' {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}
