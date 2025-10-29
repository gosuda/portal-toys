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
	Use:   "relaydns-client",
	Short: "RelayDNS demo client (local HTTP backend + libp2p advertiser)",
	RunE:  runClient,
}

var (
	flagServerURL string
	flagPort      int
	flagName      string
)

func init() {
	flags := rootCmd.PersistentFlags()
	flags.StringVar(&flagServerURL, "server-url", "wss://relaydns.gosuda.org/relay", "relayserver base URL")
	flags.IntVar(&flagPort, "port", 0, "local backend HTTP port")
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

	// client
	client, err := sdk.NewClient(func(c *sdk.RDClientConfig) { c.BootstrapServers = []string{flagServerURL} })
	if err != nil {
		return fmt.Errorf("new client: %w", err)
	}
	defer client.Close()

	// Relay listener
	cred := sdk.NewCredential()
	listener, err := client.Listen(cred, flagName, []string{"http/1.1"})
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	// app handler
	handler := NewHandler(flagName)

	// Serve over relay in background
	go func() {
		if err := http.Serve(listener, handler); err != nil && err != http.ErrServerClosed && ctx.Err() == nil {
			log.Error().Err(err).Msg("[client] relay http serve error")
		}
	}()

	// Local HTTP serve
	var httpSrv *http.Server
	if flagPort > 0 {
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
		_ = listener.Close()
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
