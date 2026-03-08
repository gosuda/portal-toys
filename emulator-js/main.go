package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"path"
	"syscall"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"github.com/gosuda/portal/v2/sdk"
	"github.com/gosuda/portal/v2/types"
)

//go:embed static
var emulatorAssets embed.FS

var rootCmd = &cobra.Command{
	Use:   "emulatorjs",
	Short: "Portal demo: EmulatorJS (served over portal HTTP backend)",
	RunE:  runEmulator,
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
	flags.StringVar(&flagServerURLs, "server-url", os.Getenv("RELAY"), "relayserver base URL(s); repeat or comma-separated (from env RELAY/RELAY_URL if set)")
	flags.IntVar(&flagPort, "port", -1, "optional local HTTP port (negative to disable)")
	flags.StringVar(&flagName, "name", "emulator-js", "backend display name")
	flags.BoolVar(&flagHide, "hide", false, "hide this lease from portal listings")
	flags.StringVar(&flagDescription, "description", "Portal demo: EmulatorJS (served over portal HTTP backend)", "lease description")
	flags.StringVar(&flagOwner, "owner", "EmulatorJS", "lease owner")
	flags.StringVar(&flagTags, "tags", "game,emulator", "comma-separated lease tags")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("execute emulatorjs command")
	}
}

func runEmulator(cmd *cobra.Command, args []string) error {
	// 1) Cancellation context for graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	// Serve static/ as the site root
	staticFS, err := fs.Sub(emulatorAssets, "static")
	if err != nil {
		return fmt.Errorf("sub fs static: %w", err)
	}
	mux.Handle("/", withStaticHeaders(http.FileServer(http.FS(staticFS))))

	// Also expose top-level data and docs
	mux.Handle("/data/", withStaticHeaders(http.FileServer(http.FS(emulatorAssets))))
	mux.Handle("/docs/", withStaticHeaders(http.FileServer(http.FS(emulatorAssets))))

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
	log.Info().Msg("[emulatorjs] shutdown complete")
	return nil
}

func withStaticHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set appropriate headers for static assets
		w.Header().Set("X-Content-Type-Options", "nosniff")

		ext := path.Ext(r.URL.Path)
		if ext == ".html" || r.URL.Path == "/" {
			w.Header().Set("Cache-Control", "no-cache")
		} else {
			// Cache JS, CSS, WASM, images for 1 day
			w.Header().Set("Cache-Control", "public, max-age=86400")
		}

		next.ServeHTTP(w, r)
	})
}
