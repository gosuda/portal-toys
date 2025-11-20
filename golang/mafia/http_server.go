package main

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

//go:embed static
var staticFS embed.FS

// HTTPServer wires HTTP routes to the room manager.
type HTTPServer struct {
	mgr      *RoomManager
	upgrader websocket.Upgrader
	authKey  string
}

// NewHTTPServer constructs an HTTPServer with sane defaults.
func NewHTTPServer(mgr *RoomManager, authKey string) *HTTPServer {
	return &HTTPServer{
		mgr: mgr,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		authKey: authKey,
	}
}

// Router exposes the HTTP mux used for both Portal relay and optional local serve.
func (s *HTTPServer) Router() http.Handler {
	mux := http.NewServeMux()
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		log.Fatal().Err(err).Msg("embed static")
	}
	mux.Handle("/", http.FileServer(http.FS(sub)))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/ws", s.handleWebSocket)
	return mux
}

func (s *HTTPServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	roomName := r.URL.Query().Get("room")
	user := r.URL.Query().Get("user")
	if roomName == "" || user == "" {
		http.Error(w, "missing room or user", http.StatusBadRequest)
		return
	}
	if s.authKey != "" {
		if r.Header.Get("X-Mafia-Key") != s.authKey {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error().Err(err).Msg("upgrade websocket")
		return
	}

	client := NewClient(user, conn, s.mgr)
	if err := s.mgr.Attach(roomName, client); err != nil {
		msg := fmt.Sprintf("join failed: %v", err)
		_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, msg), time.Now().Add(2*time.Second))
		_ = conn.Close()
		log.Warn().Err(err).Str("room", roomName).Str("user", user).Msg("attach failed")
		return
	}

	go client.writeLoop()
	client.readLoop()
}
