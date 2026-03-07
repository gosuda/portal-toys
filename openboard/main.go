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
	Use:   "openboard",
	Short: "Portal demo: OpenBoard (user-hosted HTML BBS)",
	RunE:  runOpenboard,
}

var (
	flagServerURLs  []string
	flagPort        int
	flagName        string
	flagDataPath    string
	flagHide        bool
	flagDescription string
	flagTags        string
	flagOwner       string
)

func init() {
	flags := rootCmd.PersistentFlags()
	flags.StringSliceVar(&flagServerURLs, "server-url", strings.Split(os.Getenv("RELAY"), ","), "relay websocket URL(s); repeat or comma-separated (from env RELAY/RELAY_URL if set)")
	flags.IntVar(&flagPort, "port", -1, "optional local HTTP port (negative to disable)")
	flags.StringVar(&flagName, "name", "openboard", "backend display name")
	flags.StringVar(&flagDataPath, "datapath", "./openboard/data", "directory to persist user pages")
	flags.BoolVar(&flagHide, "hide", false, "hide this lease from portal listings")
	flags.StringVar(&flagDescription, "description", "Portal demo: OpenBoard (user-hosted HTML BBS)", "lease description")
	flags.StringVar(&flagOwner, "owner", "OpenBoard", "lease owner")
	flags.StringVar(&flagTags, "tags", "bbs,openboard", "comma-separated lease tags")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("execute openboard command")
	}
}

func runOpenboard(cmd *cobra.Command, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	handler := NewHandler(flagName, flagDataPath)

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
	if err := portalapp.RunHTTP(ctx, lease, handler, portalapp.LocalAddrFromPort(flagPort)); err != nil {
		return err
	}
	log.Info().Msg("[openboard] shutdown complete")
	return nil
}
