package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"gosuda.org/portal/portal/core/cryptoops"
	"gosuda.org/portal/sdk"

	libp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/multiformats/go-multiaddr"
)

const (
	fileProtocolID = protocol.ID("/portal/p2p-file/1.0.0")
	requestTimeout = 45 * time.Second
)

var (
	flagHTTPAddr    string
	flagStorage     string
	flagAgentMode   bool
	flagP2PListen   []string
	flagServerURLs  []string
	flagPortalName  string
	flagPortalHide  bool
	flagPortalOwner string
	flagPortalTags  string
	flagPortalDesc  string
	flagCredKey     string
	flagBinaryDist  string
)

func defaultRelayList() []string {
	for _, key := range []string{"PORTAL_RELAY", "RELAY", "RELAY_URL", "SERVER_URL"} {
		val := strings.TrimSpace(os.Getenv(key))
		if val != "" {
			return strings.Split(val, ",")
		}
	}
	return nil
}

//go:embed static/*
var embeddedStatic embed.FS

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
	flags.StringSliceVar(&flagServerURLs, "server-url", defaultRelayList(), "relayserver base URL(s); repeat or comma-separated (from env PORTAL_RELAY/RELAY/RELAY_URL/SERVER_URL)")
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
	portalClose, err := startPortalBridge(handler, errCh)
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

type app struct {
	host       host.Host
	store      *fileStore
	binaryPath string
	agent      *agentManager
	binaries   []binaryArtifact
	binaryDist string
}

func (a *app) newHTTPHandler(staticFS fs.FS) http.Handler {
	r := chi.NewRouter()

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	r.Get("/api/info", a.handleInfo)
	r.Get("/api/files", a.handleFiles)
	r.Post("/api/upload", a.handleUpload)
	r.Get("/download/{id}", a.handleDownload)
	r.Post("/api/connect", a.handleConnect)
	r.Post("/api/request", a.handleRequestFile)
	r.Post("/api/list-remote", a.handleListRemote)
	r.Post("/api/launch", a.handleLaunchAgent)
	r.Post("/api/stop-agent", a.handleStopAgent)
	r.Get("/binary", a.handleBinaryDownload)

	r.Get("/", serveEmbedded(staticFS, "index.html", "text/html; charset=utf-8"))
	r.Get("/app.js", serveEmbedded(staticFS, "app.js", "application/javascript"))
	r.Get("/styles.css", serveEmbedded(staticFS, "styles.css", "text/css; charset=utf-8"))
	if a.binaryDist != "" {
		if info, err := os.Stat(a.binaryDist); err == nil && info.IsDir() {
			fsHandler := http.StripPrefix("/dist/", http.FileServer(http.Dir(a.binaryDist)))
			r.Handle("/dist/*", fsHandler)
		}
	}

	return r
}

func (a *app) handleInfo(w http.ResponseWriter, r *http.Request) {
	running, pid := false, 0
	if a.agent != nil {
		running, pid = a.agent.Status()
	}
	var binarySize int64
	if info, err := os.Stat(a.binaryPath); err == nil {
		binarySize = info.Size()
	}
	storagePath, _ := filepath.Abs(a.store.dir)
	serverURLs := cleanServerURLs(flagServerURLs)
	resp := map[string]interface{}{
		"peerId":       a.host.ID().String(),
		"addresses":    multiaddrs(a.host),
		"files":        a.store.List(),
		"agentRunning": running,
		"agentPid":     pid,
		"storageDir":   storagePath,
		"binarySize":   binarySize,
		"serverUrls":   serverURLs,
		"portalActive": len(serverURLs) > 0,
		"binaries":     a.binaries,
		"binaryDist":   a.binaryDist,
	}
	respondJSON(w, http.StatusOK, resp)
}

func (a *app) handleFiles(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"files": a.store.List(),
	})
}

func (a *app) handleUpload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(256 << 20); err != nil {
		respondError(w, http.StatusBadRequest, fmt.Errorf("parse form: %w", err))
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		respondError(w, http.StatusBadRequest, fmt.Errorf("missing file: %w", err))
		return
	}
	defer func() { _ = file.Close() }()
	meta, err := a.store.Save(header.Filename, file, "upload")
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"file": meta.Public(),
	})
}

func (a *app) handleDownload(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	reader, meta, err := a.store.Open(id)
	if err != nil {
		status := http.StatusNotFound
		if !errors.Is(err, errFileNotFound) {
			status = http.StatusInternalServerError
		}
		respondError(w, status, err)
		return
	}
	defer func() { _ = reader.Close() }()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", meta.Name))
	http.ServeContent(w, r, meta.Name, meta.AddedAt, reader)
}

