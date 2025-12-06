package main

import (
	"html/template"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// serveClientHTTP builds the local backend router using chi.
// getStatus should return a short string like "Connected" or "Connecting...".
func NewHandler(name string) http.Handler {
	r := chi.NewRouter()
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		data := struct {
			Now  string
			Name string
		}{
			Now:  time.Now().Format(time.RFC1123),
			Name: name,
		}
		_ = clientPage.Execute(w, data)
	})
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return r
}

var clientPage = template.Must(template.New("index").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <title>Portal Backend</title>
  <style>
    body { font-family: sans-serif; background: #f9f9f9; padding: 40px; }
    h1 { color: #333; }
    footer { margin-top: 40px; color: #666; font-size: 0.9em; }
    .card { background: white; border-radius: 12px; padding: 24px; box-shadow: 0 2px 6px rgba(0,0,0,0.1); }
    .stat { display:inline-flex; align-items:center; gap:8px; padding:6px 10px; border-radius:999px; font-weight:700; font-size:14px }
    .stat.connected { background:#ecfdf5; color:#065f46 }
    .stat.disconnected { background:#fee2e2; color:#b91c1c }
    .stat .dot { width:8px; height:8px; border-radius:999px; background:#10b981; display:inline-block }
    .stat.disconnected .dot { background:#ef4444 }
  </style>
  </head>
<body>
  <div class="card">
    <h1>🚀 Portal Backend</h1>
    <p>This page is served from the backend node.</p>
    <p>Current time: <b>{{.Now}}</b></p>
    <p>Name: <b>{{.Name}}</b></p>
  </div>
  <footer>demo client</footer>
</body>
</html>`))
