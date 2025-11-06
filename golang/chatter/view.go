package main

import (
	"encoding/json"
	"html/template"
	"io"
	"net/http"
	"os/exec"

	"github.com/creack/pty"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

// handleWS upgrades the HTTP connection to WebSocket and bridges it with a pty.
func handleWS(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true }, // Allow all origins
	}

	wsConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error().Err(err).Msg("Failed to upgrade connection")
		return
	}
	defer wsConn.Close()

	cmd := exec.Command("/bin/bash")

	// Start the command in a pty.
	// pty.Start handles session management and ctty setup automatically.
	ptmx, err := pty.Start(cmd)
	if err != nil {
		log.Error().Err(err).Msg("Failed to start pty")
		wsConn.WriteMessage(websocket.TextMessage, []byte("Failed to start terminal"))
		return
	}
	defer func() {
		_ = ptmx.Close()
		_ = cmd.Wait() // Ensure the child process is reaped
	}()

	// Goroutine to stream pty output to the WebSocket client
	go func() {
		defer wsConn.Close()
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if err != nil {
				// io.EOF is expected when the pty closes (shell exits)
				if err != io.EOF {
					log.Error().Err(err).Msg("Failed to read from pty")
				}
				return
			}
			// Send pty output as a WebSocket binary message
			if err := wsConn.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
				log.Error().Err(err).Msg("Failed to write to websocket")
				return
			}
		}
	}()

	// Read loop for WebSocket messages (client input)
	for {
		messageType, p, err := wsConn.ReadMessage()
		if err != nil {
			log.Info().Err(err).Msg("Websocket read loop finished")
			return // Connection closed
		}

		if messageType == websocket.TextMessage {
			// Check for a JSON resize message
			var msg struct {
				Type string `json:"type"`
				Cols uint16 `json:"cols"`
				Rows uint16 `json:"rows"`
			}
			if err := json.Unmarshal(p, &msg); err == nil && msg.Type == "resize" {
				// Apply window size to the pty
				_ = pty.Setsize(ptmx, &pty.Winsize{Cols: msg.Cols, Rows: msg.Rows})
			} else {
				// Forward standard text input to the pty
				_, _ = ptmx.Write(p)
			}
		} else if messageType == websocket.BinaryMessage {
			// Forward binary input (less common) to the pty
			_, _ = ptmx.Write(p)
		}
	}
}

// serveIndex serves the main HTML page.
func serveIndex(w http.ResponseWriter, r *http.Request, name string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = indexTmpl.Execute(w, struct{ Name string }{Name: name})
}

// NewHandler builds the main HTTP router.
func NewHandler(name string) http.Handler {
	r := chi.NewRouter()
	r.Get("/", func(w http.ResponseWriter, r *http.Request) { serveIndex(w, r, name) })
	r.Get("/ws", func(w http.ResponseWriter, r *http.Request) { handleWS(w, r) })
	return r
}

// indexTmpl contains the client-side HTML, CSS, and JS for xterm.js.
var indexTmpl = template.Must(template.New("chat").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1" />
<title>Terminal — {{.Name}}</title>
<link rel="stylesheet" href="https://unpkg.com/xterm@5.3.0/css/xterm.css" />
<script src="https://unpkg.com/xterm@5.3.0/lib/xterm.js"></script>
<script src="https://unpkg.com/xterm-addon-fit@0.8.0/lib/xterm-addon-fit.js"></script>
<style>
:root{
--bg: #0d1117;
--panel: #111827;
--border: #1f2937;
--fg: #e5e7eb;
--muted: #9ca3af;
--accent: #22c55e;
--cursor: #22c55e;
}
*{
box-sizing: border-box
}
html, body {
height: 100%;
margin: 0;
overflow: hidden; /* Prevent scrollbars on body */
}
body {
padding:24px;
background:var(--bg);
color:var(--fg);
font-family: ui-sans-serif, system-ui, -apple-system, Segoe UI, Roboto, Helvetica, Arial
}
.wrap {
max-width: 920px;
margin: 0 auto;
height: calc(100% - 48px); /* Adjust height for padding */
display: flex;
flex-direction: column;
}
h1 {
margin:0 0 12px 0;
font-weight:700
}
.term-container {
flex-grow: 1;
border:1px solid var(--border);
border-radius:10px;
background:var(--panel);
overflow: hidden;
position: relative;
}
.xterm .xterm-viewport {
overflow-y: auto;
}
</style>
</head>
<body>
<div class="wrap">
<h1>🔐 Terminal — {{.Name}}</h1>
<div id="terminal-container" class="term-container"></div>
</div>
<script>
const terminalContainer = document.getElementById('terminal-container');
const wsURL = (window.location.protocol === 'https:' ? 'wss://' : 'ws://') + window.location.host + '/ws';

const term = new Terminal({
// Customize xterm.js
fontFamily: 'D2Coding, ui-monospace, SFMono-Regular, Menlo, Consolas, monospace',
fontSize: 14,
theme: {
background: '#111827',
foreground: '#e5e7eb',
	cursor: '#22c55e',
	selectionBackground: '#334155',
	black: '#000000',
	red: '#ef4444',
	green: '#22c55e',
	yellow: '#eab308',
	blue: '#3b82f6',
	magenta: '#a855f7',
	cyan: '#06b6d4',
	white: '#ffffff',
	brightBlack: '#6b7280',
	brightRed: '#f87171',
	brightGreen: '#4ade80',
	brightYellow: '#facc15',
	brightBlue: '#60a5fa',
	brightMagenta: '#c084fc',
	brightCyan: '#22d3ee',
	brightWhite: '#f3f4f6'
},
cursorBlink: true,
cols: 80,
rows: 24
});

// Use FitAddon to resize terminal to container
const fitAddon = new FitAddon.FitAddon();
term.loadAddon(fitAddon);

term.open(terminalContainer);
fitAddon.fit(); // Fit to container size

let ws = null;

function connectWebSocket() {
if (ws && (ws.readyState === WebSocket.CONNECTING || ws.readyState === WebSocket.OPEN)) {
	return;
	}

	ws = new WebSocket(wsURL);

ws.onopen = () => {
sendTerminalSize(); // Send initial size
term.focus();
};

ws.onmessage = (event) => {
term.write(event.data); // Write pty output to terminal
};

ws.onclose = (event) => {
term.write('\r\nDisconnected. Reconnecting...\r\n');
setTimeout(connectWebSocket, 1000); // Reconnect logic
};

ws.onerror = (event) => {
console.error('WebSocket error:', event);
term.write('\r\nWebSocket error. Reconnecting...\r\n');
ws.close(); // Triggers onclose
};
}

// Send terminal input to WebSocket
term.onData(e => {
if (ws && ws.readyState === WebSocket.OPEN) {
	ws.send(e);
}
});

// Send terminal resize info to backend
function sendTerminalSize() {
if (ws && ws.readyState === WebSocket.OPEN) {
	const size = {
	type: "resize",
	cols: term.cols,
	rows: term.rows
	};
	ws.send(JSON.stringify(size));
}
}

// Handle browser window resize
window.addEventListener('resize', () => {
fitAddon.fit();
sendTerminalSize();
});

// Start the connection
connectWebSocket();
</script>
</body>
</html>`))
