package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gosuda/portal/v2/utils"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "portal-list",
	Short: "Portal listing & health check",
	RunE:  run,
}

var (
	flagServerURLs  string
	flagDiscovery   bool
	flagBanMITM     bool
	flagPortalBase  string
	flagPort        int
	flagName        string
	flagHide        bool
	flagDescription string
	flagOwner       string
	flagTags        string
	flagThumbnail   string
)

func init() {
	flags := rootCmd.PersistentFlags()
	relay := firstNonEmpty(os.Getenv("RELAY"), os.Getenv("RELAY_URL"), os.Getenv("SERVER_URL"))
	flags.StringVar(&flagServerURLs, "server-url", relay, "relay base URL(s); repeat or comma-separated")
	flags.BoolVar(&flagDiscovery, "discovery", utils.ResolveBoolEnv(true, "DISCOVERY", "DEFAULT_RELAYS"), "include registry relays and enable relay discovery [env: DISCOVERY, DEFAULT_RELAYS]")
	flags.BoolVar(&flagBanMITM, "ban-mitm", utils.ResolveBoolEnv(false, "BAN_MITM"), "ban relay when MITM self-probe detects TLS termination [env: BAN_MITM]")
	flags.StringVar(&flagPortalBase, "portal-base", derivePortalBase(relay), "portal site base URL (optional, used only for SSR listing)")
	flags.IntVar(&flagPort, "port", 8099, "local HTTP port (negative to disable)")
	flags.StringVar(&flagName, "name", "portal-list", "backend display name")
	flags.BoolVar(&flagHide, "hide", false, "hide this lease from portal listings")
	flags.StringVar(&flagDescription, "description", "Portal list viewer (online status)", "lease description")
	flags.StringVar(&flagOwner, "owner", "Portal", "lease owner")
	flags.StringVar(&flagTags, "tags", "portal,viewer", "comma-separated lease tags")
	flags.StringVar(&flagThumbnail, "thumbnail", "https://w0.peakpx.com/wallpaper/870/326/HD-wallpaper-portal-fun-cool-portal-entertainment-video-game-funny-thumbnail.jpg", "thumbnail URL for this lease")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("execute portal-list command")
	}
}

func run(cmd *cobra.Command, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	mux := NewHandler()

	relayURLs, err := utils.ResolvePortalRelayURLs(ctx, utils.SplitCSV(flagServerURLs), flagDiscovery)
	if err != nil {
		return fmt.Errorf("resolve relay urls: %w", err)
	}
	gSites.Init(deriveBootstrapSites(relayURLs))
	gPortalMgr.Init(ctx, mux)
	go monitorRelayRegistration(ctx)

	// Optional local HTTP
	var httpSrv *http.Server
	if flagPort >= 0 {
		httpSrv = &http.Server{Addr: fmt.Sprintf(":%d", flagPort), Handler: mux, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
		log.Info().Msgf("[portal-list] serving locally at http://127.0.0.1:%d", flagPort)
		go func() {
			if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Warn().Err(err).Msg("[portal-list] local http stopped")
			}
		}()
	}

	// Shutdown watcher
	go func() {
		<-ctx.Done()
		gPortalMgr.Shutdown()
		if httpSrv != nil {
			sctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := httpSrv.Shutdown(sctx); err != nil && err != context.Canceled {
				log.Warn().Err(err).Msg("[portal-list] local http shutdown error")
			}
		}
	}()

	<-ctx.Done()
	log.Info().Msg("[portal-list] shutdown complete")
	return nil
}
