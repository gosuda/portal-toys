package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/gosuda/portal/v2/sdk"
	"github.com/gosuda/portal/v2/types"
	"github.com/gosuda/portal/v2/utils"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "youtube-chat",
	Short: "Portal demo: collaborative youtube chat (relay HTTP backend)",
	RunE:  runYouTubeChat,
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
	flags.StringVar(&flagName, "name", "youtube-chat", "backend display name")
	flags.BoolVar(&flagHide, "hide", false, "hide this lease from portal listings")
	flags.StringVar(&flagDescription, "description", "Portal demo: collaborative youtube chat (relay HTTP backend)", "lease description")
	flags.StringVar(&flagOwner, "owner", "YouTube Chat", "lease owner")
	flags.StringVar(&flagTags, "tags", "chat,youtube", "comma-separated lease tags")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("execute youtube-chat command")
	}
}

func runYouTubeChat(cmd *cobra.Command, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dh := newDrawHub()
	baseHandler := NewHandler(flagName, dh)

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
	localAddr := ""
	if flagPort >= 0 {
		localAddr = fmt.Sprintf(":%d", flagPort)
	}
	if err := exposure.RunHTTP(ctx, handler, localAddr); err != nil {
		return err
	}
	log.Info().Msg("[ytchat] shutdown complete")
	return nil
}
