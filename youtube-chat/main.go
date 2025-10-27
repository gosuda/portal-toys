package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	sdk "github.com/gosuda/relaydns/sdk/go"
)

var rootCmd = &cobra.Command{
	Use:   "relaydns-paint",
	Short: "RelayDNS demo: collaborative youtube chat",
	RunE:  runPaint,
}

var (
	flagServerURL string
	flagPort      int
	flagName      string
)

func init() {
	flags := rootCmd.PersistentFlags()
	flags.StringVar(&flagServerURL, "server-url", "http://relaydns.gosuda.org", "relayserver base URL to auto-fetch multiaddrs from /health")
	flags.IntVar(&flagPort, "port", 8094, "local paint HTTP port")
	flags.StringVar(&flagName, "name", "youtube-chat", "backend display name")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("execute paint command")
	}
}

func runPaint(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1) start local HTTP backend
	httpLn, err := net.Listen("tcp", fmt.Sprintf(":%d", flagPort))
	if err != nil {
		return fmt.Errorf("listen paint: %w", err)
	}

	dh := newDrawHub()
	handler := NewHandler(flagName, dh)
	httpSrv := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		if err := httpSrv.Serve(httpLn); err != nil && err != http.ErrServerClosed {
			log.Error().Err(err).Msg("paint http error")
		}
	}()

	// 2) advertise over RelayDNS (HTTP tunneled via server /peer route)
	client, err := sdk.NewClient(ctx, sdk.ClientConfig{
		Name:      flagName,
		TargetTCP: fmt.Sprintf("127.0.0.1:%d", flagPort),
		ServerURL: flagServerURL,
	})
	if err != nil {
		return fmt.Errorf("new client: %w", err)
	}
	if err := client.Start(ctx); err != nil {
		return fmt.Errorf("start client: %w", err)
	}

	// wait for termination
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Info().Msg("[paint] shutting down...")

	// Shutdown sequence
	if err := client.Close(); err != nil {
		log.Warn().Err(err).Msg("[paint] client close error")
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("[paint] http server shutdown error")
	}
	log.Info().Msg("[paint] shutdown complete")
	return nil
}
