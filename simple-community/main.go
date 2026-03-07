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
	Use:   "simple-community",
	Short: "Portal demo: simple community board",
	RunE:  runCommunity,
}

var (
	flagServerURLs  []string
	flagPort        int
	flagName        string
	flagDBPath      string
	flagHide        bool
	flagDescription string
	flagTags        string
	flagOwner       string
)

func init() {
	flags := rootCmd.PersistentFlags()
	flags.StringSliceVar(&flagServerURLs, "server-url", strings.Split(os.Getenv("RELAY"), ","), "relay websocket URL(s); repeat or comma-separated (from env RELAY/RELAY_URL if set)")
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

	lease, err := portalapp.ListenAll(ctx, portalapp.LeaseConfig{
		ServerURLs: flagServerURLs,
		Name:       flagName,
		Metadata: types.LeaseMetadata{
			Description: flagDescription,
			Tags:        portalapp.SplitCSV(flagTags),
			Owner:       flagOwner,
			Hide:        flagHide,
		},
	})
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	if lease != nil {
		defer func() { _ = lease.Close() }()
	}
	if err := portalapp.RunHTTP(ctx, lease, router, portalapp.LocalAddrFromPort(flagPort)); err != nil {
		return err
	}
	log.Info().Msg("[community] shutdown complete")
	return nil
}
