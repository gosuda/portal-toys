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
	Use:   "chatter-bbs",
	Short: "Portal demo: Chatter BBS (relay HTTP backend)",
	RunE:  runChatter,
}

var (
	flagServerURLs    string
	flagDefaultRelays bool
	flagPort          int
	flagName          string
	flagHide          bool
	flagDescription   string
	flagTags          string
	flagOwner         string
)

func init() {
	flags := rootCmd.PersistentFlags()
	flags.StringVar(&flagServerURLs, "server-url", os.Getenv("RELAY"), "relay base URL(s); repeat or comma-separated (from env RELAY/RELAY_URL if set)")
	flags.BoolVar(&flagDefaultRelays, "default-relays", utils.ParseBoolEnv("DEFAULT_RELAYS", true), "include repository registry.json default relays [env: DEFAULT_RELAYS]")
	flags.IntVar(&flagPort, "port", -1, "optional local HTTP port (negative to disable)")
	flags.StringVar(&flagName, "name", "chatter-bbs", "backend display name")
	flags.BoolVar(&flagHide, "hide", false, "hide this lease from portal listings")
	flags.StringVar(&flagDescription, "description", "Portal demo: Chatter BBS", "lease description")
	flags.StringVar(&flagOwner, "owner", "Chatter BBS", "lease owner")
	flags.StringVar(&flagTags, "tags", "chat,bbs", "comma-separated lease tags")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("execute chatter-bbs command")
	}
}

func runChatter(cmd *cobra.Command, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	localAddr := ""
	if flagPort >= 0 {
		localAddr = fmt.Sprintf(":%d", flagPort)
	}
	displayAddr := localAddr
	if displayAddr == "" {
		displayAddr = "relay-only"
	}
	handler := NewHandler(displayAddr, flagName, func() string { return "Connected" })

	relayURLs := utils.SplitCSV(flagServerURLs)
	if flagDefaultRelays {
		relayURLs = sdk.WithDefaultRelayURLs(ctx, relayURLs...)
	}
	exposure, err := sdk.Expose(ctx, relayURLs, flagName, types.LeaseMetadata{
		Description: flagDescription,
		Tags:        utils.SplitCSV(flagTags),
		Owner:       flagOwner,
		Hide:        flagHide,
	})
	if err != nil {
		return fmt.Errorf("expose: %w", err)
	}
	if exposure != nil {
		defer func() { _ = exposure.Close() }()
	}
	if err := exposure.RunHTTP(ctx, handler, localAddr); err != nil {
		return err
	}
	log.Info().Msg("[chatter-bbs] shutdown complete")
	return nil
}
