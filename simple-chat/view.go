package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"html/template"
	"io/fs"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

// sanitizeString removes control characters and limits string length to prevent issues.
// It preserves valid Unicode including emojis, CJK characters, and printable symbols.
func sanitizeString(s string, maxLen int) string {
	if s == "" {
		return ""
	}

	// Remove control characters but keep tabs, newlines, and valid Unicode
	var builder strings.Builder
	builder.Grow(len(s))

	for _, r := range s {
		// Skip control characters except tab and newline
		if unicode.IsControl(r) && r != '\t' && r != '\n' {
			continue
		}
		// Skip invalid Unicode replacement character if it appears
		if r == unicode.ReplacementChar {
			continue
		}
		builder.WriteRune(r)
	}

	result := builder.String()

	// Trim to max length (count runes, not bytes)
	if len([]rune(result)) > maxLen {
		runes := []rune(result)
		result = string(runes[:maxLen])
	}

	return strings.TrimSpace(result)
}

// writeJSON writes a JSON-encoded message to the websocket connection.
// Unlike gorilla's default WriteJSON, this disables HTML escaping to preserve
// characters like <, >, &, etc. in their original form.
func writeJSON(conn *websocket.Conn, v interface{}) error {
	w, err := conn.NextWriter(websocket.TextMessage)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false) // Don't escape <, >, &, etc.
	if err := enc.Encode(v); err != nil {
		return err
	}
	return w.Close()
}

// simple in-memory chat hub
type hub struct {
	mu         sync.RWMutex
	messages   []message
	maxBacklog int // maximum messages to keep in memory (0 = unlimited)
	conns      map[*websocket.Conn]struct{}
	connUID    map[*websocket.Conn]string
	userConns  map[string]map[*websocket.Conn]struct{}
	userName   map[string]string
	wg         sync.WaitGroup
	store      *messageStore
	connMu     map[*websocket.Conn]*sync.Mutex // per-connection write locks
	blacklist  map[string]bool                 // blocked UIDs
	admins     map[string]bool                 // admin UIDs
	joinOrder  []string                        // UIDs in join order (most recent last)
	uidTokens  map[string]string               // UID -> session token mapping
}

type message struct {
	TS       time.Time  `json:"ts"`
	User     string     `json:"user"`
	Text     string     `json:"text"`
	Event    string     `json:"event,omitempty"` // "joined" | "left" | "roster"
	UID      string     `json:"uid,omitempty"`
	Token    string     `json:"token,omitempty"`    // session token for auth
	Users    []string   `json:"users,omitempty"`    // deprecated: use UserList instead
	UserList []userInfo `json:"userList,omitempty"` // roster with UID info
	IsAdmin  bool       `json:"isAdmin,omitempty"`  // true if recipient is admin (for roster)
}

type userInfo struct {
	Name string `json:"name"`
	UID  string `json:"uid"`
}

func newHub() *hub {
	h := &hub{
		conns:      map[*websocket.Conn]struct{}{},
		connUID:    map[*websocket.Conn]string{},
		userConns:  map[string]map[*websocket.Conn]struct{}{},
		userName:   map[string]string{},
		messages:   make([]message, 0, 64),
		maxBacklog: 100, // keep last 100 messages in memory
		connMu:     map[*websocket.Conn]*sync.Mutex{},
		blacklist:  map[string]bool{},
		admins:     map[string]bool{},
		joinOrder:  make([]string, 0),
		uidTokens:  map[string]string{},
	}
	return h
}

// generateToken creates a random session token
func generateToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}

// addAdmin adds a UID to the admin list
func (h *hub) addAdmin(uid string) {
	h.mu.Lock()
	h.admins[uid] = true
	h.mu.Unlock()
	log.Info().Str("uid", uid).Msg("[chat] admin added")
}

// isAdmin checks if a UID is an admin
func (h *hub) isAdmin(uid string) bool {
	h.mu.RLock()
	isAdmin := h.admins[uid]
	h.mu.RUnlock()
	log.Info().Str("uid", uid).Msg("[chat] admin ")
	return isAdmin
}

