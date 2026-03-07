package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/gosuda/portal-toys/internal/portalapp"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"github.com/gosuda/portal/v2/types"
)

var rootCmd = &cobra.Command{
	Use:   "mafia",
	Short: "Portal demo: multi-room mafia game",
	RunE:  runServer,
}

var (
	flagServerURLs []string
	flagPort       int
	flagName       string
	flagCredKey    string
	flagAuthKey    string
)

func init() {
	flags := rootCmd.PersistentFlags()
	flags.StringSliceVar(&flagServerURLs, "server-url", strings.Split(os.Getenv("RELAY"), ","), "relayserver base URL(s); repeat or comma-separated (from env RELAY/RELAY_URL if set)")
	flags.IntVar(&flagPort, "port", -1, "optional local HTTP port (negative to disable)")
	flags.StringVar(&flagName, "name", "mafia", "backend display name")
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
	lease, err := portalapp.ListenAll(ctx, portalapp.LeaseConfig{
		ServerURLs: flagServerURLs,
		Name:       flagName,
		Metadata: types.LeaseMetadata{
			Description: "Portal demo: multi-room mafia game",
			Owner:       "Mafia",
			Tags:        []string{"game", "mafia"},
		},
	})
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	if lease != nil {
		defer func() { _ = lease.Close() }()
	} else {
		log.Info().Msg("[mafia] relay disabled; running local mode only")
	}
	err = portalapp.RunHTTP(ctx, lease, mux, portalapp.LocalAddrFromPort(flagPort))
	mgr.Close()
	if err != nil {
		return err
	}
	log.Info().Msg("[mafia] shutdown complete")
	return nil
}
