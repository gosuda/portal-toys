package main

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	sdk "github.com/gosuda/relaydns/sdk"
	_ "github.com/mattn/go-sqlite3"
)

//go:embed public
var embeddedPublic embed.FS

var (
	publicSub     fs.FS
	staticHandler http.Handler
)

var db *sql.DB

const createTableSQL = `
CREATE TABLE IF NOT EXISTS messages (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	nickname TEXT,
	content TEXT,
	timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
);`

type JSONMessage struct {
	Nickname  string    `json:"nickname"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

type DBMessage struct {
	Nickname  sql.NullString
	Content   string
	Timestamp time.Time
}

type APIResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

var rootCmd = &cobra.Command{
	Use:   "relaydns-rolling-paper",
	Short: "RelayDNS demo: Rolling Paper (relay HTTP backend)",
	RunE:  runRollingPaper,
}

var (
	flagServerURL string
	flagPort      int
	flagName      string
)

func init() {
	flags := rootCmd.PersistentFlags()
	flags.StringVar(&flagServerURL, "server-url", "ws://localhost:4017/relay", "relay websocket URL")
	flags.IntVar(&flagPort, "port", 3000, "local HTTP port (optional)")
	flags.StringVar(&flagName, "name", "rolling-paper", "backend display name")
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

	// Init DB
	initDB()
	defer db.Close()

	// HTTP mux
	mux := http.NewServeMux()
	mux.HandleFunc("/", rootHandler)

	// Prepare embedded static files
	sub, err := fs.Sub(embeddedPublic, "public")
	if err != nil {
		return fmt.Errorf("embed sub FS: %w", err)
	}
	publicSub = sub
	staticHandler = http.FileServer(http.FS(publicSub))

	// Relay client using http-backend pattern
	client, err := sdk.NewClient(func(c *sdk.RDClientConfig) {
		c.BootstrapServers = []string{flagServerURL}
	})
	if err != nil {
		return fmt.Errorf("new client: %w", err)
	}
	defer client.Close()

	cred := sdk.NewCredential()
	listener, err := client.Listen(cred, flagName, []string{"http/1.1"})
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	// Serve over relay
	go func() {
		if err := http.Serve(listener, mux); err != nil && err != http.ErrServerClosed && ctx.Err() == nil {
			log.Error().Err(err).Msg("[rolling-paper] relay http serve error")
		}
	}()

	// Optional local serve
	var httpSrv *http.Server
	if flagPort > 0 {
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
		_ = listener.Close()
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
	db, err = sql.Open("sqlite3", "./rollpaper.db")
	if err != nil {
		log.Fatal().Err(err).Msg("open sqlite")
	}

	if _, err = db.Exec(createTableSQL); err != nil {
		log.Fatal().Err(err).Msg("create table")
	}
}

func extractAPIPart(path string) (string, bool) {
	re := regexp.MustCompile(`^/peer/[a-zA-Z0-9]{40,}/(api/.*)$`)
	matches := re.FindStringSubmatch(path)
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

	nickname := strings.TrimSpace(r.FormValue("nickname"))
	var nick interface{}
	if nickname != "" {
		nick = nickname
	}

	_, err := db.Exec(
		"INSERT INTO messages (nickname, content, timestamp) VALUES (?, ?, ?)",
		nick,
		content,
		time.Now(),
	)
	if err != nil {
		log.Error().Err(err).Msg("db insert failed")
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

	rows, err := db.Query("SELECT nickname, content, timestamp FROM messages ORDER BY timestamp DESC")
	if err != nil {
		log.Error().Err(err).Msg("db query failed")
		sendJSONError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var messages []JSONMessage
	for rows.Next() {
		var dbMsg DBMessage
		if err := rows.Scan(&dbMsg.Nickname, &dbMsg.Content, &dbMsg.Timestamp); err != nil {
			log.Warn().Err(err).Msg("scan failed")
			continue
		}

		msg := JSONMessage{
			Content:   dbMsg.Content,
			Timestamp: dbMsg.Timestamp,
		}
		if dbMsg.Nickname.Valid {
			msg.Nickname = dbMsg.Nickname.String
		}

		messages = append(messages, msg)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(messages); err != nil {
		log.Error().Err(err).Msg("json encode failed")
	}
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
		}
	}

	if strings.HasPrefix(r.URL.Path, "/peer/") && r.URL.Path != "/peer/" {
		// SPA fallback to embedded index.html
		b, err := fs.ReadFile(publicSub, "index.html")
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