// blockUser adds a UID to the blacklist and disconnects all their connections
func (h *hub) blockUser(uid string) {
	h.mu.Lock()
	h.blacklist[uid] = true
	// Get all connections for this UID
	conns := make([]*websocket.Conn, 0)
	if userConns, ok := h.userConns[uid]; ok {
		for conn := range userConns {
			conns = append(conns, conn)
		}
	}
	h.mu.Unlock()

	// Send close message to all connections for this user
	// The goroutines will handle the actual close when they detect the close frame
	for _, conn := range conns {
		h.mu.RLock()
		mu := h.connMu[conn]
		h.mu.RUnlock()
		if mu != nil {
			mu.Lock()
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "blocked"))
			mu.Unlock()
			// Don't call conn.Close() here - let the read goroutine detect the close frame and clean up
		}
	}
	log.Info().Str("uid", uid).Msg("[chat] user blocked")
}

// unblockUser removes a UID from the blacklist
func (h *hub) unblockUser(uid string) {
	h.mu.Lock()
	delete(h.blacklist, uid)
	h.mu.Unlock()
	log.Info().Str("uid", uid).Msg("[chat] user unblocked")
}

// isBlocked checks if a UID is in the blacklist
func (h *hub) isBlocked(uid string) bool {
	h.mu.RLock()
	blocked := h.blacklist[uid]
	h.mu.RUnlock()
	return blocked
}

// kickUser disconnects a user without blocking them (temporary kick)
func (h *hub) kickUser(uid string) {
	h.mu.RLock()
	// Get all connections for this UID
	conns := make([]*websocket.Conn, 0)
	if userConns, ok := h.userConns[uid]; ok {
		for conn := range userConns {
			conns = append(conns, conn)
		}
	}
	h.mu.RUnlock()

	// Send close message to all connections for this user
	for _, conn := range conns {
		h.mu.RLock()
		mu := h.connMu[conn]
		h.mu.RUnlock()
		if mu != nil {
			mu.Lock()
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "kicked by admin"))
			mu.Unlock()
		}
	}
	log.Info().Str("uid", uid).Msg("[chat] user kicked")
}

// getLastJoinedUID returns the UID of the most recently joined user (excluding admins)
func (h *hub) getLastJoinedUID() string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	// Iterate from end (most recent) to beginning
	for i := len(h.joinOrder) - 1; i >= 0; i-- {
		uid := h.joinOrder[i]
		// Skip if admin
		if h.admins[uid] {
			continue
		}
		// Check if user is still connected
		if conns, ok := h.userConns[uid]; ok && len(conns) > 0 {
			return uid
		}
	}
	return ""
}

func (h *hub) broadcast(m message) {
	h.mu.Lock()
	// Do not persist/retain roster messages in backlog; they are ephemeral UI state
	if m.Event != "roster" {
		h.messages = append(h.messages, m)
		// Trim old messages if we exceed maxBacklog
		if h.maxBacklog > 0 && len(h.messages) > h.maxBacklog {
			// Keep only the most recent maxBacklog messages
			copy(h.messages, h.messages[len(h.messages)-h.maxBacklog:])
			h.messages = h.messages[:h.maxBacklog]
		}
	}
	conns := make([]*websocket.Conn, 0, len(h.conns))
	for c := range h.conns {
		conns = append(conns, c)
	}
	h.mu.Unlock()
	if h.store != nil && m.Event != "roster" {
		if err := h.store.Append(m); err != nil {
			log.Debug().Err(err).Msg("persist message")
		}
	}
	for _, c := range conns {
		h.mu.RLock()
		mu := h.connMu[c]
		h.mu.RUnlock()
		if mu != nil {
			mu.Lock()
			_ = c.SetWriteDeadline(time.Now().Add(10 * time.Second))
			_ = writeJSON(c, m)
			mu.Unlock()
		}
	}
}

