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
	Use:   "relaydns-chat",
	Short: "RelayDNS demo chat (local HTTP backend + libp2p advertiser)",
	RunE:  runChat,
}

var (
	flagServerURL string
	flagPort      int
	flagName      string
	flagDataPath  string
)

func init() {
	flags := rootCmd.PersistentFlags()
	flags.StringVar(&flagServerURL, "server-url", "http://relaydns.gosuda.org", "relayserver base URL to auto-fetch multiaddrs from /health")
	flags.IntVar(&flagPort, "port", 8091, "local chat HTTP port")
	flags.StringVar(&flagName, "name", "example-chat", "backend display name")
	flags.StringVar(&flagDataPath, "data-path", "", "optional directory to persist chat history via PebbleDB")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("execute chat command")
	}
}

func runChat(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // Ensure context is cancelled on all exit paths

	// 1) start local chat HTTP backend
	httpLn, err := net.Listen("tcp", fmt.Sprintf(":%d", flagPort))
	if err != nil {
		return fmt.Errorf("listen chat: %w", err)
	}
	hub := newHub()

	// Optional: open persistent store and preload history
	var store *messageStore
	if flagDataPath != "" {
		s, err := openMessageStore(flagDataPath)
		if err != nil {
			log.Warn().Err(err).Msg("[chat] open store failed; running in memory only")
		} else {
			store = s
			if msgs, err := store.LoadAll(); err != nil {
				log.Warn().Err(err).Msg("[chat] load history failed")
			} else if len(msgs) > 0 {
				hub.bootstrap(msgs)
				log.Info().Msgf("[chat] loaded %d messages from store", len(msgs))
			}
			hub.attachStore(store)
		}
	}
	// Build router and run server in main
	handler := NewHandler(flagName, hub)
	httpSrv := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		if err := httpSrv.Serve(httpLn); err != nil && err != http.ErrServerClosed {
			log.Error().Err(err).Msg("chat http error")
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

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Info().Msg("[chat] shutting down...")

	// Shutdown sequence:
	// 1. Close client (waits for goroutines, closes libp2p host)
	if err := client.Close(); err != nil {
		log.Warn().Err(err).Msg("[chat] client close error")
	}

	// 2. Shutdown HTTP server with a fresh context (with timeout)
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("[chat] http server shutdown error")
	}

	// 3. Close all websocket connections and wait for handlers to finish
	hub.closeAll()
	hub.wait()

	// 4. Close persistent store if opened
	if store != nil {
		if err := store.Close(); err != nil {
			log.Warn().Err(err).Msg("[chat] store close error")
		}
	}

	log.Info().Msg("[chat] shutdown complete")
	return nil
}
