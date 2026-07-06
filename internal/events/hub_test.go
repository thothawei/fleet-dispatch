package events

import (
	"testing"
	"time"
)

func waitMsg(t *testing.T, c *Client) []byte {
	t.Helper()
	select {
	case m := <-c.Send:
		return m
	case <-time.After(time.Second):
		t.Fatal("等待事件逾時")
		return nil
	}
}

func TestHub_定向送給指定收件人(t *testing.T) {
	h := NewHub()
	go h.Run()

	cust := &Client{Rec: Recipient{Role: RoleCustomer, ID: 7}, Send: make(chan []byte, 4)}
	other := &Client{Rec: Recipient{Role: RoleCustomer, ID: 99}, Send: make(chan []byte, 4)}
	h.Register(cust)
	h.Register(other)
	// 等 Run 消化 register
	time.Sleep(20 * time.Millisecond)

	h.Publish(Recipient{Role: RoleCustomer, ID: 7}, Event{Type: TypeRideAccepted, RideID: 1})

	msg := waitMsg(t, cust)
	if len(msg) == 0 {
		t.Fatal("指定收件人未收到事件")
	}
	select {
	case <-other.Send:
		t.Fatal("非收件人不應收到事件")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestHub_後台廣播送給所有Admin(t *testing.T) {
	h := NewHub()
	go h.Run()
	a1 := &Client{Rec: Recipient{Role: RoleAdmin, ID: 1}, Send: make(chan []byte, 4)}
	a2 := &Client{Rec: Recipient{Role: RoleAdmin, ID: 2}, Send: make(chan []byte, 4)}
	h.Register(a1)
	h.Register(a2)
	time.Sleep(20 * time.Millisecond)

	h.Publish(Recipient{Role: RoleAdmin, ID: 0}, Event{Type: TypeDriverLocation})

	waitMsg(t, a1)
	waitMsg(t, a2)
}

func TestHub_註銷後不再收到(t *testing.T) {
	h := NewHub()
	go h.Run()
	c := &Client{Rec: Recipient{Role: RoleDriver, ID: 5}, Send: make(chan []byte, 4)}
	h.Register(c)
	time.Sleep(20 * time.Millisecond)
	h.Unregister(c)
	time.Sleep(20 * time.Millisecond)

	h.Publish(Recipient{Role: RoleDriver, ID: 5}, Event{Type: TypeRideAssigned})
	select {
	case m, ok := <-c.Send:
		// 註銷時 Hub 會 close(Send)，此處 ok==false 屬正常關閉；
		// 只有收到「真實事件」（ok==true）才算註銷失效。
		if ok {
			t.Fatalf("註銷後不應再收到事件: %s", m)
		}
	case <-time.After(100 * time.Millisecond):
	}
}
