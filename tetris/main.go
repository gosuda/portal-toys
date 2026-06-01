package main

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/gosuda/portal-toys/internal/portalapp"
	"github.com/gosuda/portal-tunnel/v2/sdk"
	"github.com/gosuda/portal-tunnel/v2/types"
	"github.com/gosuda/portal-tunnel/v2/utils"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "tetris",
	Short: "Portal multiplayer tetris",
	RunE:  runTetris,
}

var (
	flagServerURLs   string
	flagDiscovery    bool
	flagBanMITM      bool
	flagPort         int
	flagName         string
	flagIdentityPath string
	flagHide         bool
	flagDescription  string
	flagTags         string
	flagOwner        string
)

func init() {
	flags := rootCmd.PersistentFlags()
	flags.StringVar(&flagServerURLs, "server-url", os.Getenv("RELAY"), "relay base URL(s); repeat or comma-separated (from env RELAY/RELAY_URL if set)")
	flags.BoolVar(&flagDiscovery, "discovery", portalapp.ResolveBoolEnv(false, "DISCOVERY", "DEFAULT_RELAYS"), "include registry relays and enable relay discovery [env: DISCOVERY, DEFAULT_RELAYS]")
	flags.BoolVar(&flagBanMITM, "ban-mitm", portalapp.ResolveBoolEnv(false, "BAN_MITM"), "ban relay when MITM self-probe detects TLS termination [env: BAN_MITM]")
	flags.IntVar(&flagPort, "port", 3000, "optional local HTTP port (negative to disable)")
	flags.StringVar(&flagName, "name", "example-tetris", "backend display name")
	flags.StringVar(&flagIdentityPath, "identity-path", "identity.json", "optional path to load/save the portal identity")
	flags.BoolVar(&flagHide, "hide", false, "hide this lease from portal listings")
	flags.StringVar(&flagDescription, "description", "Portal multiplayer tetris", "lease description")
	flags.StringVar(&flagOwner, "owner", "Tetris", "lease owner")
	flags.StringVar(&flagTags, "tags", "game,tetris", "comma-separated lease tags")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("execute tetris command")
	}
}

func runTetris(cmd *cobra.Command, args []string) error {
	// Cancellation context
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	server := newServer()

	// Router: static files + websocket
	mux := http.NewServeMux()
	// Serve embedded static assets
	sub, err := fs.Sub(embeddedStatic, "static")
	if err != nil {
		return fmt.Errorf("embed static: %w", err)
	}
	staticFS := http.FileServer(http.FS(sub))
	mux.HandleFunc("/ws", server.handleWS)
	// Handle relay prefix like /peer/{id}/...
	mux.HandleFunc("/peer/", func(w http.ResponseWriter, r *http.Request) {
		// Expected forms:
		//  - /peer/{token}
		//  - /peer/{token}/
		//  - /peer/{token}/<asset>
		const prefix = "/peer/"
		rest := strings.TrimPrefix(r.URL.Path, prefix)
		// Split token and optional suffix
		token := rest
		suffix := ""
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			token = rest[:i]
			suffix = rest[i:]
		}

		// Basic token sanity: avoid treating paths like /peer/app.js as a token-only request
		if token == "" || len(token) < 8 { // keep len low to be permissive, just avoid obviously wrong cases
			http.NotFound(w, r)
			return
		}

		// If no suffix, redirect to add trailing slash so relative assets resolve correctly
		if suffix == "" {
			http.Redirect(w, r, "/peer/"+token+"/", http.StatusMovedPermanently)
			return
		}
		// Serve index explicitly when asking for folder root or index.html
		if suffix == "/" || suffix == "/index.html" {
			b, err := fs.ReadFile(sub, "index.html")
			if err != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(b)
			return
		}

		// Route ws specially
		if suffix == "/ws" {
			server.handleWS(w, r)
			return
		}

		// Rewrite to suffix for static serving
		r2 := r.Clone(r.Context())
		r2.URL.Path = suffix
		staticFS.ServeHTTP(w, r2)
	})
	// Quiet favicon 404s
	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.Handle("/", staticFS)

	exposure, err := portalapp.Expose(ctx, sdk.ExposeConfig{
		RelayURLs:    utils.SplitCSV(flagServerURLs),
		BanMITM:      flagBanMITM,
		Discovery:    flagDiscovery,
		Identity:     types.Identity{Name: flagName},
		IdentityPath: flagIdentityPath,
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
	err = exposure.RunHTTP(ctx, mux, localAddr)
	server.closeAll()
	server.wait()
	if err != nil {
		return err
	}
	log.Info().Msg("[tetris] shutdown complete")
	return nil
}