func (a *app) handleConnect(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Multiaddr string `json:"multiaddr"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, fmt.Errorf("decode body: %w", err))
		return
	}
	info, err := parseAddrInfo(req.Multiaddr)
	if err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	if err := a.host.Connect(ctx, *info); err != nil {
		respondError(w, http.StatusBadGateway, fmt.Errorf("connect peer: %w", err))
		return
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"connected": info.ID.String(),
	})
}

func (a *app) handleRequestFile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Multiaddr string `json:"multiaddr"`
		FileID    string `json:"fileId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, fmt.Errorf("decode body: %w", err))
		return
	}
	if req.FileID == "" {
		respondError(w, http.StatusBadRequest, errors.New("fileId required"))
		return
	}
	meta, err := a.fetchRemoteFile(r.Context(), req.Multiaddr, req.FileID)
	if err != nil {
		respondError(w, http.StatusBadGateway, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"file": meta.Public(),
	})
}

func (a *app) handleListRemote(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Multiaddr string `json:"multiaddr"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, fmt.Errorf("decode body: %w", err))
		return
	}
	files, err := a.fetchRemoteList(r.Context(), req.Multiaddr)
	if err != nil {
		respondError(w, http.StatusBadGateway, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"files": files,
	})
}

func (a *app) handleLaunchAgent(w http.ResponseWriter, r *http.Request) {
	if a.agent == nil {
		respondError(w, http.StatusBadRequest, errors.New("agent mode only available from UI binary"))
		return
	}
	if err := a.agent.Launch(a.store.dir); err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"status": "launched",
	})
}

func (a *app) handleStopAgent(w http.ResponseWriter, r *http.Request) {
	if a.agent == nil {
		respondError(w, http.StatusBadRequest, errors.New("agent manager disabled"))
		return
	}
	if err := a.agent.Stop(); err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"status": "stopped",
	})
}

func (a *app) handleBinaryDownload(w http.ResponseWriter, r *http.Request) {
	if len(a.binaries) == 0 {
		respondError(w, http.StatusNotFound, errors.New("no GoReleaser binaries available on server"))
		return
	}
	query := r.URL.Query()
	osName := query.Get("os")
	arch := query.Get("arch")
	if osName == "" || arch == "" {
		osName, arch = detectPlatformFromUA(r.UserAgent())
	}
	artifact, ok := a.findBinary(osName, arch)
	if !ok {
		respondError(w, http.StatusNotFound, fmt.Errorf("binary for %s/%s not found", osName, arch))
		return
	}
	file, err := os.Open(artifact.path)
	if err != nil {
		respondError(w, http.StatusInternalServerError, fmt.Errorf("open binary: %w", err))
		return
	}
	defer func() { _ = file.Close() }()
	info, _ := file.Stat()
	name := artifact.File
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	modTime := time.Now()
	if info != nil {
		modTime = info.ModTime()
		w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))
	}
	http.ServeContent(w, r, name, modTime, file)
}

func (a *app) handleStream(stream network.Stream) {
	defer func() { _ = stream.Close() }()
	var req p2pRequest
	if err := json.NewDecoder(stream).Decode(&req); err != nil {
		sendStreamError(stream, fmt.Errorf("decode request: %w", err))
		return
	}
	switch req.Type {
	case "fetch":
		a.streamSendFile(stream, req.FileID)
	case "list":
		a.streamSendList(stream)
	default:
		sendStreamError(stream, fmt.Errorf("unsupported request %q", req.Type))
	}
}

func (a *app) streamSendList(stream network.Stream) {
	resp := p2pResponse{
		OK:    true,
		Files: a.store.List(),
	}
	if err := json.NewEncoder(stream).Encode(resp); err != nil {
		log.Warn().Err(err).Msg("send list response")
	}
}

func (a *app) streamSendFile(stream network.Stream, id string) {
	reader, meta, err := a.store.Open(id)
	if err != nil {
		sendStreamError(stream, err)
		return
	}
	defer func() { _ = reader.Close() }()
	resp := p2pResponse{
		OK:       true,
		FileName: meta.Name,
		Size:     meta.Size,
	}
	if err := json.NewEncoder(stream).Encode(resp); err != nil {
		log.Warn().Err(err).Msg("send file header")
		return
	}
	if _, err := io.Copy(stream, reader); err != nil {
		log.Warn().Err(err).Msg("stream file body")
	}
}

func (a *app) fetchRemoteFile(ctx context.Context, addr, fileID string) (FileMeta, error) {
	info, err := parseAddrInfo(addr)
	if err != nil {
		return FileMeta{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	if err := a.host.Connect(ctx, *info); err != nil {
		return FileMeta{}, fmt.Errorf("connect peer: %w", err)
	}
	stream, err := a.host.NewStream(ctx, info.ID, fileProtocolID)
	if err != nil {
		return FileMeta{}, fmt.Errorf("open stream: %w", err)
	}
	defer func() { _ = stream.Close() }()
	req := p2pRequest{Type: "fetch", FileID: fileID}
	if err := json.NewEncoder(stream).Encode(req); err != nil {
		return FileMeta{}, fmt.Errorf("send request: %w", err)
	}
	var resp p2pResponse
	if err := json.NewDecoder(stream).Decode(&resp); err != nil {
		return FileMeta{}, fmt.Errorf("read response: %w", err)
	}
	if !resp.OK {
		if resp.Error == "" {
			resp.Error = "remote rejected request"
		}
		return FileMeta{}, errors.New(resp.Error)
	}
	reader := io.Reader(stream)
	if resp.Size > 0 {
		reader = io.LimitReader(stream, resp.Size)
	}
	meta, err := a.store.Save(resp.FileName, reader, fmt.Sprintf("p2p:%s", info.ID))
	if err != nil {
		return FileMeta{}, fmt.Errorf("save file: %w", err)
	}
	return meta, nil
}

func (a *app) fetchRemoteList(ctx context.Context, addr string) ([]FileInfo, error) {
	info, err := parseAddrInfo(addr)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	if err := a.host.Connect(ctx, *info); err != nil {
		return nil, fmt.Errorf("connect peer: %w", err)
	}
	stream, err := a.host.NewStream(ctx, info.ID, fileProtocolID)
	if err != nil {
		return nil, fmt.Errorf("open stream: %w", err)
	}
	defer func() { _ = stream.Close() }()
	req := p2pRequest{Type: "list"}
	if err := json.NewEncoder(stream).Encode(req); err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	var resp p2pResponse
	if err := json.NewDecoder(stream).Decode(&resp); err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if !resp.OK {
		if resp.Error == "" {
			resp.Error = "remote rejected request"
		}
		return nil, errors.New(resp.Error)
	}
	return resp.Files, nil
}

type p2pRequest struct {
	Type   string `json:"type"`
	FileID string `json:"fileId,omitempty"`
}

type p2pResponse struct {
	OK       bool       `json:"ok"`
	Error    string     `json:"error,omitempty"`
	Files    []FileInfo `json:"files,omitempty"`
	FileName string     `json:"fileName,omitempty"`
	Size     int64      `json:"size,omitempty"`
}

func sendStreamError(stream network.Stream, err error) {
	_ = json.NewEncoder(stream).Encode(p2pResponse{OK: false, Error: err.Error()})
}

func respondJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Warn().Err(err).Msg("encode json response")
	}
}

func respondError(w http.ResponseWriter, status int, err error) {
	if err == nil {
		err = errors.New("unknown error")
	}
	respondJSON(w, status, map[string]string{"error": err.Error()})
}

func serveEmbedded(fsys fs.FS, name, contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				http.NotFound(w, r)
				return
			}
			respondError(w, http.StatusInternalServerError, err)
			return
		}
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		http.ServeContent(w, r, name, time.Now(), bytes.NewReader(data))
	}
}

func cleanServerURLs(in []string) []string {
	out := make([]string, 0, len(in))
	for _, raw := range in {
		if raw == "" {
			continue
		}
		for _, part := range strings.Split(raw, ",") {
			p := strings.TrimSpace(part)
			if p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

func portalTags() []string {
	tokens := strings.Split(flagPortalTags, ",")
	out := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		if t := strings.TrimSpace(tok); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func startPortalBridge(handler http.Handler, errCh chan<- error) (func(), error) {
	serverURLs := cleanServerURLs(flagServerURLs)
	if len(serverURLs) == 0 {
		return nil, nil
	}
	cred := sdk.NewCredential()
	if flagCredKey != "" {
		key, err := base64.StdEncoding.DecodeString(flagCredKey)
		if err != nil {
			return nil, fmt.Errorf("decode cred key: %w", err)
		}
		cred2, err := cryptoops.NewCredentialFromPrivateKey(key)
		if err != nil {
			return nil, fmt.Errorf("credential from key: %w", err)
		}
		cred = cred2
	}
	client, err := sdk.NewClient(func(c *sdk.RDClientConfig) {
		c.BootstrapServers = serverURLs
	})
	if err != nil {
		return nil, fmt.Errorf("portal client: %w", err)
	}
	ln, err := client.Listen(cred, flagPortalName, []string{"http/1.1"},
		sdk.WithDescription(flagPortalDesc),
		sdk.WithHide(flagPortalHide),
		sdk.WithOwner(flagPortalOwner),
		sdk.WithTags(portalTags()),
	)
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("portal listen: %w", err)
	}
	log.Info().
		Str("name", flagPortalName).
		Strs("servers", serverURLs).
		Msg("serving Portal relay")
	go func() {
		if err := http.Serve(ln, handler); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("portal http serve: %w", err)
		}
	}()
	return func() {
		_ = ln.Close()
		_ = client.Close()
	}, nil
}

func multiaddrs(h host.Host) []string {
	raw := h.Addrs()
	addrs := make([]string, 0, len(raw))
	for _, addr := range raw {
		addrs = append(addrs, fmt.Sprintf("%s/p2p/%s", addr.String(), h.ID().String()))
	}
	sort.Strings(addrs)
	return addrs
}

func parseAddrInfo(raw string) (*peer.AddrInfo, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("multiaddr required")
	}
	if strings.Contains(raw, "/p2p/") {
		ma, err := multiaddr.NewMultiaddr(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid multiaddr: %w", err)
		}
		info, err := peer.AddrInfoFromP2pAddr(ma)
		if err != nil {
			return nil, fmt.Errorf("addr info: %w", err)
		}
		return info, nil
	}
	info, err := peer.AddrInfoFromString(raw)
	if err != nil {
		return nil, fmt.Errorf("addr info: %w", err)
	}
	return info, nil
}
