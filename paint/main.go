package main

import (
	"context"
	"embed"
	"encoding/base64"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"gosuda.org/portal/sdk"
)

//go:embed static
var staticFiles embed.FS

var rootCmd = &cobra.Command{
	Use:   "paint",
	Short: "collaborative paint",
	RunE:  runPaint,
}

var (
	flagServerURL string
	flagPort      int
	flagName      string
)

func init() {
	flags := rootCmd.PersistentFlags()
	flags.StringVar(&flagServerURL, "server-url", "wss://portal.gosuda.org/relay", "relay websocket URL")
	flags.IntVar(&flagPort, "port", -1, "optional local HTTP port (negative to disable)")
	flags.StringVar(&flagName, "name", "paint", "backend display name")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("execute paint command")
	}
}

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

func runPaint(cmd *cobra.Command, args []string) error {
	// Cancellation context for graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Create SDK client and connect to relay(s)
	client, err := sdk.NewClient(func(c *sdk.RDClientConfig) {
		c.BootstrapServers = []string{flagServerURL}
	})
	if err != nil {
		return fmt.Errorf("new client: %w", err)
	}
	defer client.Close()

	// Register lease and obtain a net.Listener that accepts relayed connections
	cred := sdk.NewCredential()
	listener, err := client.Listen(cred, flagName, []string{"http/1.1"})
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	// Setup HTTP handler
	canvas := newCanvas()
	images = newImageStore()
	mux := http.NewServeMux()

	// Serve static files from embedded filesystem
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return fmt.Errorf("create static fs: %w", err)
	}
	// Image upload endpoint (multipart/form-data; field name 'file')
	mux.HandleFunc("/upload-image", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		// limit to 10MB
		r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
		if err := r.ParseMultipartForm(12 << 20); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		f, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "file not found", http.StatusBadRequest)
			return
		}
		defer f.Close()
		buf, err := io.ReadAll(f)
		if err != nil {
			http.Error(w, "read file", http.StatusInternalServerError)
			return
		}
		// determine content-type
		ct := header.Header.Get("Content-Type")
		if ct == "" {
			ct = http.DetectContentType(buf)
		}
		// generate id
		id := fmt.Sprintf("%d", time.Now().UnixNano())
		images.put(id, buf, ct)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fmt.Sprintf(`{"id":"%s"}`, id)))
	})

	// Serve stored images
	mux.HandleFunc("/images/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/images/")
		if id == "" {
			http.NotFound(w, r)
			return
		}
		if b, ct, ok := images.get(id); ok {
			w.Header().Set("Content-Type", ct)
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(b)
			return
		}
		http.NotFound(w, r)
	})

	mux.Handle("/", http.FileServer(http.FS(staticFS)))
	mux.HandleFunc("/ws", canvas.handleWS)

	// 5) Serve HTTP over relay listener
	log.Info().Msgf("[paint] serving HTTP over relay; lease=%s id=%s", flagName, cred.ID())
	go func() {
		if err := http.Serve(listener, mux); err != nil && err != http.ErrServerClosed && ctx.Err() == nil {
			log.Error().Err(err).Msg("[paint] http serve error")
		}
	}()

	// Single watcher for shutdown
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	// Optional: also serve locally on --port like http-backend
	var httpSrv *http.Server
	if flagPort >= 0 {
		httpSrv = &http.Server{Addr: fmt.Sprintf(":%d", flagPort), Handler: mux}
		log.Info().Msgf("[paint] serving locally at http://127.0.0.1:%d", flagPort)
		go func() {
			if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Warn().Err(err).Msg("[paint] local http stopped")
			}
		}()
	}

	// One watcher that shuts down local server if started
	go func() {
		<-ctx.Done()
		if httpSrv != nil {
			sctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := httpSrv.Shutdown(sctx); err != nil && err != context.Canceled {
				log.Warn().Err(err).Msg("[paint] local http shutdown error")
			}
		}
	}()

	// Block until canceled, then cleanup canvas
	<-ctx.Done()
	canvas.closeAll()
	canvas.wait()

	log.Info().Msg("[paint] shutdown complete")
	return nil
}
