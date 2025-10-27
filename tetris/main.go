package main

import (
	"context"
	"encoding/json"
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
	Use:   "relaydns-tetris",
	Short: "RelayDNS multiplayer tetris (local HTTP backend + libp2p advertiser)",
	RunE:  runTetris,
}

var (
	flagServerURL string
	flagPort      int
	flagName      string
)

func init() {
	flags := rootCmd.PersistentFlags()
	flags.StringVar(&flagServerURL, "server-url", "http://relaydns.gosuda.org", "relayserver base URL to auto-fetch multiaddrs from /health")
	flags.IntVar(&flagPort, "port", 8093, "local tetris HTTP port")
	flags.StringVar(&flagName, "name", "example-tetris", "backend display name")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("execute tetris command")
	}
}

// Message types
type Message struct {
	Type       string        `json:"type"`
	RoomID     string        `json:"roomId,omitempty"`
	RoomName   string        `json:"roomName,omitempty"`
	PlayerID   string        `json:"playerId,omitempty"`
	Nickname   string        `json:"nickname,omitempty"`
	Ready      bool          `json:"ready,omitempty"`
	MaxPlayers int           `json:"maxPlayers,omitempty"`
	Rooms      []RoomInfo    `json:"rooms,omitempty"`
	Room       *RoomInfo     `json:"room,omitempty"`
	Error      string        `json:"error,omitempty"`
	Players    []PlayerState `json:"players,omitempty"`
	Match      *MatchInfo    `json:"match,omitempty"`
	Text       string        `json:"text,omitempty"`
	Timestamp  int64         `json:"timestamp,omitempty"`
	Board      [][]int       `json:"board,omitempty"`
}

type RoomInfo struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	HostID      string        `json:"hostId"`
	MaxPlayers  int           `json:"maxPlayers"`
	PlayerCount int           `json:"playerCount"`
	InGame      bool          `json:"inGame"`
	Players     []PlayerState `json:"players"`
}

type PlayerState struct {
	ID        string  `json:"id"`
	Nickname  string  `json:"nickname"`
	Ready     bool    `json:"ready"`
	Score     int     `json:"score"`
	Level     int     `json:"level"`
	GameOver  bool    `json:"gameOver"`
	IsPlaying bool    `json:"isPlaying"`
	IsWinner  bool    `json:"isWinner"`
	Board     [][]int `json:"board,omitempty"`
}

type MatchInfo struct {
	Player1 *PlayerState `json:"player1"`
	Player2 *PlayerState `json:"player2"`
}

// Room represents a game room
type Room struct {
	mu           sync.RWMutex
	id           string
	name         string
	hostID       string // Room host (first player)
	maxPlayers   int
	inGame       bool
	players      map[string]*Player
	playerQueue  []string  // Order players joined
	currentMatch [2]string // Player IDs of current match
}

// Player represents a player in a room
type Player struct {
	id       string
	nickname string
	conn     *websocket.Conn
	ready    bool
	score    int
	level    int
	gameOver bool
	board    [][]int
}

// Server manages all rooms
type Server struct {
	mu    sync.RWMutex
	rooms map[string]*Room
	wg    sync.WaitGroup
}

func newServer() *Server {
	return &Server{
		rooms: make(map[string]*Room),
	}
}

func (s *Server) createRoom(roomName string, maxPlayers int) *Room {
	s.mu.Lock()
	defer s.mu.Unlock()

	roomID := fmt.Sprintf("room_%d", time.Now().UnixNano())
	room := &Room{
		id:          roomID,
		name:        roomName,
		maxPlayers:  maxPlayers,
		inGame:      false,
		players:     make(map[string]*Player),
		playerQueue: make([]string, 0),
	}

	s.rooms[roomID] = room
	return room
}

func (s *Server) getRoom(roomID string) *Room {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.rooms[roomID]
}

func (s *Server) deleteRoom(roomID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.rooms, roomID)
}

