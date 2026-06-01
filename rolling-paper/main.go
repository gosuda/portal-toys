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
	Use:   "rolling-paper",
	Short: "Portal demo: Rolling Paper (relay HTTP backend)",
	RunE:  runRollingPaper,
}

var (
	flagServerURLs    string
	flagDiscovery     bool
	flagBanMITM       bool
	flagPort          int
	flagName          string
	flagIdentityPath  string
	flagVoteThreshold int
	flagMaxLen        int
	flagHide          bool
	flagDescription   string
	flagTags          string
	flagOwner         string
)

func init() {
	flags := rootCmd.PersistentFlags()
	flags.StringVar(&flagServerURLs, "server-url", os.Getenv("RELAY"), "relay base URL(s); repeat or comma-separated (from env RELAY/RELAY_URL if set)")
	flags.BoolVar(&flagDiscovery, "discovery", portalapp.ResolveBoolEnv(false, "DISCOVERY", "DEFAULT_RELAYS"), "include registry relays and enable relay discovery [env: DISCOVERY, DEFAULT_RELAYS]")
	flags.BoolVar(&flagBanMITM, "ban-mitm", portalapp.ResolveBoolEnv(false, "BAN_MITM"), "ban relay when MITM self-probe detects TLS termination [env: BAN_MITM]")
	flags.IntVar(&flagPort, "port", 3000, "optional local HTTP port (negative to disable)")
	flags.StringVar(&flagName, "name", "rolling-paper", "backend display name")
	flags.StringVar(&flagIdentityPath, "identity-path", "identity.json", "optional path to load/save the portal identity")
	flags.IntVar(&flagVoteThreshold, "delete-threshold", 3, "votes required to delete (>=1)")
	flags.IntVar(&flagMaxLen, "max-len", 2500, "maximum message length in characters (>=1)")
	flags.BoolVar(&flagHide, "hide", false, "hide this lease from portal listings")
	flags.StringVar(&flagDescription, "description", "Portal demo: Rolling Paper (relay HTTP backend)", "lease description")
	flags.StringVar(&flagOwner, "owner", "Rolling Paper", "lease owner")
	flags.StringVar(&flagTags, "tags", "collab,rolling-paper", "comma-separated lease tags")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("execute rolling-paper command")
	}
}

func runRollingPaper(cmd *cobra.Command, args []string) error {
	// Cancellation context
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Init DB (Pebble)
	initDB()
	defer db.Close()

	// HTTP mux
	mux := http.NewServeMux()
	mux.HandleFunc("/", rootHandler)

	// Prepare embedded static files
	sub, err := fs.Sub(embeddedPublic, "static")
	if err != nil {
		return fmt.Errorf("embed sub FS: %w", err)
	}
	staticHandler = http.FileServer(http.FS(sub))

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
	log.Info().Msg("[rolling-paper] shutdown complete")
	return nil
}
