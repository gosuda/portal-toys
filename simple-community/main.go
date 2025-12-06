package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"gosuda.org/portal/sdk"
)

var rootCmd = &cobra.Command{
	Use:   "simple-community",
	Short: "Portal demo: simple community board",
	RunE:  runCommunity,
}

var (
	flagServerURLs  []string
	flagPort        int
	flagName        string
	flagDBPath      string
	flagHide        bool
	flagDescription string
	flagTags        string
	flagOwner       string
)

func init() {
	flags := rootCmd.PersistentFlags()
	flags.StringSliceVar(&flagServerURLs, "server-url", strings.Split(os.Getenv("RELAY"), ","), "relay websocket URL(s); repeat or comma-separated (from env RELAY/RELAY_URL if set)")
	flags.IntVar(&flagPort, "port", -1, "optional local HTTP port (negative to disable)")
	flags.StringVar(&flagName, "name", "simple-community", "backend display name")
	flags.StringVar(&flagDBPath, "db-path", "simple-community/data", "optional directory for Pebble db")
	flags.BoolVar(&flagHide, "hide", false, "hide this lease from portal listings")
	flags.StringVar(&flagDescription, "description", "Portal demo: simple community board", "lease description")
	flags.StringVar(&flagOwner, "owner", "Community", "lease owner")
	flags.StringVar(&flagTags, "tags", "community", "comma-separated lease tags")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("execute simple community command")
	}
}

func runCommunity(cmd *cobra.Command, args []string) error {
	bootCtx := context.Background()
	if err := InitStore(flagDBPath); err != nil {
		return err
	}
	if err := LoadSnapshot(bootCtx); err != nil {
		log.Warn().Err(err).Msg("[community] failed to bootstrap from local snapshot")
	}

	router := NewHandler()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Relay listener
	cred := sdk.NewCredential()
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
		if err := http.Serve(ln, router); err != nil && err != http.ErrServerClosed && ctx.Err() == nil {
			log.Error().Err(err).Msg("[community] relay http error")
		}
	}()

	// Optional local HTTP on --port
	var httpSrv *http.Server
	if flagPort >= 0 {
		httpSrv = &http.Server{
			Addr:              fmt.Sprintf(":%d", flagPort),
			Handler:           router,
			ReadHeaderTimeout: 5 * time.Second,
			IdleTimeout:       60 * time.Second,
		}
		log.Info().Msgf("[community] serving locally at http://127.0.0.1:%d", flagPort)
		go func() {
			if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Warn().Err(err).Msg("[community] local http stopped")
			}
		}()
	}

	// Shutdown watcher
	go func() {
		<-ctx.Done()
		_ = ln.Close()
		_ = client.Close()
		if httpSrv != nil {
			sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := httpSrv.Shutdown(sctx); err != nil && err != context.Canceled {
				log.Error().Err(err).Msg("[community] http server shutdown error")
			}
		}
	}()

	<-ctx.Done()
	log.Info().Msg("[community] shutdown complete")
	return nil
}
