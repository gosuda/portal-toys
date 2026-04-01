package main

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/gosuda/portal/v2/sdk"
	"github.com/gosuda/portal/v2/types"
	"github.com/gosuda/portal/v2/utils"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "ffmpeg-converter",
	Short: "Simple ffmpeg file converter (upload ??convert ??download)",
	RunE:  run,
}

var (
	flagServerURLs    string
	flagDiscovery     bool
	flagBanMITM       bool
	flagPort          int
	flagName          string
	flagMaxSizeMB     int64
	flagFFmpegWrapper string
	flagHide          bool
	flagDescription   string
	flagTags          string
	flagOwner         string
)

func init() {
	f := rootCmd.PersistentFlags()
	f.StringVar(&flagServerURLs, "server-url", os.Getenv("RELAY"), "relay base URL(s); repeat or comma-separated (from env RELAY/RELAY_URL if set)")
	f.BoolVar(&flagDiscovery, "discovery", utils.ResolveBoolEnv(true, "DISCOVERY", "DEFAULT_RELAYS"), "include registry relays and enable relay discovery [env: DISCOVERY, DEFAULT_RELAYS]")
	f.BoolVar(&flagBanMITM, "ban-mitm", utils.ResolveBoolEnv(false, "BAN_MITM"), "ban relay when MITM self-probe detects TLS termination [env: BAN_MITM]")
	f.IntVar(&flagPort, "port", -1, "optional local HTTP port")
	f.StringVar(&flagName, "name", "ffmpeg-converter", "display name for relay lease")
	f.Int64Var(&flagMaxSizeMB, "max-mb", 200, "max upload size in MB")
	f.StringVar(&flagFFmpegWrapper, "ffmpeg-wrapper", os.Getenv("FFMPEG_WRAPPER"), "optional command prefix to run ffmpeg (e.g. 'docker exec ffmpeg ffmpeg')")
	f.BoolVar(&flagHide, "hide", false, "hide this lease from portal listings")
	f.StringVar(&flagDescription, "description", "Simple ffmpeg file converter (upload ??convert ??download)", "lease description")
	f.StringVar(&flagOwner, "owner", "FFmpeg Converter", "lease owner")
	f.StringVar(&flagTags, "tags", "media,ffmpeg", "comma-separated lease tags")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("execute command")
	}
}

func run(cmd *cobra.Command, args []string) error {
	// Verify ffmpeg exists (best effort); if wrapper provided, skip direct ffmpeg check
	if flagFFmpegWrapper == "" {
		if _, err := exec.LookPath("ffmpeg"); err != nil {
			log.Warn().Msg("ffmpeg not found in PATH. Conversions may fail until installed or --ffmpeg-wrapper is set.")
		}
	}

	// Shutdown context
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Router
	mux := http.NewServeMux()
	// Static UI
	ui, _ := fs.Sub(staticFS, "static")
	mux.Handle("/", http.FileServer(http.FS(ui)))
	// API
	mux.HandleFunc("/convert", handleConvert)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	exposure, err := sdk.Expose(ctx, sdk.ExposeConfig{
		RelayURLs: utils.SplitCSV(flagServerURLs),
		BanMITM:   flagBanMITM,
		Discovery: flagDiscovery,
		Identity:  types.Identity{Name: flagName},
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
		localAddr = fmt.Sprintf(":%d", flagPort)
	}
	if err := exposure.RunHTTP(ctx, mux, localAddr); err != nil {
		return err
	}
	log.Info().Msg("[ffmpeg] shutdown complete")
	return nil
}
