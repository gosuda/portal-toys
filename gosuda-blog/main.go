package main

import (
	"context"
	"fmt"
	"net/http"
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
	Use:   "gosuda-blog",
	Short: "Portal demo: gosuda blog static site (relay HTTP backend)",
	RunE:  runBlog,
}

var (
	flagServerURLs  []string
	flagPort        int
	flagName        string
	flagDir         string
	flagHide        bool
	flagDescription string
	flagTags        string
	flagOwner       string
)

func init() {
	flags := rootCmd.PersistentFlags()
	flags.StringSliceVar(&flagServerURLs, "server-url", strings.Split(os.Getenv("RELAY"), ","), "relay websocket URL(s); repeat or comma-separated (from env RELAY/RELAY_URL if set)")
	flags.IntVar(&flagPort, "port", -1, "optional local HTTP port (negative to disable)")
	flags.StringVar(&flagName, "name", "gosuda-blog", "Display name shown on server UI")
	flags.StringVar(&flagDir, "dir", "./gosuda-blog/dist", "Directory to serve (built static files)")
	flags.BoolVar(&flagHide, "hide", false, "hide this lease from portal listings")
	flags.StringVar(&flagDescription, "description", "Portal demo: gosuda blog static site (relay HTTP backend)", "lease description")
	flags.StringVar(&flagOwner, "owner", "gosuda-blog", "lease owner")
	flags.StringVar(&flagTags, "tags", "blog,gosuda", "comma-separated lease tags")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("execute gosuda-blog command")
	}
}

func runBlog(cmd *cobra.Command, args []string) error {
	if _, err := os.Stat(flagDir); err != nil {
		return fmt.Errorf("serve directory not found: %w", err)
	}

	// Context for graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 1) Build HTTP mux serving the static directory
	mux := http.NewServeMux()
	// Minimal health endpoint
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	// Quiet favicon 404s
	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	// Serve static files (with SPA friendly behavior)
	mux.Handle("/", fileServerWithSPA(flagDir))

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
	if err := portalapp.RunHTTP(ctx, lease, mux, portalapp.LocalAddrFromPort(flagPort)); err != nil {
		return err
	}
	log.Info().Msg("[blog] shutdown complete")
	return nil
}
