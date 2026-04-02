package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"github.com/gosuda/portal/v2/sdk"
	"github.com/gosuda/portal/v2/types"
	"github.com/gosuda/portal/v2/utils"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var (
	flagServerURLs   []string
	flagDiscovery    bool
	flagBanMITM      bool
	flagPort         int
	flagBackendPort  int
	flagName         string
	flagIdentityPath string
	flagHide         bool
	flagDescription  string
	flagTags         string
	flagOwner        string
	flagCServerPath  string
)

var rootCmd = &cobra.Command{
	Use:   "ceversi",
	Short: "Portal Othello/Reversi Game (C backend with Go relay)",
	RunE:  runCeversi,
}

func init() {
	flags := rootCmd.PersistentFlags()
	flags.StringSliceVar(&flagServerURLs, "server-url", strings.Split(os.Getenv("RELAY"), ","), "relay site URL(s); repeat or comma-separated (from env RELAY/RELAY_URL if set)")
	flags.BoolVar(&flagDiscovery, "discovery", utils.ResolveBoolEnv(true, "DISCOVERY", "DEFAULT_RELAYS"), "include registry relays and enable relay discovery [env: DISCOVERY, DEFAULT_RELAYS]")
	flags.BoolVar(&flagBanMITM, "ban-mitm", utils.ResolveBoolEnv(false, "BAN_MITM"), "ban relay when MITM self-probe detects TLS termination [env: BAN_MITM]")
	flags.IntVar(&flagPort, "port", 31744, "optional local HTTP port (negative to disable)")
	flags.IntVar(&flagBackendPort, "backend-port", 31745, "C server port")
	flags.StringVar(&flagName, "name", "ceversi", "backend display name")
	flags.StringVar(&flagIdentityPath, "identity-path", "identity.json", "optional path to load/save the portal identity")
	flags.BoolVar(&flagHide, "hide", false, "hide this lease from portal listings")
	flags.StringVar(&flagDescription, "description", "Simple Othello/Reversi game written in C", "lease description")
	flags.StringVar(&flagOwner, "owner", "Ceversi", "lease owner")
	flags.StringVar(&flagTags, "tags", "game,othello,reversi", "comma-separated lease tags")
	flags.StringVar(&flagCServerPath, "c-server", "./server", "path to C server binary")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("execute ceversi command")
	}
}

func runCeversi(cmd *cobra.Command, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Start C backend
	cCmd := exec.CommandContext(ctx, flagCServerPath, "--no-certs")
	// We need to tell the C server which port to use.
	// Current C server has port 31744 hardcoded. I will change it in src/main.c later.
	// For now let's assume it uses flagBackendPort if we can pass it,
	// but the C server code I saw doesn't take a port arg yet.
	// I'll modify src/main.c to take a port or use an env var.
	cCmd.Env = append(os.Environ(), fmt.Sprintf("PORT=%d", flagBackendPort))
	cCmd.Stdout = os.Stdout
	cCmd.Stderr = os.Stderr

	if err := cCmd.Start(); err != nil {
		return fmt.Errorf("start C server: %w", err)
	}
	log.Info().Msgf("Started C backend on port %d", flagBackendPort)

	// Setup Proxy
	backendURL, _ := url.Parse(fmt.Sprintf("http://localhost:%d", flagBackendPort))
	proxy := httputil.NewSingleHostReverseProxy(backendURL)

	mux := http.NewServeMux()
	mux.Handle("/", proxy)

	exposure, err := sdk.Expose(ctx, sdk.ExposeConfig{
		RelayURLs:    append([]string(nil), flagServerURLs...),
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
	localAddr := ""
	if flagPort >= 0 {
		localAddr = fmt.Sprintf(":%d", flagPort)
	}
	if err := exposure.RunHTTP(ctx, mux, localAddr); err != nil {
		return err
	}
	log.Info().Msg("[ceversi] shutdown complete")
	return nil
}
