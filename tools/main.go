package main

import (
	"context"
	"embed"
	"fmt"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/gosuda/portal/v2/sdk"
	"github.com/gosuda/portal/v2/types"
)

//go:embed static
var staticFS embed.FS

var rootCmd = &cobra.Command{
	Use:   "tools",
	Short: "Utility tools (hex/dec, base64, JSON, case, QR)",
	RunE:  run,
}

var (
	flagServerURLs  string
	flagPort        int
	flagName        string
	flagHide        bool
	flagDescription string
	flagTags        string
	flagOwner       string
)

func init() {
	flags := rootCmd.PersistentFlags()
	flags.StringVar(&flagServerURLs, "server-url", os.Getenv("RELAY"), "relay base URL(s); repeat or comma-separated (from env RELAY/RELAY_URL if set)")
	flags.IntVar(&flagPort, "port", -1, "optional local HTTP port (negative to disable)")
	flags.StringVar(&flagName, "name", "tools", "backend display name")
	flags.BoolVar(&flagHide, "hide", false, "hide this lease from portal listings")
	flags.StringVar(&flagDescription, "description", "Utility tools (hex/dec, base64, JSON, case, QR)", "lease description")
	flags.StringVar(&flagOwner, "owner", "Tools", "lease owner")
	flags.StringVar(&flagTags, "tags", "utility,tools", "comma-separated lease tags")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("execute tools command")
	}
}

func run(cmd *cobra.Command, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Serve embedded static directory
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		return fmt.Errorf("sub fs: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.Handle("/", http.FileServer(http.FS(sub)))

	exposure, err := sdk.Expose(ctx, sdk.SplitCSV(flagServerURLs), flagName, types.LeaseMetadata{
		Description: flagDescription,
		Tags:        sdk.SplitCSV(flagTags),
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
	if err := exposure.RunHTTP(ctx, mux, localAddr); err != nil {
		return err
	}
	log.Info().Msg("[tools] shutdown complete")
	return nil
}
