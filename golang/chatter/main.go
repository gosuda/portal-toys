package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"gosuda.org/portal/portal/core/cryptoops"
	"gosuda.org/portal/sdk"
)

var rootCmd = &cobra.Command{
	Use:   "free-chat",
	Short: "Portal demo chat",
	RunE:  runChat,
}

var (
	flagName      string
	flagDataPath  string
	flagCredKey   string
)

func init() {
	flags := rootCmd.PersistentFlags()
	flags.StringVar(&flagName, "name", "free-chat", "backend display name")
	flags.StringVar(&flagDataPath, "data-path", "", "optional directory to persist chat history via PebbleDB")
	flags.StringVar(&flagCredKey, "cred-key", "", "optional credential key to use for the listener (base64 encoded)")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("execute chat command")
	}
}

func runChat(cmd *cobra.Command, args []string) error {
	// Cancellation context for graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	hub := newHub()

	// Optional: open persistent store and preload history
	var store *messageStore
	if flagDataPath != "" {
		s, err := openMessageStore(flagDataPath)
		if err != nil {
			log.Warn().Err(err).Msg("[chat] open store failed; running in memory only")
		} else {
			store = s
			// Load only the most recent 100 messages to avoid slow startup
			if msgs, err := store.LoadRecent(100); err != nil {
				log.Warn().Err(err).Msg("[chat] load history failed")
			} else if len(msgs) > 0 {
				hub.bootstrap(msgs)
				log.Info().Msgf("[chat] loaded %d recent messages from store", len(msgs))
			}
			hub.attachStore(store)
		}
	}
	// Build router
	handler := NewHandler(flagName, hub)

	// client (http-backend style)
	client, err := sdk.NewClient(func(c *sdk.RDClientConfig) {
		c.BootstrapServers = []string{"chat.korokorok.com"}
	})
	if err != nil {
		return fmt.Errorf("new client: %w", err)
	}
	defer client.Close()

	cred := sdk.NewCredential()
	if flagCredKey != "" {
		key, err := base64.StdEncoding.DecodeString(flagCredKey)
		if err != nil {
			return fmt.Errorf("decode cred key: %w", err)
		}
		cred2, err := cryptoops.NewCredentialFromPrivateKey(key)
		if err != nil {
			return fmt.Errorf("new credential from private key: %w", err)
		}
		cred = cred2
	}
	listener, err := client.Listen(cred, flagName, []string{"http/1.1"})
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	// Serve over relay
	go func() {
		if err := http.Serve(listener, handler); err != nil && err != http.ErrServerClosed && ctx.Err() == nil {
			log.Error().Err(err).Msg("[chat] relay http error")
		}
	}()

	// Unified shutdown watcher
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	// Wait for cancel, then clean up hub/store
	<-ctx.Done()
	hub.closeAll()
	hub.wait()
	if store != nil {
		if err := store.Close(); err != nil {
			log.Warn().Err(err).Msg("[chat] store close error")
		}
	}
	log.Info().Msg("[chat] shutdown complete")
	return nil
}
