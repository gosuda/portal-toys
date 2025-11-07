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
	Use:   "youtube-chat",
	Short: "Portal demo: collaborative youtube chat (relay HTTP backend)",
	RunE:  runYouTubeChat,
}

var (
    flagServerURLs []string
    flagPort       int
    flagName       string
)

func init() {
    flags := rootCmd.PersistentFlags()
    flags.StringSliceVar(&flagServerURLs, "server-url", strings.Split(os.Getenv("RELAY"), ","), "relay websocket URL(s); repeat or comma-separated (from env RELAY/RELAY_URL if set)")
    flags.IntVar(&flagPort, "port", -1, "optional local HTTP port (negative to disable)")
    flags.StringVar(&flagName, "name", "youtube-chat", "backend display name")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("execute youtube-chat command")
	}
}

func runYouTubeChat(cmd *cobra.Command, args []string) error {
	// Cancellation context
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dh := newDrawHub()
	baseHandler := NewHandler(flagName, dh)

	// Wrap to handle relay /peer/{id}/ prefix for routes
	stripPeer := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			const prefix = "/peer/"
			if !strings.HasPrefix(r.URL.Path, prefix) {
				next.ServeHTTP(w, r)
				return
			}
			rest := strings.TrimPrefix(r.URL.Path, prefix)
			if i := strings.IndexByte(rest, '/'); i >= 0 {
				r2 := r.Clone(r.Context())
				r2.URL.Path = rest[i:]
				next.ServeHTTP(w, r2)
				return
			}
			// No suffix after token -> redirect to add trailing slash for relative URLs
			http.Redirect(w, r, r.URL.Path+"/", http.StatusMovedPermanently)
		})
	}
	handler := stripPeer(baseHandler)

    // Relay clients/listeners (multi-relay)
    cred := sdk.NewCredential()
    var clients []*sdk.RDClient
    var listeners []net.Listener
    for _, raw := range flagServerURLs {
        if raw == "" { continue }
        for _, p := range strings.Split(raw, ",") {
            u := strings.TrimSpace(p)
            if u == "" { continue }
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

    // Serve over relay
    for i, ln := range listeners {
        idx := i
        go func() {
            if err := http.Serve(ln, handler); err != nil && err != http.ErrServerClosed && ctx.Err() == nil {
                log.Error().Err(err).Int("listener", idx).Msg("[ytchat] relay http error")
            }
        }()
    }

	// Optional local HTTP
	var httpSrv *http.Server
	if flagPort >= 0 {
		httpSrv = &http.Server{Addr: fmt.Sprintf(":%d", flagPort), Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
		log.Info().Msgf("[ytchat] serving locally at http://127.0.0.1:%d", flagPort)
		go func() {
			if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Warn().Err(err).Msg("[ytchat] local http stopped")
			}
		}()
	}

	// Unified shutdown watcher
	go func() {
        <-ctx.Done()
        for _, ln := range listeners { _ = ln.Close() }
        for _, c := range clients { _ = c.Close() }
        if httpSrv != nil {
            sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
            defer cancel()
            if err := httpSrv.Shutdown(sctx); err != nil && err != context.Canceled {
                log.Error().Err(err).Msg("[ytchat] http server shutdown error")
            }
        }
    }()

	<-ctx.Done()
	log.Info().Msg("[ytchat] shutdown complete")
	return nil
}
