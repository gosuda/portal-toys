package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	sdk "github.com/gosuda/relaydns/sdk"
)

var rootCmd = &cobra.Command{
	Use:   "relaydns-chatter-bbs",
	Short: "RelayDNS demo: Chatter BBS (relay HTTP backend)",
	RunE:  runChatter,
}

var (
	flagServerURL string
	flagPort      int
	flagName      string
)

func init() {
	flags := rootCmd.PersistentFlags()
	flags.StringVar(&flagServerURL, "server-url", "ws://localhost:4017/relay", "relay websocket URL")
	flags.IntVar(&flagPort, "port", 8081, "optional local HTTP port (0 to disable)")
	flags.StringVar(&flagName, "name", "chatter-bbs", "backend display name")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("execute chatter-bbs command")
	}
}

func runChatter(cmd *cobra.Command, args []string) error {
	// Cancellation context
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Build handler (status always Connected when served over relay)
	handler := NewHandler("relay", flagName, func() string { return "Connected" })

	// Relay client (http-backend style)
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

	// Serve over relay
	go func() {
		if err := http.Serve(listener, handler); err != nil && err != http.ErrServerClosed && ctx.Err() == nil {
			log.Error().Err(err).Msg("[chatter-bbs] relay http error")
		}
	}()

	// Optional local HTTP
	var httpSrv *http.Server
	if flagPort > 0 {
		httpSrv = &http.Server{Addr: fmt.Sprintf(":%d", flagPort), Handler: handler}
		log.Info().Msgf("[chatter-bbs] serving locally at http://127.0.0.1:%d", flagPort)
		go func() {
			if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Warn().Err(err).Msg("[chatter-bbs] local http stopped")
			}
		}()
	}

	// Shutdown watcher
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
