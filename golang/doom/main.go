package main

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"path"
	"syscall"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"gosuda.org/portal/sdk"
)

//go:embed doom/public/*
var doomAssets embed.FS

var rootCmd = &cobra.Command{
	Use:   "doom",
	Short: "Portal demo: Doom (served over portal HTTP backend)",
	RunE:  runDoom,
}

var (
	flagServerURL string
	flagPort      int
	flagName      string
)

func init() {
	flags := rootCmd.PersistentFlags()
	flags.StringVar(&flagServerURL, "server-url", "wss://portal.gosuda.org/relay", "relayserver base URL")
	flags.IntVar(&flagPort, "port", -1, "optional local HTTP port (negative to disable)")
	flags.StringVar(&flagName, "name", "doom", "backend display name")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("execute doom command")
	}
}

func runDoom(cmd *cobra.Command, args []string) error {
	// 1) Create SDK client and connect to relay(s)
	client, err := sdk.NewClient(func(c *sdk.RDClientConfig) {
		c.BootstrapServers = []string{flagServerURL}
	})
	if err != nil {
		return fmt.Errorf("new client: %w", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			log.Warn().Err(err).Msg("[doom] client close error")
		}
	}()

	// 2) Register lease and obtain relay listener
	cred := sdk.NewCredential()
	listener, err := client.Listen(cred, flagName, []string{"http/1.1"})
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer listener.Close()

	// 4) Build HTTP handler to serve embedded Doom assets
	staticFS, err := fs.Sub(doomAssets, "doom/public")
	if err != nil {
		return fmt.Errorf("static assets: %w", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.Handle("/", withStaticHeaders(http.FileServer(http.FS(staticFS))))

	// 5) Serve HTTP directly over the relay listener
	log.Info().Msgf("[doom] serving HTTP over relay; lease=%s id=%s", flagName, cred.ID())
	srvErr := make(chan error, 1)
	go func() { srvErr <- http.Serve(listener, mux) }()

	// 6) Wait for termination or HTTP error
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-sig:
		log.Info().Msg("[doom] shutting down...")
	case err := <-srvErr:
		if err != nil {
			log.Error().Err(err).Msg("[doom] http serve error")
		}
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
