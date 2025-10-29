package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	sdk "github.com/gosuda/portal/sdk"
)

var rootCmd = &cobra.Command{
	Use:   "relaydns-chatter-bbs",
	Short: "RelayDNS demo: Chatter BBS (Reverse Proxy Backend)",
	RunE:  runChatter,
}

var (
	flagServerURL string
	flagPort      int
	flagName      string
	flagTargetURL string // Local service URL for reverse proxy (Node.js server)
)

func init() {
	flags := rootCmd.PersistentFlags()
	flags.StringVar(&flagTargetURL, "target-url", "http://127.0.0.1:8081", "Local URL of the actual HTTP service (e.g., Node.js server)")
	flags.StringVar(&flagServerURL, "server-url", "wss://relaydns.gosuda.org/relay", "Relay websocket URL")
	// Set local port default to 0 to minimize conflicts since the service runs on target-url
	flags.IntVar(&flagPort, "port", 0, "Optional local HTTP port for status/debug (0 to disable)") 
	flags.StringVar(&flagName, "name", "chatter-bbs", "Backend display name")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("execute chatter-bbs command")
	}
}

func runChatter(cmd *cobra.Command, args []string) error {
	// Cancellation context for graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 1. Parse the target URL for the proxy
	target, err := url.Parse(flagTargetURL)
	if err != nil {
		return fmt.Errorf("invalid target URL: %w", err)
	}

	// 2. Create the Reverse Proxy Handler
	// All traffic will be forwarded to the Node.js server at flagTargetURL.
	proxy := httputil.NewSingleHostReverseProxy(target)
	
	// Create a minimal mux. All routes go to the proxy, except an explicit status check.
	mux := http.NewServeMux()
	
	// Use the NewHandler defined in view.go for status/health checks
	mux.Handle("/status", NewHandler("relay", flagName, func() string { return "Connected" }))
	mux.Handle("/healthz", NewHandler("relay", flagName, func() string { return "Connected" }))
	
	// All other traffic, including the root '/', is proxied to the target server.
	mux.Handle("/", proxy)


	// 3. Relay Client setup
	client, err := sdk.NewClient(func(c *sdk.RDClientConfig) { c.BootstrapServers = []string{flagServerURL} })
	if err != nil {
		return fmt.Errorf("new client: %w", err)
	}
	defer client.Close()

	cred := sdk.NewCredential()
	listener, err := client.Listen(cred, flagName, []string{"http/1.1"})
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	// 4. Serve over relay using the proxy handler
	go func() {
		if err := http.Serve(listener, mux); err != nil && err != http.ErrServerClosed && ctx.Err() == nil {
			log.Error().Err(err).Msg("[chatter-bbs] relay http error")
		}
	}()

	// 5. Optional local HTTP (for status/proxy check)
	var httpSrv *http.Server
	if flagPort > 0 {
		httpSrv = &http.Server{
			Addr:              fmt.Sprintf(":%d", flagPort),
			Handler:           mux, // Use the proxy mux
			ReadHeaderTimeout: 5 * time.Second,
			IdleTimeout:       60 * time.Second,
		}
		log.Info().Msgf("[chatter-bbs] serving proxy locally at http://127.0.0.1:%d -> %s", flagPort, flagTargetURL)
		go func() {
			if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Warn().Err(err).Msg("[chatter-bbs] local http stopped")
			}
		}()
	}

	// 6. Shutdown watcher
	go func() {
		<-ctx.Done()
		_ = listener.Close()
		if httpSrv != nil {
			sctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := httpSrv.Shutdown(sctx); err != nil && err != context.Canceled {
				log.Warn().Err(err).Msg("[chatter-bbs] local http shutdown error")
			}
		}
	}()

	<-ctx.Done()
	log.Info().Msg("[chatter-bbs] shutdown complete")
	return nil
}
