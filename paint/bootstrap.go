package main

import (
	"embed"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

//go:embed static
var staticFiles embed.FS

// DrawMessage represents a drawing action
type DrawMessage struct {
	Type   string  `json:"type"` // "draw", "shape", "text", or "clear"
	X      float64 `json:"x,omitempty"`
	Y      float64 `json:"y,omitempty"`
	PrevX  float64 `json:"prevX,omitempty"`
	PrevY  float64 `json:"prevY,omitempty"`
	StartX float64 `json:"startX,omitempty"`
	StartY float64 `json:"startY,omitempty"`
	EndX   float64 `json:"endX,omitempty"`
	EndY   float64 `json:"endY,omitempty"`
	Mode   string  `json:"mode,omitempty"` // "line", "circle", "rectangle"
	Text   string  `json:"text,omitempty"` // for text type
	Color  string  `json:"color,omitempty"`
	Width  int     `json:"width,omitempty"`
	Canvas string  `json:"canvas,omitempty"` // for initial state
	Image  string  `json:"image,omitempty"`  // data URL for images
	ID     string  `json:"id,omitempty"`     // server-side image id
	W      float64 `json:"w,omitempty"`      // image draw width
	H      float64 `json:"h,omitempty"`      // image draw height
}

// ImageStore holds uploaded images in memory
type ImageStore struct {
	mu    sync.RWMutex
	data  map[string][]byte
	ctype map[string]string
}

func newImageStore() *ImageStore {
	return &ImageStore{data: make(map[string][]byte), ctype: make(map[string]string)}
}

func (s *ImageStore) put(id string, b []byte, contentType string) {
	s.mu.Lock()
	s.data[id] = b
	s.ctype[id] = contentType
	s.mu.Unlock()
}

func (s *ImageStore) get(id string) ([]byte, string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.data[id]
	if !ok {
		return nil, "", false
	}
	return b, s.ctype[id], true
}

var images *ImageStore

// Canvas holds the current drawing state
type Canvas struct {
	mu      sync.RWMutex
	clients map[*websocket.Conn]bool
	wg      sync.WaitGroup
	history []DrawMessage
}

func newCanvas() *Canvas {
	return &Canvas{
		clients: make(map[*websocket.Conn]bool),
		history: make([]DrawMessage, 0),
	}
}

func (c *Canvas) register(conn *websocket.Conn) {
	// Only register client; do NOT push full history to avoid slow joins
	c.mu.Lock()
	c.clients[conn] = true
	c.mu.Unlock()
}

func (c *Canvas) unregister(conn *websocket.Conn) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.clients[conn]; ok {
		delete(c.clients, conn)
		conn.Close()
	}
}

func (c *Canvas) broadcast(msg DrawMessage) {
	// Update history and copy client list under lock
	c.mu.Lock()
	switch msg.Type {
	case "draw", "shape", "text", "image":
		c.history = append(c.history, msg)
	case "clear":
		c.history = make([]DrawMessage, 0)
	}
	clients := make([]*websocket.Conn, 0, len(c.clients))
	for cl := range c.clients {
		clients = append(clients, cl)
	}
	c.mu.Unlock()

	// Broadcast outside lock
	for _, client := range clients {
		if err := client.WriteJSON(msg); err != nil {
			log.Error().Err(err).Msg("write to client")
			client.Close()
			// remove bad client under lock
			c.mu.Lock()
			delete(c.clients, client)
			c.mu.Unlock()
		}
	}
}

func (c *Canvas) closeAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for client := range c.clients {
		client.Close()
	}
	c.clients = make(map[*websocket.Conn]bool)
}

func (c *Canvas) wait() {
	c.wg.Wait()
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func (c *Canvas) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error().Err(err).Msg("upgrade websocket")
		return
	}

	c.register(conn)
	c.wg.Add(1)

	defer func() {
		c.unregister(conn)
		c.wg.Done()
	}()

	for {
		var msg DrawMessage
		err := conn.ReadJSON(&msg)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Error().Err(err).Msg("read message")
			}
			break
		}
		// If client sent inline data URL image, convert to server-side ID to avoid huge frames
		if msg.Type == "image" && msg.ID == "" && msg.Image != "" {
			if mt, raw, derr := decodeDataURL(msg.Image); derr == nil {
				// derive extension from mimetype
				id := fmt.Sprintf("%d", time.Now().UnixNano())
				images.put(id, raw, mt)
				msg.ID = id
				msg.Image = ""
			} else {
				log.Warn().Err(derr).Msg("failed to decode data url image")
			}
		}
		c.broadcast(msg)
	}
}

func decodeDataURL(dataURL string) (mimeType string, raw []byte, err error) {
	if !strings.HasPrefix(dataURL, "data:") {
		return "", nil, fmt.Errorf("invalid data url")
	}
	parts := strings.SplitN(dataURL, ",", 2)
	if len(parts) != 2 {
		return "", nil, fmt.Errorf("invalid data url payload")
	}
	header := parts[0]
	payload := parts[1]
	// header like: data:<mime>;base64
	if !strings.HasSuffix(header, ";base64") {
		return "", nil, fmt.Errorf("unsupported data url encoding")
	}
	// extract mime
	header = strings.TrimPrefix(header, "data:")
	if i := strings.IndexByte(header, ';'); i != -1 {
		header = header[:i]
	}
	b, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return "", nil, fmt.Errorf("decode base64: %w", err)
	}
	return header, b, nil
}
