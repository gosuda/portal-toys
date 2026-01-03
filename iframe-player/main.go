package main

import (
	"context"
	"fmt"
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
	Use:   "iframe-player",
	Short: "Portal demo: iframe playlist viewer (relay HTTP backend)",
	RunE:  runIframePlayer,
}

var (
	flagServerURLs  []string
	flagPort        int
	flagName        string
	flagHide        bool
	flagDescription string
	flagTags        string
	flagOwner       string
)

func init() {
	flags := rootCmd.PersistentFlags()
	flags.StringSliceVar(&flagServerURLs, "server-url", strings.Split(os.Getenv("RELAY"), ","), "relay websocket URL(s); repeat or comma-separated (from env RELAY/RELAY_URL if set)")
	flags.IntVar(&flagPort, "port", -1, "optional local HTTP port (negative to disable)")
	flags.StringVar(&flagName, "name", "iframe-player", "backend display name")
	flags.BoolVar(&flagHide, "hide", false, "hide this lease from portal listings")
	flags.StringVar(&flagDescription, "description", "Portal demo: iframe playlist viewer (relay HTTP backend)", "lease description")
	flags.StringVar(&flagOwner, "owner", "Iframe Player", "lease owner")
	flags.StringVar(&flagTags, "tags", "viewer,iframe,player", "comma-separated lease tags")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("execute iframe-player command")
	}
}

func runIframePlayer(cmd *cobra.Command, args []string) error {
	// Cancellation context
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	baseHandler := NewHandler(flagName)

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

	// Relay client/listener (single client using all bootstrap servers)
	cred := sdk.NewCredential()
	client, err := sdk.NewClient(func(c *sdk.RDClientConfig) { c.BootstrapServers = flagServerURLs })
	if err != nil {
		return fmt.Errorf("new client: %w", err)
	}
	ln, err := client.Listen(cred, flagName, []string{"http/1.1"},
		sdk.WithDescription(flagDescription),
		sdk.WithHide(flagHide),
		sdk.WithOwner(flagOwner),
		sdk.WithTags(strings.Split(flagTags, ",")),
	)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	// Serve over relay
	go func() {
		if err := http.Serve(ln, handler); err != nil && err != http.ErrServerClosed && ctx.Err() == nil {
			log.Error().Err(err).Msg("[iframe] relay http error")
		}
	}()

	// Optional local HTTP
	var httpSrv *http.Server
	if flagPort >= 0 {
		httpSrv = &http.Server{Addr: fmt.Sprintf(":%d", flagPort), Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
		log.Info().Msgf("[iframe] serving locally at http://127.0.0.1:%d", flagPort)
		go func() {
			if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Warn().Err(err).Msg("[iframe] local http stopped")
			}
		}()
	}

	// Unified shutdown watcher
	go func() {
		<-ctx.Done()
		_ = ln.Close()
		_ = client.Close()
		if httpSrv != nil {
			sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := httpSrv.Shutdown(sctx); err != nil && err != context.Canceled {
				log.Error().Err(err).Msg("[iframe] http server shutdown error")
			}
		}
	}()

	<-ctx.Done()
	log.Info().Msg("[iframe] shutdown complete")
	return nil
}
