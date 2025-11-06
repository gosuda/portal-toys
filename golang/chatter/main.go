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
	flagCredKey   string
)

func init() {
	flags := rootCmd.PersistentFlags()
	flags.StringVar(&flagName, "name", "free-chat", "backend display name")
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

	// Build router
	handler := NewHandler(flagName)

	// client (http-backend style)
	// The bootstrap server is required by the SDK, but not used in this application.
	// We use a dummy value to satisfy the SDK.
	client, err := sdk.NewClient(func(c *sdk.RDClientConfig) {
		c.BootstrapServers = []string{"wss://bootstrap.dummy.com/ws"}
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

	// Wait for cancel
	<-ctx.Done()
	return nil
}
