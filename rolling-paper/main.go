package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	sdk "github.com/gosuda/relaydns/sdk/go"
	_ "github.com/mattn/go-sqlite3"
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

func main() {
	ctx := context.Background()

	cli, err := sdk.NewClient(ctx, sdk.ClientConfig{
		Name:      "썸고롤링페이퍼",
		TargetTCP: "127.0.0.1:3000",
		ServerURL: "http://relaydns.gosuda.org",
	})
	if err != nil {
		log.Fatal(err)
	}

	if err := cli.Start(ctx); err != nil {
		log.Fatal(err)
	}
	defer func() { _ = cli.Close() }()

	initDB()
	defer db.Close()

	http.HandleFunc("/", rootHandler)

	const port = "3000"
	log.Printf("rollpaper server listening on http://localhost:%s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}

func initDB() {
	var err error
	db, err = sql.Open("sqlite3", "./rollpaper.db")
	if err != nil {
		log.Fatal(err)
	}

	if _, err = db.Exec(createTableSQL); err != nil {
		log.Fatalf("create table: %v", err)
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
		log.Printf("db insert failed: %v", err)
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
		log.Printf("db query failed: %v", err)
		sendJSONError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var messages []JSONMessage
	for rows.Next() {
		var dbMsg DBMessage
		if err := rows.Scan(&dbMsg.Nickname, &dbMsg.Content, &dbMsg.Timestamp); err != nil {
			log.Printf("scan failed: %v", err)
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
		log.Printf("json encode failed: %v", err)
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
		http.ServeFile(w, r, "./public/index.html")
		return
	}

	http.FileServer(http.Dir("./public")).ServeHTTP(w, r)
}
