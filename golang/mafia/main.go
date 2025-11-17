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
	Use:   "mafia",
	Short: "Portal demo: multi-room mafia game",
	RunE:  runServer,
}

var (
	flagServerURLs []string
	flagPort       int
	flagName       string
	flagCredKey    string
	flagAuthKey    string
)

func init() {
	flags := rootCmd.PersistentFlags()
	flags.StringSliceVar(&flagServerURLs, "server-url", strings.Split(os.Getenv("RELAY"), ","), "relayserver base URL(s); repeat or comma-separated (from env RELAY/RELAY_URL if set)")
	flags.IntVar(&flagPort, "port", -1, "optional local HTTP port (negative to disable)")
	flags.StringVar(&flagName, "name", "mafia", "backend display name")
	flags.StringVar(&flagCredKey, "cred-key", "", "optional credential key to use for the listener (base64 encoded)")
	flags.StringVar(&flagAuthKey, "ws-auth-key", os.Getenv("MAFIA_WS_AUTH"), "optional shared secret required from clients via X-Mafia-Key header")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("execute mafia command")
	}
}

func runServer(cmd *cobra.Command, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	servers := make([]string, 0, len(flagServerURLs))
	for _, raw := range flagServerURLs {
		if trimmed := strings.TrimSpace(raw); trimmed != "" {
			servers = append(servers, trimmed)
		}
	}

	mgr := NewRoomManager()
	handler := NewHTTPServer(mgr, flagAuthKey)

	var (
		ln     net.Listener
		client *sdk.RDClient
	)

	if len(servers) > 0 {
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

		c, err := sdk.NewClient(func(cfg *sdk.RDClientConfig) {
			cfg.BootstrapServers = servers
		})
		if err != nil {
			return fmt.Errorf("new client: %w", err)
		}
		listener, err := c.Listen(cred, flagName, []string{"http/1.1"})
		if err != nil {
			_ = c.Close()
			return fmt.Errorf("listen: %w", err)
		}
		client = c
		ln = listener
		log.Info().Msg("[mafia] relay listener enabled")
	} else {
		log.Info().Msg("[mafia] relay disabled; running local mode only")
	}

	mux := handler.Router()
	if ln != nil {
		go func() {
			if err := http.Serve(ln, mux); err != nil && err != http.ErrServerClosed && ctx.Err() == nil {
				log.Error().Err(err).Msg("[mafia] relay http error")
			}
		}()
	}

	var httpSrv *http.Server
	if flagPort >= 0 {
		httpSrv = &http.Server{Addr: fmt.Sprintf(":%d", flagPort), Handler: mux, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
		log.Info().Msgf("[mafia] serving locally at http://127.0.0.1:%d", flagPort)
		go func() {
			if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Warn().Err(err).Msg("[mafia] local http stopped")
			}
		}()
	}

	go func() {
		<-ctx.Done()
		if ln != nil {
			_ = ln.Close()
		}
		if client != nil {
			_ = client.Close()
		}
		if httpSrv != nil {
			sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := httpSrv.Shutdown(sctx); err != nil && err != context.Canceled {
				log.Error().Err(err).Msg("[mafia] http server shutdown error")
			}
		}
		mgr.Close()
	}()

	<-ctx.Done()
	log.Info().Msg("[mafia] shutdown complete")
	return nil
}
