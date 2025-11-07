package main

import (
	"context"
	"encoding/base64"
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

	"gosuda.org/portal/portal/core/cryptoops"
	"gosuda.org/portal/sdk"
)

var rootCmd = &cobra.Command{
	Use:   "simple-chat",
	Short: "Portal demo chat",
	RunE:  runChat,
}

var (
	flagServerURLs []string
	flagPort       int
	flagName       string
	flagDataPath   string
	flagCredKey    string
)

func init() {
	flags := rootCmd.PersistentFlags()
	flags.StringSliceVar(&flagServerURLs, "server-url", strings.Split(os.Getenv("RELAY"), ","), "relayserver base URL(s); repeat or comma-separated (from env RELAY/RELAY_URL if set)")
	flags.IntVar(&flagPort, "port", -1, "optional local HTTP port (negative to disable)")
	flags.StringVar(&flagName, "name", "simple-chat", "backend display name")
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

	// Shared credential across all relay listeners
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
	// Build clients and listeners for all provided relays
	var clients []*sdk.RDClient
	var listeners []net.Listener
	for _, raw := range flagServerURLs {
		if raw == "" {
			continue
		}
		parts := strings.Split(raw, ",")
		for _, p := range parts {
			u := strings.TrimSpace(p)
			if u == "" {
				continue
			}
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

	// Serve over each relay listener
	for i, ln := range listeners {
		idx := i
		go func() {
			if err := http.Serve(ln, handler); err != nil && err != http.ErrServerClosed && ctx.Err() == nil {
				log.Error().Err(err).Int("listener", idx).Msg("[chat] relay http error")
			}
		}()
	}

	// Optional local server on --port
	var httpSrv *http.Server
	if flagPort >= 0 {
		httpSrv = &http.Server{Addr: fmt.Sprintf(":%d", flagPort), Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
		log.Info().Msgf("[chat] serving locally at http://127.0.0.1:%d", flagPort)
		go func() {
			if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Warn().Err(err).Msg("[chat] local http stopped")
			}
		}()
	}

	// Unified shutdown watcher
	go func() {
		<-ctx.Done()
		for _, ln := range listeners {
			_ = ln.Close()
		}
		for _, c := range clients {
			_ = c.Close()
		}
		if httpSrv != nil {
			sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := httpSrv.Shutdown(sctx); err != nil && err != context.Canceled {
				log.Error().Err(err).Msg("[chat] http server shutdown error")
			}
		}
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