func (s *Server) getRoomList() []RoomInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rooms := make([]RoomInfo, 0, len(s.rooms))
	for _, room := range s.rooms {
		room.mu.RLock()
		info := RoomInfo{
			ID:          room.id,
			Name:        room.name,
			MaxPlayers:  room.maxPlayers,
			PlayerCount: len(room.players),
			InGame:      room.inGame,
			Players:     room.getPlayerStates(),
		}
		room.mu.RUnlock()
		rooms = append(rooms, info)
	}

	return rooms
}

func (r *Room) addPlayer(player *Player) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.players) >= r.maxPlayers {
		return false
	}

	// Set first player as host
	if len(r.players) == 0 {
		r.hostID = player.id
		log.Info().Msgf("[room %s] Player %s is now the host", r.id, player.id)
	}

	r.players[player.id] = player
	r.playerQueue = append(r.playerQueue, player.id)
	return true
}

func (r *Room) removePlayer(playerID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if player, ok := r.players[playerID]; ok {
		player.conn.Close()
		delete(r.players, playerID)

		// Remove from queue
		for i, id := range r.playerQueue {
			if id == playerID {
				r.playerQueue = append(r.playerQueue[:i], r.playerQueue[i+1:]...)
				break
			}
		}

		// If host left, assign new host
		if r.hostID == playerID && len(r.playerQueue) > 0 {
			r.hostID = r.playerQueue[0]
			log.Info().Msgf("[room %s] Player %s is now the host", r.id, r.hostID)
		}
	}
}

func (r *Room) getPlayerStates() []PlayerState {
	// Assume lock is held by caller
	states := make([]PlayerState, 0, len(r.players))

	for _, id := range r.playerQueue {
		if p, ok := r.players[id]; ok {
			// Check if player is in current match
			isPlaying := false
			if r.inGame && r.currentMatch[0] != "" {
				// Player is playing if they're player 1 or player 2 (player 2 can be empty for solo mode)
				isPlaying = (id == r.currentMatch[0] || (r.currentMatch[1] != "" && id == r.currentMatch[1]))
				log.Debug().Msgf("[room] Player %s (nick: %s) isPlaying=%v (match: %s vs %s)", id, p.nickname, isPlaying, r.currentMatch[0], r.currentMatch[1])
			} else if r.inGame {
				log.Warn().Msgf("[room] Game is in progress but match not set properly: [%s] vs [%s]", r.currentMatch[0], r.currentMatch[1])
			}

			states = append(states, PlayerState{
				ID:        p.id,
				Nickname:  p.nickname,
				Ready:     p.ready,
				Score:     p.score,
				Level:     p.level,
				GameOver:  p.gameOver,
				IsPlaying: isPlaying,
				IsWinner:  false,
				Board:     p.board,
			})
		}
	}
	return states
}

func (r *Room) broadcast(msg Message) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	data, _ := json.Marshal(msg)
	for _, player := range r.players {
		player.conn.WriteMessage(websocket.TextMessage, data)
	}
}

func (r *Room) broadcastRoomState() {
	r.mu.RLock()
	players := r.getPlayerStates()
	inGame := r.inGame
	hostID := r.hostID
	r.mu.RUnlock()

	msg := Message{
		Type:    "roomState",
		Players: players,
		Room: &RoomInfo{
			ID:          r.id,
			Name:        r.name,
			HostID:      hostID,
			MaxPlayers:  r.maxPlayers,
			PlayerCount: len(players),
			InGame:      inGame,
			Players:     players,
		},
	}

	r.broadcast(msg)
}

func (r *Room) checkAllReady() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Need at least 2 players to start
	if len(r.players) < 2 {
		log.Debug().Msgf("[room %s] Not enough players to start: %d", r.id, len(r.players))
		return false
	}

	// All players must be ready
	for _, p := range r.players {
		if !p.ready {
			log.Debug().Msgf("[room %s] Player %s not ready", r.id, p.id)
			return false
		}
	}

	log.Info().Msgf("[room %s] All %d players are ready!", r.id, len(r.players))
	return true
}

