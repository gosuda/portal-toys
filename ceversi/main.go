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
	"time"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"gosuda.org/portal/sdk"
)

var (
	flagServerURLs  []string
	flagPort        int
	flagBackendPort int
	flagName        string
	flagHide        bool
	flagDescription string
	flagTags        string
	flagOwner       string
	flagCServerPath string
)

var rootCmd = &cobra.Command{
	Use:   "ceversi",
	Short: "Portal Othello/Reversi Game (C backend with Go relay)",
	RunE:  runCeversi,
}

func init() {
	flags := rootCmd.PersistentFlags()
	flags.StringSliceVar(&flagServerURLs, "server-url", strings.Split(os.Getenv("RELAY"), ","), "relay websocket URL(s); repeat or comma-separated (from env RELAY/RELAY_URL if set)")
	flags.IntVar(&flagPort, "port", 31744, "optional local HTTP port (negative to disable)")
	flags.IntVar(&flagBackendPort, "backend-port", 31745, "C server port")
	flags.StringVar(&flagName, "name", "ceversi", "backend display name")
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

	// Portal SDK
	cred := sdk.NewCredential()
    client, err := sdk.NewClient(sdk.WithBootstrapServers(flagServerURLs))
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

	go func() {
		if err := http.Serve(ln, mux); err != nil && err != http.ErrServerClosed && ctx.Err() == nil {
			log.Error().Err(err).Msg("[ceversi] relay http error")
		}
	}()

	// Local HTTP
	var httpSrv *http.Server
	if flagPort >= 0 {
		httpSrv = &http.Server{Addr: fmt.Sprintf(":%d", flagPort), Handler: mux}
		log.Info().Msgf("[ceversi] serving relay locally at http://127.0.0.1:%d", flagPort)
		go func() {
			if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Warn().Err(err).Msg("[ceversi] local http stopped")
			}
		}()
	}

	go func() {
		<-ctx.Done()
		_ = ln.Close()
		_ = client.Close()
		if httpSrv != nil {
			sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = httpSrv.Shutdown(sctx)
		}
	}()

	<-ctx.Done()
	log.Info().Msg("[ceversi] shutdown complete")
	return nil
}
