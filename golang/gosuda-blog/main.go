package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"gosuda.org/portal/sdk"
)

var rootCmd = &cobra.Command{
	Use:   "gosuda-blog",
	Short: "Portal demo: gosuda blog static site (relay HTTP backend)",
	RunE:  runBlog,
}

var (
	flagServerURLs []string
	flagPort       int
	flagName       string
	flagDir        string
)

func init() {
	flags := rootCmd.PersistentFlags()
	flags.StringSliceVar(&flagServerURLs, "server-url", strings.Split(os.Getenv("RELAY"), ","), "relay websocket URL(s); repeat or comma-separated (from env RELAY/RELAY_URL if set)")
	flags.IntVar(&flagPort, "port", -1, "optional local HTTP port (negative to disable)")
	flags.StringVar(&flagName, "name", "gosuda-blog", "Display name shown on server UI")
	flags.StringVar(&flagDir, "dir", "./gosuda-blog/dist", "Directory to serve (built static files)")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("execute gosuda-blog command")
	}
}

func runBlog(cmd *cobra.Command, args []string) error {
	if _, err := os.Stat(flagDir); err != nil {
		return fmt.Errorf("serve directory not found: %w", err)
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
	mux.Handle("/", fileServerWithSPA(flagDir))

	// 2) Start client(s) and serve over relay listener(s)
	cred := sdk.NewCredential()
	var clients []*sdk.RDClient
	var listeners []net.Listener
	for _, raw := range flagServerURLs {
		if raw == "" {
			continue
		}
		for _, p := range strings.Split(raw, ",") {
			u := strings.TrimSpace(p)
			if u == "" {
				continue
			}
			client, err := sdk.NewClient(func(c *sdk.RDClientConfig) { c.BootstrapServers = []string{u} })
			if err != nil {
				log.Error().Err(err).Str("url", u).Msg("new client failed")
				continue
			}
			clients = append(clients, client)
			ln, err := client.Listen(cred, flagName, []string{"http/1.1"})
			if err != nil {
				return fmt.Errorf("listen (%s): %w", u, err)
			}
			listeners = append(listeners, ln)
		}
	}
	if len(listeners) == 0 {
		return fmt.Errorf("no valid relay servers provided via --server-url or RELAY/RELAY_URL env")
	}
	for i, ln := range listeners {
		idx := i
		go func() {
			if err := http.Serve(ln, mux); err != nil && err != http.ErrServerClosed && ctx.Err() == nil {
				log.Error().Err(err).Int("listener", idx).Msg("[blog] relay http serve error")
			}
		}()
	}

	// 3) Optional local HTTP server on --port
	var httpSrv *http.Server
	if flagPort >= 0 {
		httpSrv = &http.Server{Addr: fmt.Sprintf(":%d", flagPort), Handler: mux, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
		log.Info().Msgf("[blog] serving %s locally at http://127.0.0.1:%d", flagDir, flagPort)
		go func() {
			if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Warn().Err(err).Msg("local http stopped")
			}
		}()
	}

	// Unified shutdown watcher
	go func() {
		<-ctx.Done()
		for _, ln := range listeners {
			_ = ln.Close()
		}
		for _, c := range clients {
			_ = c.Close()
		}
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
	return nil
}
