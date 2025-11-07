package main

import (
    "context"
    "fmt"
    "net"
    "net/http"
    "os"
    "os/signal"
    "strings"
    "syscall"
    "time"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"gosuda.org/portal/sdk"
)

var rootCmd = &cobra.Command{
	Use:   "client",
	Short: "Portal demo client",
	RunE:  runClient,
}

var (
    flagServerURLs []string
    flagPort       int
    flagName       string
)

func init() {
    flags := rootCmd.PersistentFlags()
    flags.StringSliceVar(&flagServerURLs, "server-url", strings.Split(os.Getenv("RELAY"), ","), "relayserver base URL(s); repeat or comma-separated (from env RELAY/RELAY_URL if set)")
    flags.IntVar(&flagPort, "port", -1, "optional local HTTP port (negative to disable)")
    flags.StringVar(&flagName, "name", "example-backend", "backend display name shown on server UI")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("execute root command")
	}
}

func runClient(cmd *cobra.Command, args []string) error {
	// Context for graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

    // clients + relay listeners (multi)
    cred := sdk.NewCredential()
    var clients []*sdk.RDClient
    var listeners []net.Listener
    for _, raw := range flagServerURLs {
        if raw == "" { continue }
        for _, p := range strings.Split(raw, ",") {
            u := strings.TrimSpace(p)
            if u == "" { continue }
            client, err := sdk.NewClient(func(c *sdk.RDClientConfig) { c.BootstrapServers = []string{u} })
            if err != nil {
                log.Error().Err(err).Str("url", u).Msg("new client failed")
                continue
            }
            clients = append(clients, client)
            ln, err := client.Listen(cred, flagName, []string{"http/1.1"})
            if err != nil {
                return fmt.Errorf("listen (%s): %w", u, err)
            }
            listeners = append(listeners, ln)
        }
    }
    if len(listeners) == 0 {
        return fmt.Errorf("no valid relay servers provided via --server-url or RELAY/RELAY_URL env")
    }

	// app handler
	handler := NewHandler(flagName)

	// Serve over relay in background
    for i, ln := range listeners {
        idx := i
        go func() {
            if err := http.Serve(ln, handler); err != nil && err != http.ErrServerClosed && ctx.Err() == nil {
                log.Error().Err(err).Int("listener", idx).Msg("[client] relay http serve error")
            }
        }()
    }

	// Local HTTP serve
	var httpSrv *http.Server
	if flagPort >= 0 {
		httpSrv = &http.Server{Addr: fmt.Sprintf(":%d", flagPort), Handler: handler}
		log.Info().Int("port", flagPort).Msg("[client] serving local http")
		go func() {
			if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Warn().Err(err).Msg("[client] local http stopped")
			}
		}()
	}

	// handle shutdown and cleanup
	go func() {
        <-ctx.Done()
        for _, ln := range listeners { _ = ln.Close() }
        for _, c := range clients { _ = c.Close() }
        if httpSrv != nil {
            sctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
            defer cancel()
            if err := httpSrv.Shutdown(sctx); err != nil && err != context.Canceled {
                log.Warn().Err(err).Msg("[client] local http shutdown error")
            }
        }
    }()

	// Wait for shutdown
	<-ctx.Done()
	log.Info().Msg("[client] shutdown complete")
	return nil
}
