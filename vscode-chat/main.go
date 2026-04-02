package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
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
	Use:   "vscode-chat",
	Short: "Portal demo: VSCode Web relay proxy",
	RunE:  runVSCodeRelay,
}

var (
	flagServerURLs   string
	flagDiscovery    bool
	flagBanMITM      bool
	flagName         string
	flagIdentityPath string
	flagTargetHost   string
	flagTargetPort   int
	flagHide         bool
	flagDescription  string
	flagTags         string
	flagOwner        string
)

func init() {
	flags := rootCmd.PersistentFlags()
	flags.StringVar(&flagServerURLs, "server-url", os.Getenv("RELAY"), "relay base URL(s); repeat or comma-separated (from env RELAY/RELAY_URL if set)")
	flags.BoolVar(&flagDiscovery, "discovery", utils.ResolveBoolEnv(true, "DISCOVERY", "DEFAULT_RELAYS"), "include registry relays and enable relay discovery [env: DISCOVERY, DEFAULT_RELAYS]")
	flags.BoolVar(&flagBanMITM, "ban-mitm", utils.ResolveBoolEnv(false, "BAN_MITM"), "ban relay when MITM self-probe detects TLS termination [env: BAN_MITM]")
	flags.StringVar(&flagName, "name", "vscode-relay", "Display name shown on server UI")
	flags.StringVar(&flagIdentityPath, "identity-path", "identity.json", "optional path to load/save the portal identity")
	flags.StringVar(&flagTargetHost, "target-host", "127.0.0.1", "Local host where VSCode Web listens")
	flags.IntVar(&flagTargetPort, "target-port", 8100, "Local port where VSCode Web listens")
	flags.BoolVar(&flagHide, "hide", false, "hide this lease from portal listings")
	flags.StringVar(&flagDescription, "description", "Portal demo: VSCode Web relay proxy", "lease description")
	flags.StringVar(&flagOwner, "owner", "VSCode Relay", "lease owner")
	flags.StringVar(&flagTags, "tags", "dev,vscode", "comma-separated lease tags")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("execute vscode-chat command")
	}
}

func runVSCodeRelay(cmd *cobra.Command, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Build reverse proxy to the local VSCode Web instance
	backendURL, err := url.Parse(fmt.Sprintf("http://%s:%d", flagTargetHost, flagTargetPort))
	if err != nil {
		return fmt.Errorf("parse target url: %w", err)
	}
	proxy := httputil.NewSingleHostReverseProxy(backendURL)
	// Trust X-Forwarded headers for ws and origin handling
	proxy.Director = func(req *http.Request) {
		req.URL.Scheme = backendURL.Scheme
		req.URL.Host = backendURL.Host
		// Strip relay peer prefix if present
		const prefix = "/peer/"
		if after, ok := strings.CutPrefix(req.URL.Path, prefix); ok {
			rest := after
			if i := strings.IndexByte(rest, '/'); i >= 0 {
				req.URL.Path = rest[i:]
			}
		}
		// Preserve original Host for backend
		req.Host = backendURL.Host
		// Add forwarding headers
		req.Header.Set("X-Forwarded-Host", req.Host)
		req.Header.Set("X-Forwarded-Proto", "http")
	}

	exposure, err := sdk.Expose(ctx, sdk.ExposeConfig{
		RelayURLs:    utils.SplitCSV(flagServerURLs),
		BanMITM:      flagBanMITM,
		Discovery:    flagDiscovery,
		Name:         flagName,
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
	if err := exposure.RunHTTP(ctx, proxy, ""); err != nil {
		return err
	}
	log.Info().Msg("[vscode-relay] shutdown complete")
	return nil
}
