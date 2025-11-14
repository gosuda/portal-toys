package main

import (
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

// HTTPServer wires HTTP routes to the room manager.
type HTTPServer struct {
	mgr      *RoomManager
	upgrader websocket.Upgrader
}

// NewHTTPServer constructs an HTTPServer with sane defaults.
func NewHTTPServer(mgr *RoomManager) *HTTPServer {
	return &HTTPServer{
		mgr: mgr,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

// Router exposes the HTTP mux used for both Portal relay and optional local serve.
func (s *HTTPServer) Router() http.Handler {
	mux := http.NewServeMux()
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

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error().Err(err).Msg("upgrade websocket")
		return
	}

	client := NewClient(user, conn, s.mgr)
	if err := s.mgr.Attach(roomName, client); err != nil {
		_ = conn.Close()
		log.Warn().Err(err).Str("room", roomName).Str("user", user).Msg("attach failed")
		return
	}

	go client.writeLoop()
	client.readLoop()
}
