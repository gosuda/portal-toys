package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	sdk "github.com/gosuda/relaydns/sdk/go"
)

//go:embed doom/public/*
var doomAssets embed.FS

var rootCmd = &cobra.Command{
	Use:   "relaydns-doom",
	Short: "RelayDNS demo: Doom in Docker (static build)",
	RunE:  runDoom,
}

var (
	flagServerURL string
	flagPort      int
	flagName      string
)

func init() {
	flags := rootCmd.PersistentFlags()
	flags.StringVar(&flagServerURL, "server-url", "http://relaydns.gosuda.org", "relayserver base URL to auto-fetch multiaddrs from /health")
	flags.IntVar(&flagPort, "port", 8096, "local Doom HTTP port")
	flags.StringVar(&flagName, "name", "doom", "backend display name")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("execute doom command")
	}
}

func runDoom(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", flagPort))
	if err != nil {
		return fmt.Errorf("listen doom: %w", err)
	}

	staticFS, err := fs.Sub(doomAssets, "doom/public")
	if err != nil {
		return fmt.Errorf("static assets: %w", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/healthz", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	fileServer := http.FileServer(http.FS(staticFS))
	mux.Handle("/", withStaticHeaders(fileServer))

	httpSrv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		if err := httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Warn().Err(err).Msg("[doom] http serve error")
		}
	}()

	client, err := sdk.NewClient(ctx, sdk.ClientConfig{
		Name:      flagName,
		TargetTCP: fmt.Sprintf("127.0.0.1:%d", flagPort),
		ServerURL: flagServerURL,
	})
	if err != nil {
		return fmt.Errorf("new relaydns client: %w", err)
	}
	if err := client.Start(ctx); err != nil {
		return fmt.Errorf("start relaydns client: %w", err)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Info().Msg("[doom] shutting down...")

	if err := client.Close(); err != nil {
		log.Warn().Err(err).Msg("[doom] relay client close error")
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil && err != context.Canceled {
		log.Error().Err(err).Msg("[doom] http server shutdown error")
	}

	log.Info().Msg("[doom] shutdown complete")
	return nil
}

func withStaticHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Doom assets contain wasm + JS, so disable sniffing and cache non-HTML responses.
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if path.Ext(r.URL.Path) == ".html" || r.URL.Path == "/" {
			w.Header().Set("Cache-Control", "no-cache")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=86400")
		}
		next.ServeHTTP(w, r)
	})
}
