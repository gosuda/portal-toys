package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/gosuda/portal/v2/sdk"
	"github.com/gosuda/portal/v2/types"
	"github.com/gosuda/portal/v2/utils"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "simple-community",
	Short: "Portal demo: simple community board",
	RunE:  runCommunity,
}

var (
	flagServerURLs    string
	flagDefaultRelays bool
	flagPort          int
	flagName          string
	flagDBPath        string
	flagHide          bool
	flagDescription   string
	flagTags          string
	flagOwner         string
)

func init() {
	flags := rootCmd.PersistentFlags()
	flags.StringVar(&flagServerURLs, "server-url", os.Getenv("RELAY"), "relay base URL(s); repeat or comma-separated (from env RELAY/RELAY_URL if set)")
	flags.BoolVar(&flagDefaultRelays, "default-relays", utils.ResolveBoolEnv(true, "DEFAULT_RELAYS"), "include repository registry.json default relays [env: DEFAULT_RELAYS]")
	flags.IntVar(&flagPort, "port", -1, "optional local HTTP port (negative to disable)")
	flags.StringVar(&flagName, "name", "simple-community", "backend display name")
	flags.StringVar(&flagDBPath, "db-path", "simple-community/data", "optional directory for Pebble db")
	flags.BoolVar(&flagHide, "hide", false, "hide this lease from portal listings")
	flags.StringVar(&flagDescription, "description", "Portal demo: simple community board", "lease description")
	flags.StringVar(&flagOwner, "owner", "Community", "lease owner")
	flags.StringVar(&flagTags, "tags", "community", "comma-separated lease tags")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("execute simple community command")
	}
}

func runCommunity(cmd *cobra.Command, args []string) error {
	bootCtx := context.Background()
	if err := InitStore(flagDBPath); err != nil {
		return err
	}
	if err := LoadSnapshot(bootCtx); err != nil {
		log.Warn().Err(err).Msg("[community] failed to bootstrap from local snapshot")
	}

	router := NewHandler()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	exposure, err := sdk.Expose(ctx, sdk.ExposeConfig{
		RelayURLs:           utils.SplitCSV(flagServerURLs),
		DefaultRelayEnabled: flagDefaultRelays,
		Name:                flagName,
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
	if err := exposure.RunHTTP(ctx, router, localAddr); err != nil {
		return err
	}
	log.Info().Msg("[community] shutdown complete")
	return nil
}
