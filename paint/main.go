package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	sdk "github.com/gosuda/relaydns/sdk/go"
)

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
	flags.StringVar(&flagServerURL, "server-url", "http://relaydns.gosuda.org", "relayserver base URL to auto-fetch multiaddrs from /health")
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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1) start local paint HTTP backend
	httpLn, err := net.Listen("tcp", fmt.Sprintf(":%d", flagPort))
	if err != nil {
		return fmt.Errorf("listen paint: %w", err)
	}

	canvas := newCanvas()

	mux := http.NewServeMux()

	// Serve static files
	fs := http.FileServer(http.Dir("./paint/static"))
	mux.Handle("/", fs)
	mux.HandleFunc("/ws", canvas.handleWS)

	httpSrv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		if err := httpSrv.Serve(httpLn); err != nil && err != http.ErrServerClosed {
			log.Error().Err(err).Msg("paint http error")
		}
	}()

	log.Info().Msgf("[paint] local server running on :%d", flagPort)

	// 2) advertise over RelayDNS
	client, err := sdk.NewClient(ctx, sdk.ClientConfig{
		Name:      flagName,
		TargetTCP: fmt.Sprintf("127.0.0.1:%d", flagPort),
		ServerURL: flagServerURL,
	})
	if err != nil {
		return fmt.Errorf("new client: %w", err)
	}
	if err := client.Start(ctx); err != nil {
		return fmt.Errorf("start client: %w", err)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Info().Msg("[paint] shutting down...")

	// Shutdown sequence
	if err := client.Close(); err != nil {
		log.Warn().Err(err).Msg("[paint] client close error")
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("[paint] http server shutdown error")
	}

	canvas.closeAll()
	canvas.wait()

	log.Info().Msg("[paint] shutdown complete")
	return nil
}
