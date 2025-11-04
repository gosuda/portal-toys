package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"gosuda.org/portal/sdk"
)

//go:embed static
var staticFS embed.FS

var rootCmd = &cobra.Command{
	Use:   "ffmpeg-converter",
	Short: "Simple ffmpeg file converter (upload → convert → download)",
	RunE:  run,
}

var (
	flagServerURL     string
	flagPort          int
	flagName          string
	flagMaxSizeMB     int64
	flagFFmpegWrapper string
)

func init() {
	f := rootCmd.PersistentFlags()
	f.StringVar(&flagServerURL, "server-url", "wss://portal.gosuda.org/relay", "relay websocket URL")
	f.IntVar(&flagPort, "port", -1, "optional local HTTP port")
	f.StringVar(&flagName, "name", "ffmpeg-converter", "display name for relay lease")
	f.Int64Var(&flagMaxSizeMB, "max-mb", 200, "max upload size in MB")
	f.StringVar(&flagFFmpegWrapper, "ffmpeg-wrapper", os.Getenv("FFMPEG_WRAPPER"), "optional command prefix to run ffmpeg (e.g. 'docker exec ffmpeg ffmpeg')")
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

	// Relay
	client, err := sdk.NewClient(func(c *sdk.RDClientConfig) { c.BootstrapServers = []string{flagServerURL} })
	if err != nil {
		return fmt.Errorf("new client: %w", err)
	}
	defer client.Close()
	cred := sdk.NewCredential()
	ln, err := client.Listen(cred, flagName, []string{"http/1.1"})
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	go func() {
		if err := http.Serve(ln, mux); err != nil && err != http.ErrServerClosed && ctx.Err() == nil {
			log.Error().Err(err).Msg("[ffmpeg] relay http serve error")
		}
	}()
	log.Info().Msgf("[ffmpeg] serving over relay; id=%s", cred.ID())

	// Optional local
	var httpSrv *http.Server
	if flagPort >= 0 {
		httpSrv = &http.Server{Addr: fmt.Sprintf(":%d", flagPort), Handler: mux, ReadHeaderTimeout: 5 * time.Second}
		go func() {
			if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Warn().Err(err).Msg("[ffmpeg] local http stopped")
			}
		}()
		log.Info().Msgf("[ffmpeg] local http on http://127.0.0.1:%d", flagPort)
	}

	go func() {
		<-ctx.Done()
		_ = ln.Close()
		if httpSrv != nil {
			sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = httpSrv.Shutdown(sctx)
		}
	}()

	<-ctx.Done()
	log.Info().Msg("[ffmpeg] shutdown complete")
	return nil
}

