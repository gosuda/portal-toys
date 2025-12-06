package main

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strings"
	"syscall"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"gosuda.org/portal/sdk"
)

//go:embed doom/public/*
var doomAssets embed.FS

var rootCmd = &cobra.Command{
	Use:   "doom",
	Short: "Portal demo: Doom (served over portal HTTP backend)",
	RunE:  runDoom,
}

var (
	flagServerURLs  []string
	flagPort        int
	flagName        string
	flagHide        bool
	flagDescription string
	flagTags        string
	flagOwner       string
)

func init() {
	flags := rootCmd.PersistentFlags()
	flags.StringSliceVar(&flagServerURLs, "server-url", strings.Split(os.Getenv("RELAY"), ","), "relayserver base URL(s); repeat or comma-separated (from env RELAY/RELAY_URL if set)")
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
	// 1) Create single SDK client and connect to relay(s)
	cred := sdk.NewCredential()
	client, err := sdk.NewClient(func(c *sdk.RDClientConfig) { c.BootstrapServers = flagServerURLs })
	if err != nil {
		return fmt.Errorf("new client: %w", err)
	}
	ln, err := client.Listen(cred, flagName, []string{"http/1.1"},
		sdk.WithDescription(flagDescription),
		sdk.WithHide(flagHide),
		sdk.WithOwner(flagOwner),
		sdk.WithTags(strings.Split(flagTags, ",")),
	)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	// 4) Build HTTP handler to serve embedded Doom assets
	staticFS, err := fs.Sub(doomAssets, "doom/public")
	if err != nil {
		return fmt.Errorf("static assets: %w", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.Handle("/", withStaticHeaders(http.FileServer(http.FS(staticFS))))

	// 5) Serve HTTP directly over the relay listener
	log.Info().Msgf("[doom] serving HTTP over relay; lease=%s id=%s", flagName, cred.ID())
	srvErr := make(chan error, 1)
	go func() {
		srvErr <- http.Serve(ln, mux)
	}()

	// 6) Wait for termination or HTTP error
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-sig:
		log.Info().Msg("[doom] shutting down...")
	case err := <-srvErr:
		if err != nil {
			log.Error().Err(err).Msg("[doom] http serve error")
		}
	}

	_ = ln.Close()
	_ = client.Close()
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
