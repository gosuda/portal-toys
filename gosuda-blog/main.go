package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"

	sdk "github.com/gosuda/relaydns/sdk"
)

func main() {
	var (
		serverURL string
		port      int
		name      string
		dir       string
	)

	flag.StringVar(&serverURL, "server-url", "ws://localhost:4017/relay", "RelayDNS relay websocket URL")
	flag.IntVar(&port, "port", 8081, "Optional local HTTP port to serve the static site (0 to disable)")
	flag.StringVar(&name, "name", "gosuda-blog", "Display name shown on RelayDNS server UI")
	flag.StringVar(&dir, "dir", "./gosuda-blog/dist", "Directory to serve (built static files)")
	flag.Parse()

	if _, err := os.Stat(dir); err != nil {
		log.Fatal().Err(err).Str("dir", dir).Msg("serve directory not found")
	}

	// Context for graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 1) Build HTTP mux serving the static directory
	mux := http.NewServeMux()
	// Minimal health endpoint
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	// Quiet favicon 404s
	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	// Serve static files (with SPA friendly behavior)
	mux.Handle("/", fileServerWithSPA(dir))

	// 2) Start RelayDNS client and serve over a relay listener
	client, err := sdk.NewClient(func(c *sdk.RDClientConfig) {
		c.BootstrapServers = []string{serverURL}
	})
	if err != nil {
		log.Fatal().Err(err).Msg("new relaydns client")
	}
	defer client.Close()

	cred := sdk.NewCredential()
	listener, err := client.Listen(cred, name, []string{"htt/1.1"})
	if err != nil {
		log.Fatal().Err(err).Msg("listen over relay")
	}
	go func() {
		if err := http.Serve(listener, mux); err != nil && err != http.ErrServerClosed && ctx.Err() == nil {
			log.Error().Err(err).Msg("relay http serve error")
		}
	}()

	// 3) Optional local HTTP server on --port
	var httpSrv *http.Server
	if port > 0 {
		httpSrv = &http.Server{Addr: fmt.Sprintf(":%d", port), Handler: mux, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
		log.Info().Msgf("[blog] serving %s locally at http://127.0.0.1:%d", dir, port)
		go func() {
			if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Warn().Err(err).Msg("local http stopped")
			}
		}()
	}

	// Unified shutdown watcher
	go func() {
		<-ctx.Done()
		_ = listener.Close()
		if httpSrv != nil {
			sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := httpSrv.Shutdown(sctx); err != nil && err != context.Canceled {
				log.Error().Err(err).Msg("http server shutdown error")
			}
		}
	}()

	<-ctx.Done()
	log.Info().Msg("[blog] shutdown complete")
}
