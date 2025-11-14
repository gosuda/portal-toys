package main

import (
	"encoding/json"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingInterval   = 30 * time.Second
	sendBufferSize = 64
)

// Client represents a single websocket participant.
type Client struct {
	name   string
	room   *Room
	conn   *websocket.Conn
	send   chan ServerEvent
	mgr    *RoomManager
	closed atomic.Bool
}

func NewClient(name string, conn *websocket.Conn, mgr *RoomManager) *Client {
	return &Client{
		name: name,
		conn: conn,
		mgr:  mgr,
		send: make(chan ServerEvent, sendBufferSize),
	}
}

func (c *Client) readLoop() {
	defer func() {
		c.close()
	}()
	c.conn.SetReadLimit(1 << 20)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	for {
		_, payload, err := c.conn.ReadMessage()
		if err != nil {
			log.Debug().Err(err).Str("user", c.name).Msg("read message")
			return
		}
		var msg ClientMessage
		if err := json.Unmarshal(payload, &msg); err != nil {
			c.pushSystem("잘못된 메시지 형식입니다.")
			continue
		}
		c.mgr.RouteMessage(c, msg)
	}
}

func (c *Client) writeLoop() {
	ticker := time.NewTicker(pingInterval)
	defer func() {
		ticker.Stop()
		c.close()
	}()
	for {
		select {
		case ev, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteJSON(ev); err != nil {
				log.Debug().Err(err).Str("user", c.name).Msg("write json")
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *Client) push(ev ServerEvent) {
	select {
	case c.send <- ev:
	default:
		// drop oldest to avoid blocking
		select {
		case <-c.send:
		default:
		}
		c.send <- ev
	}
}

func (c *Client) pushSystem(body string) {
	c.push(ServerEvent{Type: "log", Body: body, Room: c.roomName()})
}

func (c *Client) roomName() string {
	if c.room == nil {
		return ""
	}
	return c.room.name
}

func (c *Client) close() {
	if c.closed.Swap(true) {
		return
	}
	if c.room != nil {
		c.mgr.Detach(c)
	}
	close(c.send)
	_ = c.conn.Close()
}
