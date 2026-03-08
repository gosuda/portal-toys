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

//go:embed doom/public/*
var doomAssets embed.FS

var rootCmd = &cobra.Command{
	Use:   "doom",
	Short: "Portal demo: Doom (served over portal HTTP backend)",
	RunE:  runDoom,
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
	flags.StringVar(&flagName, "name", "doom", "backend display name")
	flags.BoolVar(&flagHide, "hide", false, "hide this lease from portal listings")
	flags.StringVar(&flagDescription, "description", "Portal demo: Doom (served over portal HTTP backend)", "lease description")
	flags.StringVar(&flagOwner, "owner", "Doom", "lease owner")
	flags.StringVar(&flagTags, "tags", "game,doom", "comma-separated lease tags")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("execute doom command")
	}
}

func runDoom(cmd *cobra.Command, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	staticFS, err := fs.Sub(doomAssets, "doom/public")
	if err != nil {
		return fmt.Errorf("static assets: %w", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.Handle("/", withStaticHeaders(http.FileServer(http.FS(staticFS))))

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
	log.Info().Msg("[doom] shutdown complete")
	return nil
}

func withStaticHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Doom assets contain wasm + JS, so disable sniffing and cache non-HTML responses.
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if path.Ext(r.URL.Path) == ".html" || r.URL.Path == "/" {
			w.Header().Set("Cache-Control", "no-cache")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=86400")
		}
		next.ServeHTTP(w, r)
	})
}
