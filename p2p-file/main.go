package main

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var (
	flagHTTPAddr    string
	flagStorage     string
	flagAgentMode   bool
	flagP2PListen   []string
	flagServerURLs  string
	flagPortalName  string
	flagPortalHide  bool
	flagPortalOwner string
	flagPortalTags  string
	flagPortalDesc  string
	flagCredKey     string
	flagBinaryDist  string
)

var rootCmd = &cobra.Command{
	Use:   "p2p-file",
	Short: "libp2p upload/download playground",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return runService(ctx)
	},
}

func init() {
	flags := rootCmd.PersistentFlags()
	flags.StringVar(&flagHTTPAddr, "listen", "127.0.0.1:8234", "local HTTP listen address (host:port)")
	flags.StringVar(&flagStorage, "storage", "./p2p-data", "directory used to persist files")
	flags.BoolVar(&flagAgentMode, "agent", false, "run as headless libp2p agent (no HTTP UI)")
	flags.StringSliceVar(&flagP2PListen, "p2p-listen", []string{"/ip4/0.0.0.0/tcp/0"}, "libp2p listen multiaddrs (repeatable)")
	flags.StringVar(&flagServerURLs, "server-url", defaultRelayList(), "relayserver base URL(s); repeat or comma-separated (from env PORTAL_RELAY/RELAY/RELAY_URL/SERVER_URL)")
	flags.StringVar(&flagPortalName, "name", "p2p-file", "Portal lease display name")
	flags.BoolVar(&flagPortalHide, "hide", false, "hide this lease from portal listings")
	flags.StringVar(&flagPortalDesc, "description", "Portal libp2p file share", "Portal lease description")
	flags.StringVar(&flagPortalOwner, "owner", "P2P File", "Portal lease owner")
	flags.StringVar(&flagPortalTags, "tags", "p2p,file,libp2p", "comma-separated Portal lease tags")
	flags.StringVar(&flagCredKey, "cred-key", "", "optional credential key for the Portal listener (base64 private key)")
	flags.StringVar(&flagBinaryDist, "binary-dist", "./dist", "directory containing GoReleaser outputs for download")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("execute p2p-file")
	}
}

func runService(ctx context.Context) error {
	store, err := newFileStore(flagStorage)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}

	opts := []libp2p.Option{
		libp2p.ListenAddrStrings(flagP2PListen...),
	}
	p2pHost, err := libp2p.New(opts...)
	if err != nil {
		return fmt.Errorf("libp2p host: %w", err)
	}
	defer func() { _ = p2pHost.Close() }()

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve binary path: %w", err)
	}
	distDir := flagBinaryDist
	if distDir != "" && !filepath.IsAbs(distDir) {
		if abs, err := filepath.Abs(distDir); err == nil {
			distDir = abs
		}
	}
	binaries := loadBinaryArtifacts(distDir)
	if len(binaries) == 0 && distDir != "" {
		log.Warn().Str("dir", distDir).Msg("no GoReleaser binaries discovered; downloads will be unavailable")
	}

	var agent *agentManager
	if !flagAgentMode {
		agent = newAgentManager()
	}

	app := &app{
		host:       p2pHost,
		store:      store,
		binaryPath: exePath,
		agent:      agent,
		binaries:   binaries,
		binaryDist: distDir,
	}

	p2pHost.SetStreamHandler(fileProtocolID, app.handleStream)
	log.Info().Str("peer_id", p2pHost.ID().String()).Strs("multiaddr", multiaddrs(p2pHost)).Msg("libp2p ready")

	if flagAgentMode {
		log.Info().Msg("running in agent mode (libp2p only)")
		<-ctx.Done()
		return nil
	}

	staticFS, err := fs.Sub(embeddedStatic, "static")
	if err != nil {
		return fmt.Errorf("prepare static files: %w", err)
	}

	handler := app.newHTTPHandler(staticFS)
	httpSrv := &http.Server{
		Addr:              flagHTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	errCh := make(chan error, 2)
	portalClose, err := startPortalBridge(ctx, handler, errCh)
	if err != nil {
		return err
	}
	go func() {
		log.Info().Msgf("serving UI at http://%s", flagHTTPAddr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(shutdownCtx); err != nil && err != context.Canceled {
			log.Warn().Err(err).Msg("http shutdown")
		}
		if app.agent != nil {
			if err := app.agent.Stop(); err != nil {
				log.Warn().Err(err).Msg("stop agent")
			}
		}
		if portalClose != nil {
			portalClose()
		}
		return nil
	case err := <-errCh:
		if portalClose != nil {
			portalClose()
		}
		if app.agent != nil {
			if err2 := app.agent.Stop(); err2 != nil {
				log.Warn().Err(err2).Msg("stop agent")
			}
		}
		_ = httpSrv.Close()
		return err
	}
}