// broadcastRoster sends the current list of connected user names to all clients.
// Admins receive UID info and isAdmin flag; regular users only see names.
func (h *hub) broadcastRoster() {
	// Build roster snapshot with UID info
	h.mu.RLock()
	userListFull := make([]userInfo, 0, len(h.userName))
	userListNoUID := make([]userInfo, 0, len(h.userName))
	legacyUsers := make([]string, 0, len(h.userName))
	for uid, name := range h.userName {
		if set, ok := h.userConns[uid]; !ok || len(set) == 0 {
			continue
		}
		if name == "" {
			name = "anon"
		}
		userListFull = append(userListFull, userInfo{Name: name, UID: uid})
		userListNoUID = append(userListNoUID, userInfo{Name: name, UID: ""})
		legacyUsers = append(legacyUsers, name)
	}

	// Get all connections with their UIDs and admin status
	type connInfo struct {
		conn    *websocket.Conn
		mu      *sync.Mutex
		isAdmin bool
	}
	connInfos := make([]connInfo, 0, len(h.conns))
	for c := range h.conns {
		uid := h.connUID[c]
		mu := h.connMu[c]
		isAdmin := h.admins[uid]
		connInfos = append(connInfos, connInfo{conn: c, mu: mu, isAdmin: isAdmin})
	}
	h.mu.RUnlock()

	// Sort by name for stable UI order
	sort.Slice(userListFull, func(i, j int) bool {
		return userListFull[i].Name < userListFull[j].Name
	})
	sort.Slice(userListNoUID, func(i, j int) bool {
		return userListNoUID[i].Name < userListNoUID[j].Name
	})
	sort.Strings(legacyUsers)

	ts := time.Now().UTC()

	// Send different roster to admins vs regular users
	for _, ci := range connInfos {
		var msg message
		if ci.isAdmin {
			msg = message{
				TS:       ts,
				Event:    "roster",
				Users:    legacyUsers,
				UserList: userListFull,
				IsAdmin:  true,
			}
		} else {
			msg = message{
				TS:       ts,
				Event:    "roster",
				Users:    legacyUsers,
				UserList: userListNoUID,
				IsAdmin:  false,
			}
		}
		if ci.mu != nil {
			ci.mu.Lock()
			_ = ci.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			_ = writeJSON(ci.conn, msg)
			ci.mu.Unlock()
		}
	}
}

// attachStore connects a persistent store to the hub.
func (h *hub) attachStore(s *messageStore) {
	h.mu.Lock()
	h.store = s
	h.mu.Unlock()
}

// bootstrap preloads history into the in-memory buffer.
func (h *hub) bootstrap(msgs []message) {
	h.mu.Lock()
	h.messages = append(h.messages, msgs...)
	h.mu.Unlock()
}

// closeAll force-closes all active websocket connections (used during shutdown).
func (h *hub) closeAll() {
	h.mu.Lock()
	conns := make([]*websocket.Conn, 0, len(h.conns))
	for c := range h.conns {
		conns = append(conns, c)
	}
	h.mu.Unlock()
	for _, c := range conns {
		h.mu.RLock()
		mu := h.connMu[c]
		h.mu.RUnlock()
		if mu != nil {
			mu.Lock()
			_ = c.SetWriteDeadline(time.Now().Add(10 * time.Second))
			_ = c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseGoingAway, "server shutdown"))
			mu.Unlock()
		}
	}
}

// wait blocks until all websocket handler goroutines have finished.
func (h *hub) wait() {
	h.wg.Wait()
}