func (r *Room) startGame() {
	r.mu.Lock()
	r.inGame = true

	// Set players for current match
	if len(r.playerQueue) >= 2 {
		r.currentMatch[0] = r.playerQueue[0]
		r.currentMatch[1] = r.playerQueue[1]
		log.Info().Msgf("[room %s] Starting match: %s vs %s", r.id, r.currentMatch[0], r.currentMatch[1])
	} else if len(r.playerQueue) == 1 {
		// Solo practice mode
		r.currentMatch[0] = r.playerQueue[0]
		r.currentMatch[1] = ""
		log.Info().Msgf("[room %s] Starting solo practice: %s", r.id, r.currentMatch[0])
	} else {
		log.Warn().Msgf("[room %s] Not enough players: %d", r.id, len(r.playerQueue))
	}
	r.mu.Unlock()

	r.broadcast(Message{Type: "gameStart"})
	r.broadcastRoomState()
}

func (r *Room) sendChat(playerID, nickname, text string) {
	msg := Message{
		Type:      "chat",
		PlayerID:  playerID,
		Nickname:  nickname,
		Text:      text,
		Timestamp: time.Now().Unix(),
	}
	r.broadcast(msg)
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func (s *Server) handleWS(w http.ResponseWriter, req *http.Request) {
	conn, err := upgrader.Upgrade(w, req, nil)
	if err != nil {
		log.Error().Err(err).Msg("upgrade websocket")
		return
	}

	s.wg.Add(1)
	defer s.wg.Done()

	var currentRoom *Room
	var playerID string

	defer func() {
		if currentRoom != nil && playerID != "" {
			// Check if the leaving player was in a match
			currentRoom.mu.Lock()
			wasPlaying := currentRoom.inGame && (currentRoom.currentMatch[0] == playerID || currentRoom.currentMatch[1] == playerID)
			if wasPlaying {
				// End the game and reset
				log.Info().Msgf("[room %s] Player %s left during game, ending match", currentRoom.id, playerID)
				currentRoom.inGame = false
				currentRoom.currentMatch[0] = ""
				currentRoom.currentMatch[1] = ""

				// Reset all players ready status
				for _, p := range currentRoom.players {
					p.ready = false
					p.score = 0
					p.level = 1
					p.gameOver = false
					p.board = nil
				}
			}
			currentRoom.mu.Unlock()

			currentRoom.removePlayer(playerID)

			// If game was in progress, notify everyone to return to room
			if wasPlaying {
				currentRoom.broadcast(Message{Type: "gameEnded", Error: "A player left the game"})
			}

			currentRoom.broadcastRoomState()

			// Delete room if empty
			currentRoom.mu.RLock()
			isEmpty := len(currentRoom.players) == 0
			currentRoom.mu.RUnlock()

			if isEmpty {
				s.deleteRoom(currentRoom.id)
			}

			// Broadcast updated room list
			s.broadcastRoomList()
		}
	}()

	for {
		var msg Message
		err := conn.ReadJSON(&msg)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Error().Err(err).Msg("read message")
			}
			break
		}

		switch msg.Type {
		case "getRooms":
			rooms := s.getRoomList()
			conn.WriteJSON(Message{
				Type:  "roomList",
				Rooms: rooms,
			})

		case "createRoom":
			room := s.createRoom(msg.RoomName, msg.MaxPlayers)
			player := &Player{
				id:       msg.PlayerID,
				nickname: msg.Nickname,
				conn:     conn,
				ready:    false,
			}

			room.addPlayer(player)
			currentRoom = room
			playerID = msg.PlayerID

			conn.WriteJSON(Message{
				Type:   "roomJoined",
				RoomID: room.id,
			})

			room.broadcastRoomState()
			s.broadcastRoomList()

		case "joinRoom":
			room := s.getRoom(msg.RoomID)
			if room == nil {
				conn.WriteJSON(Message{
					Type:  "error",
					Error: "Room not found",
				})
				continue
			}

			player := &Player{
				id:       msg.PlayerID,
				nickname: msg.Nickname,
				conn:     conn,
				ready:    false,
			}

			if !room.addPlayer(player) {
				conn.WriteJSON(Message{
					Type:  "error",
					Error: "Room is full",
				})
				continue
			}

			currentRoom = room
			playerID = msg.PlayerID

			conn.WriteJSON(Message{
				Type:   "roomJoined",
				RoomID: room.id,
			})

			room.broadcastRoomState()
			s.broadcastRoomList()

		case "setReady":
			if currentRoom == nil {
				continue
			}

			currentRoom.mu.Lock()
			if player, ok := currentRoom.players[msg.PlayerID]; ok {
				player.ready = msg.Ready
			}
			currentRoom.mu.Unlock()

			currentRoom.broadcastRoomState()

		case "startGame":
			if currentRoom == nil {
				continue
			}

			// Only host can start the game
			currentRoom.mu.RLock()
			isHost := currentRoom.hostID == msg.PlayerID
			playerCount := len(currentRoom.players)
			currentRoom.mu.RUnlock()

			if !isHost {
				conn.WriteJSON(Message{Type: "error", Error: "Only the host can start the game"})
				continue
			}

			// Host can start with 1+ players, others need 2+ and all ready
			if isHost && playerCount >= 1 {
				currentRoom.startGame()
			} else if !isHost && playerCount >= 2 && currentRoom.checkAllReady() {
				currentRoom.startGame()
			} else {
				conn.WriteJSON(Message{Type: "error", Error: "Not enough players or not all ready"})
			}

		case "gameState":
			if currentRoom == nil {
				continue
			}

			currentRoom.mu.Lock()
			if player, ok := currentRoom.players[msg.PlayerID]; ok {
				if len(msg.Players) > 0 {
					ps := msg.Players[0]
					player.score = ps.Score
					player.level = ps.Level
					player.gameOver = ps.GameOver
				}
				// Update board if provided
				if msg.Board != nil {
					player.board = msg.Board
				}
			}
			currentRoom.mu.Unlock()

			currentRoom.broadcastRoomState()

		case "chat":
			if currentRoom != nil {
				currentRoom.mu.RLock()
				player, ok := currentRoom.players[msg.PlayerID]
				currentRoom.mu.RUnlock()

				if ok {
					currentRoom.sendChat(player.id, player.nickname, msg.Text)
				}
			}

		case "leaveRoom":
			if currentRoom != nil {
				currentRoom.removePlayer(msg.PlayerID)
				currentRoom.broadcastRoomState()

				// Delete room if empty
				currentRoom.mu.RLock()
				isEmpty := len(currentRoom.players) == 0
				currentRoom.mu.RUnlock()

				if isEmpty {
					s.deleteRoom(currentRoom.id)
				}

				currentRoom = nil
				playerID = ""

				s.broadcastRoomList()
			}
		}
	}
}

