package main

import (
	"context"
	crand "crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/cockroachdb/pebble"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"gosuda.org/portal/sdk"
)

//go:embed static
var embeddedPublic embed.FS

var (
	staticSub     fs.FS
	staticHandler http.Handler
	db            *pebble.DB
)

const (
	keyMsgPrefix = "m:"
	keyVoteCnt   = "v:"
	keyVoteSess  = "vs:"
	keyVoteIP    = "vi:"
)

type JSONMessage struct {
	ID        string    `json:"id,omitempty"`
	Nickname  string    `json:"nickname"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
	VoteCount int       `json:"voteCount,omitempty"`
}

// Stored value format: JSON-encoded JSONMessage
type APIResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

var rootCmd = &cobra.Command{
	Use:   "rolling-paper",
	Short: "Portal demo: Rolling Paper (relay HTTP backend)",
	RunE:  runRollingPaper,
}

var (
	flagServerURLs    []string
	flagPort          int
	flagName          string
	flagVoteThreshold int
	flagMaxLen        int
)

func init() {
	flags := rootCmd.PersistentFlags()
	flags.StringSliceVar(&flagServerURLs, "server-url", strings.Split(os.Getenv("RELAY"), ","), "relay websocket URL(s); repeat or comma-separated (from env RELAY/RELAY_URL if set)")
	flags.IntVar(&flagPort, "port", -1, "optional local HTTP port (negative to disable)")
	flags.StringVar(&flagName, "name", "rolling-paper", "backend display name")
	flags.IntVar(&flagVoteThreshold, "delete-threshold", 3, "votes required to delete (>=1)")
	flags.IntVar(&flagMaxLen, "max-len", 2500, "maximum message length in characters (>=1)")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("execute rolling-paper command")
	}
}

func runRollingPaper(cmd *cobra.Command, args []string) error {
	// Cancellation context
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Init DB (Pebble)
	initDB()
	defer db.Close()

	// HTTP mux
	mux := http.NewServeMux()
	mux.HandleFunc("/", rootHandler)

	// Prepare embedded static files
	sub, err := fs.Sub(embeddedPublic, "static")
	if err != nil {
		return fmt.Errorf("embed sub FS: %w", err)
	}
	staticHandler = http.FileServer(http.FS(sub))

	// Relay clients and listeners (multi-relay)
	cred := sdk.NewCredential()
	client, err := sdk.NewClient(func(c *sdk.RDClientConfig) { c.BootstrapServers = flagServerURLs })
	if err != nil {
		return fmt.Errorf("new client: %w", err)
	}
	ln, err := client.Listen(cred, flagName, []string{"http/1.1"})
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	// Serve over each relay listener
	go func() {
		if err := http.Serve(ln, mux); err != nil && err != http.ErrServerClosed && ctx.Err() == nil {
			log.Error().Err(err).Str("listener", ln.Addr().String()).Msg("[rolling-paper] relay http serve error")
		}
	}()

	// Optional local serve
	var httpSrv *http.Server
	if flagPort >= 0 {
		httpSrv = &http.Server{Addr: fmt.Sprintf(":%d", flagPort), Handler: mux}
		log.Info().Msgf("[rolling-paper] serving locally at http://127.0.0.1:%d", flagPort)
		go func() {
			if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Warn().Err(err).Msg("[rolling-paper] local http stopped")
			}
		}()
	}

	// Unified shutdown watcher
	go func() {
		<-ctx.Done()
		_ = ln.Close()
		_ = client.Close()
		if httpSrv != nil {
			sctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := httpSrv.Shutdown(sctx); err != nil && err != context.Canceled {
				log.Warn().Err(err).Msg("[rolling-paper] local http shutdown error")
			}
		}
	}()

	<-ctx.Done()
	log.Info().Msg("[rolling-paper] shutdown complete")
	return nil
}

func initDB() {
	var err error
	db, err = pebble.Open("./rolling-paper/db", &pebble.Options{})
	if err != nil {
		log.Fatal().Err(err).Msg("open pebble")
	}
}

var apiPathRe = regexp.MustCompile(`^/peer/[^/]+/(api/.*)$`)

func extractAPIPart(path string) (string, bool) {
	// Accept peer IDs containing base64/base32-like characters and padding.
	matches := apiPathRe.FindStringSubmatch(path)
	if len(matches) > 1 {
		return "/" + matches[1], true
	}
	if strings.HasPrefix(path, "/api/") {
		return path, true
	}
	return "", false
}

func handleSubmit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "invalid method", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseForm(); err != nil {
		sendJSONError(w, "bad form", http.StatusBadRequest)
		return
	}

	content := strings.TrimSpace(r.FormValue("message"))
	if content == "" {
		sendJSONError(w, "message required", http.StatusBadRequest)
		return
	}
	if utf8.RuneCountInString(content) > currentMaxLen() {
		sendJSONError(w, fmt.Sprintf("Too long message (maximum %d characters)", currentMaxLen()), http.StatusBadRequest)
		return
	}

	nickname := strings.TrimSpace(r.FormValue("nickname"))

	now := time.Now().UTC()
	msg := JSONMessage{
		Nickname:  nickname,
		Content:   content,
		Timestamp: now,
	}
	val, err := json.Marshal(msg)
	if err != nil {
		log.Error().Err(err).Msg("json marshal failed")
		sendJSONError(w, "store failed", http.StatusInternalServerError)
		return
	}

	key := makeMsgKey(now)
	if err := db.Set([]byte(key), val, nil); err != nil {
		log.Error().Err(err).Msg("pebble set failed")
		sendJSONError(w, "store failed", http.StatusInternalServerError)
		return
	}

	sendJSONResponse(w, APIResponse{Status: "success", Message: "ok"})
}

func handleGetMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "invalid method", http.StatusMethodNotAllowed)
		return
	}

	// Iterate Pebble keys with prefix keyMsgPrefix (newest-first by reversed timestamp)
	iter, err := db.NewIter(&pebble.IterOptions{
		LowerBound: []byte(keyMsgPrefix),
		UpperBound: []byte("m;"),
	})
	if err != nil {
		log.Error().Err(err).Msg("pebble new iter failed")
		return
	}
	defer iter.Close()

	var messages []JSONMessage
	for ok := iter.First(); ok; ok = iter.Next() {
		var msg JSONMessage
		if err := json.Unmarshal(iter.Value(), &msg); err != nil {
			log.Warn().Err(err).Msg("json decode failed")
			continue
		}
		// Attach ID from key and current vote count
		k := string(iter.Key())
		msg.ID = k
		if vc, err := getVoteCount(k); err == nil {
			msg.VoteCount = vc
		}
		messages = append(messages, msg)
	}

	writeJSON(w, map[string]any{"messages": messages, "threshold": currentThreshold(), "maxLen": currentMaxLen()})
}

func sendJSONError(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(APIResponse{Status: "error", Message: message})
}

func sendJSONResponse(w http.ResponseWriter, data APIResponse) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(data)
}

func rootHandler(w http.ResponseWriter, r *http.Request) {
	apiPath, isAPI := extractAPIPart(r.URL.Path)

	if isAPI {
		switch apiPath {
		case "/api/submit":
			handleSubmit(w, r)
			return
		case "/api/messages":
			handleGetMessages(w, r)
			return
		case "/api/vote-delete":
			handleVoteDelete(w, r)
			return
		case "/api/proxy":
			handleProxy(w, r)
			return
		}
	}

	if strings.HasPrefix(r.URL.Path, "/peer/") && r.URL.Path != "/peer/" {
		// SPA fallback to embedded index.html
		b, err := fs.ReadFile(staticSub, "index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(b)
		return
	}

	// Serve embedded static files
	staticHandler.ServeHTTP(w, r)
}

// getVoteCount reads the vote count for the given message key (string form of the key)
func getVoteCount(msgKey string) (int, error) {
	vKey := []byte(keyVoteCnt + msgKey)
	b, closer, err := db.Get(vKey)
	if err != nil {
		if err == pebble.ErrNotFound {
			return 0, nil
		}
		return 0, err
	}
	defer closer.Close()
	var n int
	if err := json.Unmarshal(b, &n); err != nil {
		return 0, err
	}
	return n, nil
}

func setVoteCount(msgKey string, n int) error {
	vKey := []byte(keyVoteCnt + msgKey)
	b, _ := json.Marshal(n)
	return db.Set(vKey, b, nil)
}

func handleVoteDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "invalid method", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		sendJSONError(w, "bad form", http.StatusBadRequest)
		return
	}
	id := strings.TrimSpace(r.FormValue("id"))
	if id == "" {
		sendJSONError(w, "id required", http.StatusBadRequest)
		return
	}
	// Session + IP based dedupe
	sid := getOrSetSessionID(w, r)
	ip := getClientIP(r)
	vsKey := []byte(keyVoteSess + id + ":" + sid)
	viKey := []byte(keyVoteIP + id + ":" + ip)
	// If either already exists, it's a duplicate vote
	if exists(vsKey) || exists(viKey) {
		count, _ := getVoteCount(id)
		respondVote(w, false, count)
		return
	}
	// Check message exists
	if _, closer, err := db.Get([]byte(id)); err != nil {
		if err == pebble.ErrNotFound {
			sendJSONError(w, "not found", http.StatusNotFound)
			return
		}
		log.Error().Err(err).Msg("pebble get failed")
		sendJSONError(w, "internal error", http.StatusInternalServerError)
		return
	} else {
		closer.Close()
	}

	// Increment vote count (non-atomic RMW for demo simplicity)
	count, err := getVoteCount(id)
	if err != nil {
		log.Error().Err(err).Msg("read vote count failed")
		sendJSONError(w, "internal error", http.StatusInternalServerError)
		return
	}
	count++
	if err := setVoteCount(id, count); err != nil {
		log.Error().Err(err).Msg("write vote count failed")
		sendJSONError(w, "internal error", http.StatusInternalServerError)
		return
	}
	// Mark dedupe keys
	_ = db.Set(vsKey, []byte("1"), nil)
	_ = db.Set(viKey, []byte("1"), nil)

	// If threshold reached, delete message and votes
	deleted := false
	threshold := currentThreshold()
	if count >= threshold {
		if err := db.Delete([]byte(id), nil); err != nil {
			log.Error().Err(err).Msg("delete message failed")
			sendJSONError(w, "delete failed", http.StatusInternalServerError)
			return
		}
		if err := db.Delete([]byte(keyVoteCnt+id), nil); err != nil && err != pebble.ErrNotFound {
			log.Warn().Err(err).Msg("delete vote key failed")
		}
		// Best-effort cleanup of dedupe keys (optional, not exhaustive scan)
		_ = db.Delete(vsKey, nil)
		_ = db.Delete(viKey, nil)
		deleted = true
	}

	respondVote(w, deleted, count)
}

func respondVote(w http.ResponseWriter, deleted bool, count int) {
	threshold := currentThreshold()
	writeJSON(w, map[string]any{
		"status":    "success",
		"deleted":   deleted,
		"voteCount": count,
		"threshold": threshold,
	})
}

func currentThreshold() int {
	if flagVoteThreshold < 1 {
		return 1
	}
	return flagVoteThreshold
}

func currentMaxLen() int {
	if flagMaxLen < 1 {
		return 1
	}
	return flagMaxLen
}

func getClientIP(r *http.Request) string {
	// Prefer X-Forwarded-For if present
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// take first IP
		parts := strings.Split(xff, ",")
		ip := strings.TrimSpace(parts[0])
		if ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func getOrSetSessionID(w http.ResponseWriter, r *http.Request) string {
	const cookieName = "rp_sid"
	if c, err := r.Cookie(cookieName); err == nil && c.Value != "" {
		return c.Value
	}
	// generate 16 random bytes
	var b [16]byte
	if _, err := crand.Read(b[:]); err != nil {
		// fallback to time-based rand
		for i := range b {
			b[i] = byte(rand.Intn(256))
		}
	}
	sid := hex.EncodeToString(b[:])
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    sid,
		Path:     "/",
		HttpOnly: true,
		MaxAge:   60 * 60 * 24 * 180, // ~180 days
		SameSite: http.SameSiteLaxMode,
	})
	return sid
}

// Helpers
func makeMsgKey(t time.Time) string {
	// Key layout: "m:<rev_ts_hex>:<rand_hex>"
	// rev_ts ensures lexicographic ascending order == newest first
	revTs := ^uint64(t.UnixNano())
	return fmt.Sprintf(keyMsgPrefix+"%016x:%08x", revTs, rand.Uint32())
}

func exists(key []byte) bool {
	if _, c, err := db.Get(key); err == nil {
		c.Close()
		return true
	}
	return false
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// --- Simple streaming proxy for media to mitigate CORS/hotlinking ---
// WARNING: This is a minimal demo proxy; it includes basic SSRF protections.
// Do NOT expose publicly without adding proper allowlists and rate limiting.
func handleProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	raw := strings.TrimSpace(r.URL.Query().Get("u"))
	if raw == "" {
		http.Error(w, "missing u", http.StatusBadRequest)
		return
	}
	if len(raw) > 2048 {
		http.Error(w, "url too long", http.StatusBadRequest)
		return
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		http.Error(w, "invalid url", http.StatusBadRequest)
		return
	}
	// Basic SSRF guard: block localhost/private/multicast/link-local
	if !hostIsPublic(u.Hostname()) {
		http.Error(w, "forbidden host", http.StatusForbidden)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	req.Header.Set("User-Agent", "rolling-paper-proxy/1.0")
	// Best-effort: suppress referrer upstream
	req.Header.Set("Referer", "")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, "upstream error", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		http.Error(w, "upstream status", http.StatusBadGateway)
		return
	}
	// Pass through limited headers
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	// Limit size to 10MB for demo
	const max = 10 << 20
	n, err := io.CopyN(w, resp.Body, max)
	if err != nil {
		if err == io.EOF {
			return
		}
		_ = n
		// Truncated or client closed; no extra handling
	}
}

func hostIsPublic(host string) bool {
	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return false
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() {
			return false
		}
		if isPrivateIP(ip) {
			return false
		}
	}
	return true
}

func isPrivateIP(ip net.IP) bool {
	if ip4 := ip.To4(); ip4 != nil {
		switch {
		case ip4[0] == 10:
			return true
		case ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31:
			return true
		case ip4[0] == 192 && ip4[1] == 168:
			return true
		case ip4[0] == 127:
			return true
		case ip4[0] == 169 && ip4[1] == 254: // link-local
			return true
		}
		return false
	}
	// IPv6 private/multicast/link-local checks
	if ip.IsLoopback() || ip.IsLinkLocalMulticast() || ip.IsLinkLocalUnicast() || ip.IsMulticast() {
		return true
	}
	// Unique local address fc00::/7
	if len(ip) == net.IPv6len && (ip[0]&0xfe) == 0xfc {
		return true
	}
	return false
}
