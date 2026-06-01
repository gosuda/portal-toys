package main

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/gosuda/portal-toys/internal/portalapp"
	"github.com/gosuda/portal-tunnel/v2/sdk"
	"github.com/gosuda/portal-tunnel/v2/types"
	"github.com/gosuda/portal-tunnel/v2/utils"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "emulatorjs",
	Short: "Portal demo: EmulatorJS (served over portal HTTP backend)",
	RunE:  runEmulator,
}

var (
	flagServerURLs   string
	flagDiscovery    bool
	flagBanMITM      bool
	flagPort         int
	flagName         string
	flagIdentityPath string
	flagHide         bool
	flagDescription  string
	flagTags         string
	flagOwner        string
)

func init() {
	flags := rootCmd.PersistentFlags()
	flags.StringVar(&flagServerURLs, "server-url", os.Getenv("RELAY"), "relayserver base URL(s); repeat or comma-separated (from env RELAY/RELAY_URL if set)")
	flags.BoolVar(&flagDiscovery, "discovery", portalapp.ResolveBoolEnv(true, "DISCOVERY", "DEFAULT_RELAYS"), "include registry relays and enable relay discovery [env: DISCOVERY, DEFAULT_RELAYS]")
	flags.BoolVar(&flagBanMITM, "ban-mitm", portalapp.ResolveBoolEnv(false, "BAN_MITM"), "ban relay when MITM self-probe detects TLS termination [env: BAN_MITM]")
	flags.IntVar(&flagPort, "port", -1, "optional local HTTP port (negative to disable)")
	flags.StringVar(&flagName, "name", "emulator-js", "backend display name")
	flags.StringVar(&flagIdentityPath, "identity-path", "identity.json", "optional path to load/save the portal identity")
	flags.BoolVar(&flagHide, "hide", false, "hide this lease from portal listings")
	flags.StringVar(&flagDescription, "description", "Portal demo: EmulatorJS (served over portal HTTP backend)", "lease description")
	flags.StringVar(&flagOwner, "owner", "EmulatorJS", "lease owner")
	flags.StringVar(&flagTags, "tags", "game,emulator", "comma-separated lease tags")
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

	exposure, err := portalapp.Expose(ctx, sdk.ExposeConfig{
		RelayURLs:    utils.SplitCSV(flagServerURLs),
		BanMITM:      flagBanMITM,
		Discovery:    flagDiscovery,
		Identity:     types.Identity{Name: flagName},
		IdentityPath: flagIdentityPath,
		Metadata: types.LeaseMetadata{
			Description: flagDescription,
			Tags:        utils.SplitCSV(flagTags),
			Owner:       flagOwner,
			Hide:        flagHide,
		},
	})
	if err != nil {
		return fmt.Errorf("expose: %w", err)
	}
	if exposure != nil {
		defer func() { _ = exposure.Close() }()
	}
	localAddr := ""
	if flagPort >= 0 {
		localAddr = fmt.Sprintf(":%d", flagPort)
	}
	if err := exposure.RunHTTP(ctx, mux, localAddr); err != nil {
		return err
	}
	log.Info().Msg("[emulatorjs] shutdown complete")
	return nil
}
