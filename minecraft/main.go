package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gosuda/portal/v2/sdk"
	"github.com/gosuda/portal/v2/types"
	"github.com/gosuda/portal/v2/utils"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "minecraft",
	Short: "Portal Minecraft server proxy with status dashboard",
	RunE:  runMinecraft,
}

var (
	flagServerURLs   string
	flagDiscovery    bool
	flagBanMITM      bool
	flagPort         int
	flagName         string
	flagIdentityPath string
	flagHide         bool
	flagDescription  string
	flagTags         string
	flagOwner        string
	flagMCAddr       string
	flagTCPAddr      string
)

func init() {
	flags := rootCmd.PersistentFlags()
	flags.StringVar(&flagServerURLs, "server-url", getEnv("RELAY", ""), "relay base URL(s); comma-separated (env: RELAY)")
	flags.BoolVar(&flagDiscovery, "discovery", utils.ResolveBoolEnv(true, "DISCOVERY", "DEFAULT_RELAYS"), "enable relay discovery [env: DISCOVERY]")
	flags.BoolVar(&flagBanMITM, "ban-mitm", utils.ResolveBoolEnv(false, "BAN_MITM"), "ban relay on MITM detection [env: BAN_MITM]")
	flags.IntVar(&flagPort, "port", 3000, "local HTTP dashboard port (negative to disable)")
	flags.StringVar(&flagName, "name", getEnv("MC_NAME", "minecraft"), "service display name (env: MC_NAME)")
	flags.StringVar(&flagIdentityPath, "identity-path", "identity.json", "path to load/save portal identity")
	flags.BoolVar(&flagHide, "hide", false, "hide from relay listings")
	flags.StringVar(&flagDescription, "description", getEnv("MC_DESCRIPTION", "Minecraft server via Portal"), "lease description")
	flags.StringVar(&flagTags, "tags", getEnv("MC_TAGS", "game,minecraft"), "comma-separated lease tags")
	flags.StringVar(&flagOwner, "owner", getEnv("MC_OWNER", "Minecraft"), "lease owner")
	flags.StringVar(&flagMCAddr, "mc-addr", getEnv("MC_ADDR", "localhost:25565"), "local Minecraft server address (env: MC_ADDR)")
	flags.StringVar(&flagTCPAddr, "tcp-addr", getEnv("TCP_ADDR", ""), "relay TCP address to display on dashboard (env: TCP_ADDR)")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("execute minecraft command")
	}
}

func runMinecraft(cmd *cobra.Command, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Prepare static files for dashboard
	staticFS, err := fs.Sub(embeddedStatic, "static")
	if err != nil {
		return fmt.Errorf("embed sub FS: %w", err)
	}

	// HTTP dashboard handler
	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", handleStatus)
	mux.Handle("/", http.FileServer(http.FS(staticFS)))

	// Start local HTTP dashboard
	if flagPort >= 0 {
		addr := fmt.Sprintf(":%d", flagPort)
		go func() {
			log.Info().Str("addr", addr).Msg("[minecraft] dashboard started")
			if err := http.ListenAndServe(addr, mux); err != nil && err != http.ErrServerClosed {
				log.Error().Err(err).Msg("[minecraft] dashboard server error")
			}
		}()
	}

	// Expose via portal SDK with TCP port routing
	exposure, err := sdk.Expose(ctx, sdk.ExposeConfig{
		RelayURLs:    utils.SplitCSV(flagServerURLs),
		TCPEnabled:   true,
		TargetAddr:   flagMCAddr,
		Discovery:    flagDiscovery,
		BanMITM:      flagBanMITM,
		Name:         flagName,
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
	defer func() {
		if err := exposure.Close(); err != nil {
			log.Warn().Err(err).Msg("[minecraft] close exposure")
		}
	}()

	log.Info().
		Str("mc_addr", flagMCAddr).
		Str("tcp_addr", flagTCPAddr).
		Strs("relays", exposure.ActiveRelayURLs()).
		Msg("[minecraft] portal tunnel started; proxying MC connections")

	// Close exposure on context cancellation
	go func() {
		<-ctx.Done()
		_ = exposure.Close()
	}()

	// Accept relay connections and proxy to MC server
	var wg sync.WaitGroup
	for {
		conn, err := exposure.Accept()
		if err != nil {
			if ctx.Err() != nil {
				break // graceful shutdown
			}
			log.Error().Err(err).Msg("[minecraft] accept error")
			break
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			proxyToMC(ctx, conn, flagMCAddr)
		}()
	}

	wg.Wait()
	log.Info().Msg("[minecraft] shutdown complete")
	return nil
}

func proxyToMC(ctx context.Context, relayConn net.Conn, mcAddr string) {
	defer relayConn.Close()

	dialer := net.Dialer{Timeout: 5 * time.Second}
	mcConn, err := dialer.DialContext(ctx, "tcp", mcAddr)
	if err != nil {
		log.Warn().Err(err).Str("mc_addr", mcAddr).Msg("[minecraft] dial MC server failed")
		return
	}
	defer mcConn.Close()

	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(mcConn, relayConn)
		close(done)
	}()
	_, _ = io.Copy(relayConn, mcConn)
	<-done
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	resp := map[string]interface{}{
		"tcp_addr": flagTCPAddr,
	}

	status, err := PingServer(flagMCAddr, 3*time.Second)
	if err != nil {
		resp["online"] = false
		resp["error"] = err.Error()
	} else {
		resp["online"] = true
		resp["motd"] = ExtractMOTD(status.Description)
		resp["players"] = status.Players.Online
		resp["max_players"] = status.Players.Max
		resp["version"] = status.Version.Name
		resp["favicon"] = status.Favicon
		resp["error"] = ""
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Error().Err(err).Msg("[minecraft] encode status response")
	}
}