func handleWS(w http.ResponseWriter, r *http.Request, h *hub) {
	upgrader := websocket.Upgrader{
		CheckOrigin:      func(r *http.Request) bool { return true },
		ReadBufferSize:   1024,
		WriteBufferSize:  1024,
		HandshakeTimeout: 10 * time.Second,
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	// Set connection timeouts and keepalive
	_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	mu := &sync.Mutex{}
	h.mu.Lock()
	h.conns[conn] = struct{}{}
	h.connMu[conn] = mu
	backlog := append([]message(nil), h.messages...)
	h.mu.Unlock()

	for _, m := range backlog {
		mu.Lock()
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		_ = writeJSON(conn, m)
		mu.Unlock()
	}

	// Start ping ticker to keep connection alive
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	// Channel to signal when to stop the ping goroutine
	done := make(chan struct{})

	// Ping goroutine
	go func() {
		for {
			select {
			case <-ticker.C:
				mu.Lock()
				_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					mu.Unlock()
					return
				}
				mu.Unlock()
			case <-done:
				return
			}
		}
	}()

	h.wg.Add(1)
	go func() {
		defer func() {
			close(done)
			var leftUser string
			var uid string
			var lastConn bool
			h.mu.Lock()
			uid = h.connUID[conn]
			if uid != "" {
				if set, ok := h.userConns[uid]; ok {
					delete(set, conn)
					if len(set) == 0 {
						lastConn = true
						delete(h.userConns, uid)
					} else {
						h.userConns[uid] = set
					}
				}
				leftUser = h.userName[uid]
				if lastConn {
					delete(h.userName, uid)
				}
				delete(h.connUID, conn)
			}
			delete(h.conns, conn)
			delete(h.connMu, conn)
			h.mu.Unlock()
			if leftUser != "" && lastConn {
				h.broadcast(message{TS: time.Now().UTC(), User: leftUser, Event: "left"})
				h.broadcastRoster()
			}
			mu.Lock()
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			_ = conn.Close()
			mu.Unlock()
			h.wg.Done()
		}()
		for {
			var req struct {
				User  string `json:"user"`
				Text  string `json:"text"`
				UID   string `json:"uid"`
				Token string `json:"token"`
			}
			// Reset read deadline on each message
			_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
			if err := conn.ReadJSON(&req); err != nil {
				log.Debug().Err(err).Msg("[chat] failed to read JSON from client")
				return
			}
			// Sanitize nickname: limit length and remove control characters
			req.User = sanitizeString(req.User, 100)
			if req.User == "" {
				req.User = "anon"
			}
			// Sanitize message text: limit length and remove control characters
			// Allow larger limit for image messages (base64 encoded images can be ~500KB)
			textLimit := 10000
			if strings.HasPrefix(req.Text, "[IMAGE]") {
				textLimit = 600000 // ~600KB for base64 images
			}
			req.Text = sanitizeString(req.Text, textLimit)
			if req.UID == "" {
				// Fallback to a per-connection unique id if client didn't provide one
				req.UID = strconv.FormatInt(time.Now().UnixNano(), 10)
			}

			// Check if user is blocked
			if h.isBlocked(req.UID) {
				mu.Lock()
				_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "blocked"))
				mu.Unlock()
				log.Info().Str("uid", req.UID).Str("user", req.User).Msg("[chat] blocked user tried to connect")
				return
			}

			// Check for commands
			if req.Text == "/myuid" {
				// Send UID back to the user privately
				mu.Lock()
				_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				_ = writeJSON(conn, message{
					TS:    time.Now().UTC(),
					User:  "system",
					Text:  "Your UID: " + req.UID + " (stored in browser localStorage as 'simple-chat-uid')",
					Event: "",
				})
				mu.Unlock()
				continue
			}

			if req.Text == "/help" {
				// Show available commands
				helpText := "Available commands:\n" +
					"/myuid - Show your UID\n" +
					"/help - Show this help message"
				if h.isAdmin(req.UID) {
					helpText += "\n\nAdmin commands:\n" +
						"/block <uid> - Block a user by UID\n" +
						"/unblock <uid> - Unblock a user by UID\n" +
						"/kick - Permanently kick and block the most recently joined user"
				}

				mu.Lock()
				_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				_ = writeJSON(conn, message{
					TS:    time.Now().UTC(),
					User:  "system",
					Text:  helpText,
					Event: "",
				})
				mu.Unlock()
				continue
			}

			// Admin-only commands
			if h.isAdmin(req.UID) {
				if strings.HasPrefix(req.Text, "/block ") {
					targetUID := strings.TrimSpace(strings.TrimPrefix(req.Text, "/block "))
					h.blockUser(targetUID)
					// Notify admin
					mu.Lock()
					_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
					_ = writeJSON(conn, message{
						TS:    time.Now().UTC(),
						User:  "system",
						Text:  "Blocked UID: " + targetUID,
						Event: "",
					})
					mu.Unlock()
					continue
				}
				if strings.HasPrefix(req.Text, "/unblock ") {
					targetUID := strings.TrimSpace(strings.TrimPrefix(req.Text, "/unblock "))
					h.unblockUser(targetUID)
					// Notify admin
					mu.Lock()
					_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
					_ = writeJSON(conn, message{
						TS:    time.Now().UTC(),
						User:  "system",
						Text:  "Unblocked UID: " + targetUID,
						Event: "",
					})
					mu.Unlock()
					continue
				}
				if req.Text == "/kick" {
					targetUID := h.getLastJoinedUID()
					if targetUID == "" {
						mu.Lock()
						_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
						_ = writeJSON(conn, message{
							TS:    time.Now().UTC(),
							User:  "system",
							Text:  "No users to kick (all users are admins or no users online)",
							Event: "",
						})
						mu.Unlock()
						continue
					}
					h.mu.RLock()
					targetName := h.userName[targetUID]
					h.mu.RUnlock()
					// Permanently block the user (add to blacklist)
					h.blockUser(targetUID)
					// Notify admin
					mu.Lock()
					_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
					_ = writeJSON(conn, message{
						TS:    time.Now().UTC(),
						User:  "system",
						Text:  "Kicked and blocked user: " + targetName + " (UID: " + targetUID + ")",
						Event: "",
					})
					mu.Unlock()
					continue
				}
			}

			// map connection to uid and maintain per-user state
			var announce bool
			var renamed bool
			var newToken string
			var tokenInvalid bool
			h.mu.Lock()
			if _, ok := h.connUID[conn]; !ok {
				// Token validation
				existingToken, hasToken := h.uidTokens[req.UID]
				if hasToken {
					// UID already has a token - validate
					if req.Token != existingToken {
						// Token mismatch - reject connection
						tokenInvalid = true
						log.Warn().Str("uid", req.UID).Msg("[chat] token mismatch, rejecting connection")
					}
				} else {
					// First time for this UID - generate token
					newToken = generateToken()
					h.uidTokens[req.UID] = newToken
				}

				if !tokenInvalid {
					h.connUID[conn] = req.UID
					if _, ok := h.userConns[req.UID]; !ok {
						h.userConns[req.UID] = map[*websocket.Conn]struct{}{}
					}
					if len(h.userConns[req.UID]) == 0 {
						announce = true
						// Add to join order when user first joins
						h.joinOrder = append(h.joinOrder, req.UID)
					}
					h.userConns[req.UID][conn] = struct{}{}
				}
			}
			if !tokenInvalid {
				if cur, ok := h.userName[req.UID]; !ok {
					h.userName[req.UID] = req.User
				} else if cur != req.User {
					h.userName[req.UID] = req.User
					renamed = true
				}
			}
			h.mu.Unlock()

			// Reject invalid token
			if tokenInvalid {
				mu.Lock()
				_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				_ = writeJSON(conn, message{
					TS:    time.Now().UTC(),
					User:  "system",
					Text:  "Connection rejected: invalid session token",
					Event: "",
				})
				_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "invalid token"))
				mu.Unlock()
				return
			}

			// Send new token to client if generated
			if newToken != "" {
				mu.Lock()
				_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				_ = writeJSON(conn, message{
					TS:    time.Now().UTC(),
					Event: "token",
					Token: newToken,
				})
				mu.Unlock()
			}

			if announce {
				h.broadcast(message{TS: time.Now().UTC(), User: req.User, Event: "joined"})
				h.broadcastRoster()
			} else if renamed {
				// Only update roster, don't announce rename in chat
				h.broadcastRoster()
			}
			if req.Text == "" {
				continue
			}
			h.broadcast(message{TS: time.Now().UTC(), User: req.User, Text: req.Text})
		}
	}()
}

func serveIndex(w http.ResponseWriter, tmpl *template.Template, name string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = tmpl.Execute(w, struct{ Name string }{Name: name})
}

// serveChatHTTP starts serving the chat UI and websocket endpoint and returns the server.
// Callers are responsible for shutting it down via Server.Shutdown.
// NewHandler builds the chat HTTP router (UI + websocket)
func NewHandler(name string, h *hub, staticFS fs.FS) http.Handler {
	indexTmpl := template.Must(template.ParseFS(staticFS, "index.html"))

	r := chi.NewRouter()
	r.Get("/", func(w http.ResponseWriter, r *http.Request) { serveIndex(w, indexTmpl, name) })
	r.Get("/ws", func(w http.ResponseWriter, r *http.Request) { handleWS(w, r, h) })
	// Serve embedded static files
	staticHandler := http.FileServer(http.FS(staticFS))
	r.Handle("/static/*", http.StripPrefix("/static/", staticHandler))
	return r
}
