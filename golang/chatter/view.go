package main

import (
	"encoding/json"
	"html/template"
	"net/http"
	"os/exec"
	"io"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
	"github.com/creack/pty"
)

func handleWS(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	wsConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error().Err(err).Msg("Failed to upgrade connection")
		return
	}
	defer wsConn.Close()

	// Start a new shell
	cmd := exec.Command("/bin/bash") // Or /bin/sh, or whatever shell you prefer
	
	// Start the command with a pty
	ptmx, err := pty.Start(cmd)
	if err != nil {
		log.Error().Err(err).Msg("Failed to start pty")
		wsConn.WriteMessage(websocket.TextMessage, []byte("Failed to start terminal"))
		return
	}
	defer func() {
		_ = ptmx.Close()
		_ = cmd.Wait()
	}()

	// Goroutine to read from pty and write to WebSocket
	go func() {
		_, _ = io.Copy(wsConn.UnderlyingConn(), ptmx)
	}()

	// Goroutine to read from WebSocket and write to pty
	for {
		messageType, p, err := wsConn.ReadMessage()
		if err != nil {
			log.Error().Err(err).Msg("Failed to read message from websocket")
			return
		}

		if messageType == websocket.TextMessage {
			// Handle terminal resize messages
			var msg struct {
				Type string `json:"type"`
				Cols uint16 `json:"cols"`
				Rows uint16 `json:"rows"`
			}
			if err := json.Unmarshal(p, &msg); err == nil && msg.Type == "resize" {
				_ = pty.Setsize(ptmx, &pty.Winsize{Cols: msg.Cols, Rows: msg.Rows})
			} else {
				_, _ = ptmx.Write(p)
			}
		} else if messageType == websocket.BinaryMessage {
			_, _ = ptmx.Write(p)
		}
	}
}

func serveIndex(w http.ResponseWriter, r *http.Request, name string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = indexTmpl.Execute(w, struct{ Name string }{Name: name})
}

// NewHandler builds the chat HTTP router (UI + websocket)
func NewHandler(name string) http.Handler {
	r := chi.NewRouter()
	r.Get("/", func(w http.ResponseWriter, r *http.Request) { serveIndex(w, r, name) })
	r.Get("/ws", func(w http.ResponseWriter, r *http.Request) { handleWS(w, r) })
	return r
}

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
        // Customize xterm.js to support 256 colors and a better theme
        fontFamily: 'D2Coding, ui-monospace, SFMono-Regular, Menlo, Consolas, monospace',
        fontSize: 14,
        theme: {
            background: '#111827', // var(--panel)
            foreground: '#e5e7eb', // var(--fg)
            cursor: '#22c55e',     // var(--cursor)
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
        cols: 80, // Initial columns, will be adjusted by fit addon
        rows: 24  // Initial rows, will be adjusted by fit addon
    });

    const fitAddon = new FitAddon.FitAddon();
    term.loadAddon(fitAddon);

    term.open(terminalContainer);
    fitAddon.fit();

    let ws = null;

    function connectWebSocket() {
      if (ws && (ws.readyState === WebSocket.CONNECTING || ws.readyState === WebSocket.OPEN)) {
        return;
      }

      ws = new WebSocket(wsURL);

      ws.onopen = () => {
        // Send initial terminal size to the backend
        sendTerminalSize();
        term.focus();
      };

      ws.onmessage = (event) => {
        term.write(event.data);
      };

      ws.onclose = (event) => {
        term.write('\r\nDisconnected. Reconnecting...\r\n');
        setTimeout(() => {
          connectWebSocket();
        }, 1000);
      };

      ws.onerror = (event) => {
        console.error('WebSocket error:', event);
        term.write('\r\nWebSocket error. Reconnecting...\r\n');
        ws.close();
      };
    }

    term.onData(e => {
      if (ws && ws.readyState === WebSocket.OPEN) {
        ws.send(e);
      }
    });

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

    window.addEventListener('resize', () => {
        fitAddon.fit();
        sendTerminalSize();
    });

    // Initial connection
    connectWebSocket();
  </script>
</body>
</html>`))