func (s *Server) broadcastRoomList() {
	// This would need all connections, but we only have room-specific ones
	// For simplicity, clients will poll for room list
}

func (s *Server) closeAll() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, room := range s.rooms {
		room.mu.Lock()
		for _, player := range room.players {
			player.conn.Close()
		}
		room.mu.Unlock()
	}

	s.rooms = make(map[string]*Room)
}

func (s *Server) wait() {
	s.wg.Wait()
}

func runTetris(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	httpLn, err := net.Listen("tcp", fmt.Sprintf(":%d", flagPort))
	if err != nil {
		return fmt.Errorf("listen tetris: %w", err)
	}

	server := newServer()

	mux := http.NewServeMux()
	fs := http.FileServer(http.Dir("./static"))
	mux.Handle("/", fs)
	mux.HandleFunc("/ws", server.handleWS)

	httpSrv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		if err := httpSrv.Serve(httpLn); err != nil && err != http.ErrServerClosed {
			log.Error().Err(err).Msg("tetris http error")
		}
	}()

	log.Info().Msgf("[tetris] local server running on :%d", flagPort)

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
	log.Info().Msg("[tetris] shutting down...")

	if err := client.Close(); err != nil {
		log.Warn().Err(err).Msg("[tetris] client close error")
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("[tetris] http server shutdown error")
	}

	server.closeAll()
	server.wait()

	log.Info().Msg("[tetris] shutdown complete")
	return nil
}