func handleConvert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Limit body size
	maxBytes := flagMaxSizeMB * 1024 * 1024
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	if err := r.ParseMultipartForm(maxBytes); err != nil {
		http.Error(w, "invalid multipart or too large", http.StatusBadRequest)
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "file missing", http.StatusBadRequest)
		return
	}
	defer file.Close()

	preset := r.FormValue("preset") // e.g., mp4, webm, mp3, wav, gif2mp4, etc
	if preset == "" {
		http.Error(w, "preset required", http.StatusBadRequest)
		return
	}
	// Optional advanced params
	o := convOpts{}
	if v := strings.TrimSpace(r.FormValue("crf")); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			o.CRF = n
		}
	}
	if v := strings.TrimSpace(r.FormValue("fps")); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			o.FPS = n
		}
	}
	if v := strings.TrimSpace(r.FormValue("width")); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			o.Width = n
		}
	}
	if v := strings.TrimSpace(r.FormValue("height")); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			o.Height = n
		}
	}
	if v := strings.TrimSpace(r.FormValue("abitrate")); v != "" {
		o.ABitrate = v
	}
	if v := strings.TrimSpace(r.FormValue("ss")); v != "" {
		o.Start = v
	}
	if v := strings.TrimSpace(r.FormValue("t")); v != "" {
		o.Duration = v
	}
	if v := strings.TrimSpace(r.FormValue("speed")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			o.Speed = f
		}
	}

	// Save to temp
	inTmp, err := os.CreateTemp("", "in-*"+safeExt(hdr.Filename))
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	defer os.Remove(inTmp.Name())
	defer inTmp.Close()
	if _, err := io.Copy(inTmp, file); err != nil {
		http.Error(w, "upload copy error", http.StatusInternalServerError)
		return
	}

	// Determine output path and args
	outExt, args, err := ffmpegArgsForPreset(preset, inTmp.Name(), &o)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	outTmp, err := os.CreateTemp("", "out-*"+outExt)
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	outPath := outTmp.Name()
	outTmp.Close()
	os.Remove(outTmp.Name()) // ffmpeg will create
	defer os.Remove(outPath)

	// Run ffmpeg with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	args = append(args, outPath)
	cmd := buildFFmpegCmd(ctx, args...)
	// Quiet logging
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			http.Error(w, "conversion timed out", http.StatusGatewayTimeout)
			return
		}
		http.Error(w, "ffmpeg failed", http.StatusBadGateway)
		return
	}

	// Send file
	outName := strings.TrimSuffix(filepath.Base(hdr.Filename), filepath.Ext(hdr.Filename)) + outExt
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", outName))
	http.ServeFile(w, r, outPath)
}

type convOpts struct {
	CRF      int
	FPS      int
	Width    int
	Height   int
	ABitrate string
	Start    string
	Duration string
	Speed    float64
}

