package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"

	sdk "github.com/gosuda/relaydns/sdk/go"
)

func main() {
	var (
		serverURL string
		port      int
		name      string
		dir       string
	)

	flag.StringVar(&serverURL, "server-url", "http://relaydns.gosuda.org", "RelayDNS admin base URL to fetch multiaddrs from /health")
	flag.IntVar(&port, "port", 8081, "Local HTTP port to serve the static site")
	flag.StringVar(&name, "name", "gosuda-blog", "Display name shown on RelayDNS server UI")
	flag.StringVar(&dir, "dir", "dist", "Directory to serve (built static files)")
	flag.Parse()

	if _, err := os.Stat(dir); err != nil {
		log.Fatal().Err(err).Str("dir", dir).Msg("serve directory not found")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1) Start local HTTP backend serving the static directory
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatal().Err(err).Msg("failed to listen")
	}
	mux := http.NewServeMux()
	// Minimal health endpoint
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	// Serve static files (with SPA friendly behavior)
	mux.Handle("/", fileServerWithSPA(dir))

	httpSrv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		log.Info().Msgf("[relaydns-proxy] serving %s on http://127.0.0.1:%d", dir, port)
		if err := httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Error().Err(err).Msg("http server error")
		}
	}()

	// 2) Start RelayDNS client advertising the local backend
	client, err := sdk.NewClient(ctx, sdk.ClientConfig{
		Name:      name,
		TargetTCP: fmt.Sprintf("127.0.0.1:%d", port),
		ServerURL: serverURL,
	})
	if err != nil {
		log.Fatal().Err(err).Msg("new relaydns client")
	}
	if err := client.Start(ctx); err != nil {
		log.Fatal().Err(err).Msg("start relaydns client")
	}

	// 3) Graceful shutdown
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Info().Msg("[relaydns-proxy] shutting down...")

	if err := client.Close(); err != nil {
		log.Warn().Err(err).Msg("client close error")
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("http server shutdown error")
	}
	log.Info().Msg("[relaydns-proxy] shutdown complete")
}
