package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"github.com/gosuda/relaydns/sdk"
)

//go:embed static
var staticFiles embed.FS

var rootCmd = &cobra.Command{
	Use:   "relaydns-paint",
	Short: "RelayDNS collaborative paint (local HTTP backend + libp2p advertiser)",
	RunE:  runPaint,
}

var (
	flagServerURL string
	flagPort      int
	flagName      string
)

func init() {
	flags := rootCmd.PersistentFlags()
	flags.StringVar(&flagServerURL, "server-url", "wss://relaydns.gosuda.org/relay", "relay websocket URL")
	flags.IntVar(&flagPort, "port", 8092, "local paint HTTP port")
	flags.StringVar(&flagName, "name", "example-paint", "backend display name")
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
}

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
	c.mu.Lock()
	defer c.mu.Unlock()
	c.clients[conn] = true

	// Send history to new client
	for _, msg := range c.history {
		conn.WriteJSON(msg)
	}
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
	c.mu.Lock()
	defer c.mu.Unlock()

	// Store in history
	switch msg.Type {
	case "draw", "shape", "text":
		c.history = append(c.history, msg)
	case "clear":
		c.history = make([]DrawMessage, 0)
	}

	// Broadcast to all clients
	for client := range c.clients {
		err := client.WriteJSON(msg)
		if err != nil {
			log.Error().Err(err).Msg("write to client")
			client.Close()
			delete(c.clients, client)
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
		c.broadcast(msg)
	}
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
	mux := http.NewServeMux()

	// Serve static files from embedded filesystem
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return fmt.Errorf("create static fs: %w", err)
	}
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
	if flagPort > 0 {
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