func ffmpegArgsForPreset(preset, inPath string, o *convOpts) (string, []string, error) {
	if o == nil {
		o = &convOpts{}
	}
	pre := []string{"-y"}
	if o.Start != "" {
		pre = append(pre, "-ss", o.Start)
	}
	pre = append(pre, "-i", inPath)
	if o.Duration != "" {
		pre = append(pre, "-t", o.Duration)
	}

	vf := buildVideoFilter(o)
	af := buildAudioFilter(o)

	switch preset {
	case "mp4":
		crf := 23
		if o.CRF > 0 {
			crf = o.CRF
		}
		args := append([]string{}, pre...)
		if vf != "" {
			args = append(args, "-vf", vf)
		}
		args = append(args, "-movflags", "+faststart", "-c:v", "libx264", "-preset", "veryfast", "-crf", strconv.Itoa(crf))
		if af != "" {
			args = append(args, "-af", af)
		}
		ab := o.ABitrate
		if ab == "" {
			ab = "128k"
		}
		args = append(args, "-c:a", "aac", "-b:a", ab)
		return ".mp4", args, nil
	case "webm":
		crf := 33
		if o.CRF > 0 {
			crf = o.CRF
		}
		args := append([]string{}, pre...)
		if vf != "" {
			args = append(args, "-vf", vf)
		}
		args = append(args, "-c:v", "libvpx-vp9", "-b:v", "0", "-crf", strconv.Itoa(crf), "-c:a", "libopus")
		if af != "" {
			args = append(args, "-af", af)
		}
		return ".webm", args, nil
	case "mp3":
		ab := o.ABitrate
		if ab == "" {
			ab = "192k"
		}
		args := append([]string{}, pre...)
		if af != "" {
			args = append(args, "-af", af)
		}
		args = append(args, "-vn", "-c:a", "libmp3lame", "-b:a", ab)
		return ".mp3", args, nil
	case "wav":
		args := append([]string{}, pre...)
		if af != "" {
			args = append(args, "-af", af)
		}
		args = append(args, "-vn", "-acodec", "pcm_s16le")
		return ".wav", args, nil
	case "m4a":
		ab := o.ABitrate
		if ab == "" {
			ab = "192k"
		}
		args := append([]string{}, pre...)
		if af != "" {
			args = append(args, "-af", af)
		}
		args = append(args, "-vn", "-c:a", "aac", "-b:a", ab)
		return ".m4a", args, nil
	case "flac":
		args := append([]string{}, pre...)
		args = append(args, "-vn", "-c:a", "flac")
		return ".flac", args, nil
	case "mp4-copy":
		args := append([]string{}, pre...)
		args = append(args, "-c", "copy", "-movflags", "+faststart")
		return ".mp4", args, nil
	case "gif2mp4":
		args := append([]string{}, pre...)
		fps := 30
		if o.FPS > 0 {
			fps = o.FPS
		}
		sc := scaleExpr(o)
		vfStr := fmt.Sprintf("fps=%d", fps)
		if sc != "" {
			vfStr += "," + sc
		}
		args = append(args, "-movflags", "+faststart", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-vf", vfStr)
		return ".mp4", args, nil
	case "thumbnail":
		args := append([]string{}, pre...)
		if vf != "" {
			args = append(args, "-vf", vf)
		}
		args = append(args, "-frames:v", "1", "-q:v", "2")
		return ".jpg", args, nil
	case "gif":
		args := append([]string{}, pre...)
		fps := 10
		if o.FPS > 0 {
			fps = o.FPS
		}
		sc := scaleExpr(o)
		vfStr := fmt.Sprintf("fps=%d", fps)
		if sc != "" {
			vfStr += "," + sc
		}
		args = append(args, "-vf", vfStr, "-loop", "0")
		return ".gif", args, nil
	default:
		return "", nil, fmt.Errorf("unsupported preset")
	}
}

func buildVideoFilter(o *convOpts) string {
	var parts []string
	if o != nil {
		if s := scaleExpr(o); s != "" {
			parts = append(parts, s)
		}
		if o.FPS > 0 {
			parts = append(parts, fmt.Sprintf("fps=%d", o.FPS))
		}
		if o.Speed > 0 && o.Speed != 1.0 {
			sp := 1.0 / o.Speed
			parts = append(parts, fmt.Sprintf("setpts=%f*PTS", sp))
		}
	}
	return strings.Join(parts, ",")
}

func buildAudioFilter(o *convOpts) string {
	if o == nil || o.Speed == 0 || o.Speed == 1.0 {
		return ""
	}
	sp := o.Speed
	var parts []string
	for sp > 2.0 {
		parts = append(parts, "atempo=2.0")
		sp /= 2.0
	}
	for sp < 0.5 {
		parts = append(parts, "atempo=0.5")
		sp /= 0.5
	}
	parts = append(parts, fmt.Sprintf("atempo=%g", sp))
	return strings.Join(parts, ",")
}

func scaleExpr(o *convOpts) string {
	if o == nil {
		return ""
	}
	if o.Width > 0 && o.Height > 0 {
		return fmt.Sprintf("scale=%d:%d:flags=lanczos", o.Width, o.Height)
	}
	if o.Width > 0 {
		return fmt.Sprintf("scale=%d:-2:flags=lanczos", o.Width)
	}
	if o.Height > 0 {
		return fmt.Sprintf("scale=-2:%d:flags=lanczos", o.Height)
	}
	return ""
}

func safeExt(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	if len(ext) == 0 || len(ext) > 8 {
		return ""
	}
	for _, r := range ext {
		if !(r == '.' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
			return ""
		}
	}
	return ext
}

func buildFFmpegCmd(ctx context.Context, ffArgs ...string) *exec.Cmd {
	if flagFFmpegWrapper == "" {
		return exec.CommandContext(ctx, "ffmpeg", ffArgs...)
	}
	toks := strings.Fields(flagFFmpegWrapper)
	hasFF := false
	for _, t := range toks {
		if t == "ffmpeg" {
			hasFF = true
			break
		}
	}
	var argv []string
	argv = append(argv, toks...)
	if !hasFF {
		argv = append(argv, "ffmpeg")
	}
	argv = append(argv, ffArgs...)
	prog := argv[0]
	return exec.CommandContext(ctx, prog, argv[1:]...)
}
