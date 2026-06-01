package main

import (
	"context"
	"fmt"
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
	Use:   "mafia",
	Short: "Portal demo: multi-room mafia game",
	RunE:  runServer,
}

var (
	flagServerURLs   string
	flagDiscovery    bool
	flagBanMITM      bool
	flagPort         int
	flagName         string
	flagIdentityPath string
	flagCredKey      string
	flagAuthKey      string
)

func init() {
	flags := rootCmd.PersistentFlags()
	flags.StringVar(&flagServerURLs, "server-url", os.Getenv("RELAY"), "relayserver base URL(s); repeat or comma-separated (from env RELAY/RELAY_URL if set)")
	flags.BoolVar(&flagDiscovery, "discovery", portalapp.ResolveBoolEnv(true, "DISCOVERY", "DEFAULT_RELAYS"), "include registry relays and enable relay discovery [env: DISCOVERY, DEFAULT_RELAYS]")
	flags.BoolVar(&flagBanMITM, "ban-mitm", portalapp.ResolveBoolEnv(false, "BAN_MITM"), "ban relay when MITM self-probe detects TLS termination [env: BAN_MITM]")
	flags.IntVar(&flagPort, "port", -1, "optional local HTTP port (negative to disable)")
	flags.StringVar(&flagName, "name", "mafia", "backend display name")
	flags.StringVar(&flagIdentityPath, "identity-path", "identity.json", "optional path to load/save the portal identity")
	flags.StringVar(&flagCredKey, "cred-key", "", "optional credential key to use for the listener (base64 encoded)")
	flags.StringVar(&flagAuthKey, "ws-auth-key", os.Getenv("MAFIA_WS_AUTH"), "optional shared secret required from clients via X-Mafia-Key header")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("execute mafia command")
	}
}

func runServer(cmd *cobra.Command, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	mgr := NewRoomManager()
	handler := NewHTTPServer(mgr, flagAuthKey)
	mux := handler.Router()
	if flagCredKey != "" {
		log.Warn().Msg("[mafia] --cred-key is no longer supported with the current portal SDK and will be ignored")
	}
	exposure, err := portalapp.Expose(ctx, sdk.ExposeConfig{
		RelayURLs:    utils.SplitCSV(flagServerURLs),
		BanMITM:      flagBanMITM,
		Discovery:    flagDiscovery,
		Identity:     types.Identity{Name: flagName},
		IdentityPath: flagIdentityPath,
		Metadata: types.LeaseMetadata{
			Description: "Portal demo: multi-room mafia game",
			Owner:       "Mafia",
			Tags:        []string{"game", "mafia"},
		},
	})
	if err != nil {
		return fmt.Errorf("expose: %w", err)
	}
	if exposure != nil {
		defer func() { _ = exposure.Close() }()
	} else {
		log.Info().Msg("[mafia] relay disabled; running local mode only")
	}
	localAddr := ""
	if flagPort >= 0 {
		localAddr = fmt.Sprintf(":%d", flagPort)
	}
	err = exposure.RunHTTP(ctx, mux, localAddr)
	mgr.Close()
	if err != nil {
		return err
	}
	log.Info().Msg("[mafia] shutdown complete")
	return nil
}
