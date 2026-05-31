package main

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/gosuda/portal-toys/internal/portalapp"
	"github.com/gosuda/portal-tunnel/v2/sdk"
	"github.com/gosuda/portal-tunnel/v2/types"
	"github.com/gosuda/portal-tunnel/v2/utils"
	"github.com/joho/godotenv"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "simple-chat",
	Short: "Portal demo chat",
	RunE:  runChat,
}

var (
	flagServerURLs   string
	flagDiscovery    bool
	flagBanMITM      bool
	flagPort         int
	flagName         string
	flagIdentityPath string
	flagDataPath     string
	flagCredKey      string
	flagHide         bool
	flagDescription  string
	flagTags         string
	flagOwner        string
	flagAdminUIDs    []string
)

func init() {
	// Load .env file FIRST, before any getEnv/getEnvSlice calls
	_ = godotenv.Load()

	flags := rootCmd.PersistentFlags()
	flags.StringVar(&flagServerURLs, "server-url", getEnv("RELAY", ""), "relayserver base URL(s); repeat or comma-separated (from env RELAY if set)")
	flags.BoolVar(&flagDiscovery, "discovery", portalapp.ResolveBoolEnv(true, "DISCOVERY", "DEFAULT_RELAYS"), "include registry relays and enable relay discovery [env: DISCOVERY, DEFAULT_RELAYS]")
	flags.BoolVar(&flagBanMITM, "ban-mitm", portalapp.ResolveBoolEnv(false, "BAN_MITM"), "ban relay when MITM self-probe detects TLS termination [env: BAN_MITM]")
	flags.IntVar(&flagPort, "port", 3005, "optional local HTTP port (negative to disable)")
	flags.StringVar(&flagName, "name", getEnv("CHAT_NAME", "simple-chat-demo"), "backend display name (from env CHAT_NAME if set)")
	flags.StringVar(&flagIdentityPath, "identity-path", "identity.json", "optional path to load/save the portal identity")
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

	if flagCredKey != "" {
		log.Warn().Msg("[chat] --cred-key is no longer supported with the current portal SDK and will be ignored")
	}
	exposure, err := portalapp.Expose(ctx, sdk.ExposeConfig{
		RelayURLs:    utils.SplitCSV(flagServerURLs),
		BanMITM:      flagBanMITM,
		Discovery:    flagDiscovery,
		IdentityPath: flagIdentityPath,
		Metadata: types.LeaseMetadata{
			Description: flagDescription,
			Tags:        utils.SplitCSV(flagTags),
			Owner:       flagOwner,
			Hide:        flagHide,
		},
	})
	if err != nil {
		return fmt.Errorf("expose: %w", err)
	}
	if exposure != nil {
		defer func() { _ = exposure.Close() }()
	}
	localAddr := ""
	if flagPort >= 0 {
		localAddr = fmt.Sprintf("localhost:%d", flagPort)
	}
	err = exposure.RunHTTP(ctx, handler, localAddr)
	hub.closeAll()
	hub.wait()
	if store != nil {
		if err := store.Close(); err != nil {
			log.Warn().Err(err).Msg("[chat] store close error")
		}
	}
	if err != nil {
		return err
	}
	log.Info().Msg("[chat] shutdown complete")
	return nil
}
