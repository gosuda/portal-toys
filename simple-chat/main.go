package main

import (
	"context"
	"embed"
	"encoding/base64"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"gosuda.org/portal/portal/core/cryptoops"
	"gosuda.org/portal/sdk"
)

//go:embed static
var embeddedStatic embed.FS

var rootCmd = &cobra.Command{
	Use:   "simple-chat",
	Short: "Portal demo chat",
	RunE:  runChat,
}

var (
	flagServerURLs  []string
	flagPort        int
	flagName        string
	flagDataPath    string
	flagCredKey     string
	flagHide        bool
	flagDescription string
	flagTags        string
	flagOwner       string
	flagAdminUIDs   []string
)

// getEnv returns the environment variable value or the default value if not set
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvBool returns the environment variable as a boolean or the default value if not set
func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		return value == "true" || value == "1" || value == "yes"
	}
	return defaultValue
}

// getEnvSlice returns a comma-separated environment variable as a string slice, filtering empty strings
func getEnvSlice(key string) []string {
	value := os.Getenv(key)
	if value == "" {
		return nil
	}
	// Remove surrounding quotes if present
	value = strings.Trim(value, "'\"")
	parts := strings.Split(value, ",")
	// Filter out empty strings and trim quotes/whitespace
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		trimmed = strings.Trim(trimmed, "'\"")
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func init() {
	// Load .env file FIRST, before any getEnv/getEnvSlice calls
	_ = godotenv.Load()

	flags := rootCmd.PersistentFlags()
	flags.StringSliceVar(&flagServerURLs, "server-url", getEnvSlice("RELAY"), "relayserver base URL(s); repeat or comma-separated (from env RELAY if set)")
	flags.IntVar(&flagPort, "port", -1, "optional local HTTP port (negative to disable)")
	flags.StringVar(&flagName, "name", getEnv("CHAT_NAME", "simple-chat"), "backend display name (from env CHAT_NAME if set)")
	flags.BoolVar(&flagHide, "hide", getEnvBool("CHAT_HIDE", false), "hide this lease from portal listings (from env CHAT_HIDE if set)")
	flags.StringVar(&flagDescription, "description", getEnv("CHAT_DESCRIPTION", "Portal demo chat"), "lease description (from env CHAT_DESCRIPTION if set)")
	flags.StringVar(&flagOwner, "owner", getEnv("CHAT_OWNER", "Simple Chat"), "lease owner (from env CHAT_OWNER if set)")
	flags.StringVar(&flagTags, "tags", getEnv("CHAT_TAGS", "chat,simple"), "comma-separated lease tags (from env CHAT_TAGS if set)")
	flags.StringVar(&flagDataPath, "data-path", getEnv("CHAT_DATA_PATH", ""), "optional directory to persist chat history via PebbleDB (from env CHAT_DATA_PATH if set)")
	flags.StringVar(&flagCredKey, "cred-key", getEnv("CHAT_CRED_KEY", ""), "optional credential key to use for the listener (base64 encoded) (from env CHAT_CRED_KEY if set)")
	flags.StringSliceVar(&flagAdminUIDs, "admin-uid", getEnvSlice("CHAT_ADMIN_UIDS"), "comma-separated list of admin UIDs (from env CHAT_ADMIN_UIDS if set)")
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

	// Add admin UIDs from flag/env
	log.Info().Strs("admin_uids", flagAdminUIDs).Msg("[chat] configured admin UIDs")
	adminCount := 0
	for _, uid := range flagAdminUIDs {
		uid = strings.TrimSpace(uid)
		if uid != "" && uid != "," {
			hub.addAdmin(uid)
			adminCount++
		}
	}
	if adminCount == 0 {
		log.Warn().Msg("[chat] no admin UIDs configured - use CHAT_ADMIN_UIDS env var or --admin-uid flag")
	}

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
	// Prepare embedded static files
	staticFS, err := fs.Sub(embeddedStatic, "static")
	if err != nil {
		return fmt.Errorf("embed sub FS: %w", err)
	}

	// Build router
	handler := NewHandler(flagName, hub, staticFS)

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
	// Single client and listener over all bootstrap servers
	client, err := sdk.NewClient(func(c *sdk.RDClientConfig) { c.BootstrapServers = flagServerURLs })
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
		if err := http.Serve(ln, handler); err != nil && err != http.ErrServerClosed && ctx.Err() == nil {
			log.Error().Err(err).Msg("[chat] relay http error")
		}
	}()

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
		_ = ln.Close()
		_ = client.Close()
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
