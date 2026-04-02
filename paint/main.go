package main

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gosuda/portal/v2/sdk"
	"github.com/gosuda/portal/v2/types"
	"github.com/gosuda/portal/v2/utils"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "paint",
	Short: "collaborative paint",
	RunE:  runPaint,
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
)

func init() {
	flags := rootCmd.PersistentFlags()
	flags.StringVar(&flagServerURLs, "server-url", os.Getenv("RELAY"), "relay base URL(s); repeat or comma-separated (from env RELAY/RELAY_URL if set)")
	flags.BoolVar(&flagDiscovery, "discovery", utils.ResolveBoolEnv(true, "DISCOVERY", "DEFAULT_RELAYS"), "include registry relays and enable relay discovery [env: DISCOVERY, DEFAULT_RELAYS]")
	flags.BoolVar(&flagBanMITM, "ban-mitm", utils.ResolveBoolEnv(false, "BAN_MITM"), "ban relay when MITM self-probe detects TLS termination [env: BAN_MITM]")
	flags.IntVar(&flagPort, "port", -1, "optional local HTTP port (negative to disable)")
	flags.StringVar(&flagName, "name", "paint", "backend display name")
	flags.StringVar(&flagIdentityPath, "identity-path", "identity.json", "optional path to load/save the portal identity")
	flags.BoolVar(&flagHide, "hide", false, "hide this lease from portal listings")
	flags.StringVar(&flagDescription, "description", "Portal demo: collaborative paint", "lease description")
	flags.StringVar(&flagOwner, "owner", "Paint", "lease owner")
	flags.StringVar(&flagTags, "tags", "collab,paint", "comma-separated lease tags")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("execute paint command")
	}
}

func runPaint(cmd *cobra.Command, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	canvas := newCanvas()
	images = newImageStore()
	mux := http.NewServeMux()

	// Serve static files from embedded filesystem
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return fmt.Errorf("create static fs: %w", err)
	}
	// Image upload endpoint (multipart/form-data; field name 'file')
	mux.HandleFunc("/upload-image", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		// limit to 10MB
		r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
		if err := r.ParseMultipartForm(12 << 20); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		f, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "file not found", http.StatusBadRequest)
			return
		}
		defer f.Close()
		buf, err := io.ReadAll(f)
		if err != nil {
			http.Error(w, "read file", http.StatusInternalServerError)
			return
		}
		// determine content-type
		ct := header.Header.Get("Content-Type")
		if ct == "" {
			ct = http.DetectContentType(buf)
		}
		// generate id
		id := fmt.Sprintf("%d", time.Now().UnixNano())
		images.put(id, buf, ct)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fmt.Appendf(nil, `{"id":"%s"}`, id))
	})

	// Serve stored images
	mux.HandleFunc("/images/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/images/")
		if id == "" {
			http.NotFound(w, r)
			return
		}
		if b, ct, ok := images.get(id); ok {
			w.Header().Set("Content-Type", ct)
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(b)
			return
		}
		http.NotFound(w, r)
	})

	mux.Handle("/", http.FileServer(http.FS(staticFS)))
	mux.HandleFunc("/ws", canvas.handleWS)

	exposure, err := sdk.Expose(ctx, sdk.ExposeConfig{
		RelayURLs:    utils.SplitCSV(flagServerURLs),
		BanMITM:      flagBanMITM,
		Discovery:    flagDiscovery,
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
	if exposure != nil {
		defer func() { _ = exposure.Close() }()
	}
	localAddr := ""
	if flagPort >= 0 {
		localAddr = fmt.Sprintf(":%d", flagPort)
	}
	err = exposure.RunHTTP(ctx, mux, localAddr)
	canvas.closeAll()
	canvas.wait()
	if err != nil {
		return err
	}

	log.Info().Msg("[paint] shutdown complete")
	return nil
}
