package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"path"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"gosuda.org/portal/sdk"
)

//go:embed static/index.html static/data static/docs
var emulatorAssets embed.FS

var rootCmd = &cobra.Command{
	Use:   "emulatorjs",
	Short: "Portal demo: EmulatorJS (served over portal HTTP backend)",
	RunE:  runEmulator,
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
	flags.StringVar(&flagName, "name", "emulator-js", "backend display name")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("execute emulatorjs command")
	}
}

func runEmulator(cmd *cobra.Command, args []string) error {
	// 1) Cancellation context for graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 2) Create SDK client and connect to relay(s)
	client, err := sdk.NewClient(func(c *sdk.RDClientConfig) {
		c.BootstrapServers = []string{flagServerURL}
	})
	if err != nil {
		return fmt.Errorf("new client: %w", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			log.Warn().Err(err).Msg("[emulatorjs] client close error")
		}
	}()

	// 3) Register lease and obtain relay listener
	cred := sdk.NewCredential()
	listener, err := client.Listen(cred, flagName, []string{"http/1.1"})
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer listener.Close()

	// 4) Build HTTP handler to serve embedded EmulatorJS assets
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	// Serve static/ as the site root
	staticFS, err := fs.Sub(emulatorAssets, "static")
	if err != nil {
		return fmt.Errorf("sub fs static: %w", err)
	}
	mux.Handle("/", withStaticHeaders(http.FileServer(http.FS(staticFS))))

	// Also expose top-level data and docs
	mux.Handle("/data/", withStaticHeaders(http.FileServer(http.FS(emulatorAssets))))
	mux.Handle("/docs/", withStaticHeaders(http.FileServer(http.FS(emulatorAssets))))

	// 5) Serve HTTP directly over the relay listener
	log.Info().Msgf("[emulatorjs] serving HTTP over relay; lease=%s id=%s", flagName, cred.ID())
	go func() {
		if err := http.Serve(listener, mux); err != nil && err != http.ErrServerClosed && ctx.Err() == nil {
			log.Error().Err(err).Msg("[emulatorjs] relay http serve error")
		}
	}()

	// 6) Optional: also serve locally on --port
	var httpSrv *http.Server
	if flagPort >= 0 {
		httpSrv = &http.Server{
			Addr:              fmt.Sprintf(":%d", flagPort),
			Handler:           mux,
			ReadHeaderTimeout: 5 * time.Second,
			IdleTimeout:       60 * time.Second,
		}
		log.Info().Msgf("[emulatorjs] serving locally at http://127.0.0.1:%d", flagPort)
		go func() {
			if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Warn().Err(err).Msg("[emulatorjs] local http stopped")
			}
		}()
	}

	// 7) Unified shutdown watcher
	go func() {
		<-ctx.Done()
		_ = listener.Close()
		if httpSrv != nil {
			sctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := httpSrv.Shutdown(sctx); err != nil && err != context.Canceled {
				log.Warn().Err(err).Msg("[emulatorjs] local http shutdown error")
			}
		}
	}()

	// 8) Wait for termination signal
	<-ctx.Done()
	log.Info().Msg("[emulatorjs] shutdown complete")
	return nil
}

func withStaticHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set appropriate headers for static assets
		w.Header().Set("X-Content-Type-Options", "nosniff")

		ext := path.Ext(r.URL.Path)
		if ext == ".html" || r.URL.Path == "/" {
			w.Header().Set("Cache-Control", "no-cache")
		} else {
			// Cache JS, CSS, WASM, images for 1 day
			w.Header().Set("Cache-Control", "public, max-age=86400")
		}

		next.ServeHTTP(w, r)
	})
}
