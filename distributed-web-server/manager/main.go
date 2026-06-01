package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gabriel-vasile/mimetype"
	"github.com/gosuda/portal-tunnel/v2/sdk"
	"github.com/gosuda/portal-tunnel/v2/types"
	"github.com/gosuda/portal-tunnel/v2/utils"
	"github.com/joho/godotenv"
	"github.com/microcosm-cc/bluemonday"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
	"github.com/spf13/cobra"
	"github.com/vmihailenco/msgpack/v5"
)

const (
	defaultFlushInterval = 10 * time.Millisecond
	defaultFlushBytes    = 1 << 20 // 1MB
	maxPayloadSize       = 4 << 20 // 4MB
	defaultMaxInflight   = 256
	defaultLimiterWait   = 100 * time.Millisecond
)

var rootCmd = &cobra.Command{
	Use:   "distributed-manager",
	Short: "Portal-ready distributed web server manager",
	RunE:  runManagerCmd,
}

var (
	flagServerURLs   string
	flagDiscovery    bool
	flagBanMITM      bool
	flagPort         int
	flagName         string
	flagIdentityPath string
	flagDescription  string
	flagOwner        string
	flagTags         string
	flagHide         bool
	flagDisableRelay bool
)

var harmfulPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(union\s+select|drop\s+table|;--|insert\s+into)`),
	regexp.MustCompile(`(?i)(<script|onerror\s*=|javascript:)`),
	regexp.MustCompile(`(?i)(\.\./|\.\.)`),
}

func init() {
	_ = godotenv.Load()
	flags := rootCmd.PersistentFlags()
	flags.StringVar(&flagServerURLs, "server-url", getEnv("RELAY", ""), "relay base URL(s); repeat or comma-separated (env RELAY)")
	flags.BoolVar(&flagDiscovery, "discovery", ResolveBoolEnv(true, "DISCOVERY", "DEFAULT_RELAYS"), "include registry relays and enable discovery [env: DISCOVERY, DEFAULT_RELAYS]")
	flags.BoolVar(&flagBanMITM, "ban-mitm", ResolveBoolEnv(false, "BAN_MITM"), "ban relay when MITM self-probe detects TLS termination [env: BAN_MITM]")
	flags.IntVar(&flagPort, "port", parseIntEnv("MANAGER_PORT", 8080), "local HTTP port for the manager + portal binding [env: MANAGER_PORT]")
	flags.StringVar(&flagName, "name", getEnv("MANAGER_NAME", "distributed-web-manager"), "lease display name [env: MANAGER_NAME]")
	flags.StringVar(&flagIdentityPath, "identity-path", "identity.json", "optional path to load/save the portal identity")
	flags.StringVar(&flagDescription, "description", getEnv("MANAGER_DESCRIPTION", "High-performance distributed web manager"), "lease description [env: MANAGER_DESCRIPTION]")
	flags.StringVar(&flagOwner, "owner", getEnv("MANAGER_OWNER", "Distributed Manager"), "lease owner label [env: MANAGER_OWNER]")
	flags.StringVar(&flagTags, "tags", getEnv("MANAGER_TAGS", "distributed,worker,manager"), "comma-separated tags [env: MANAGER_TAGS]")
	flags.BoolVar(&flagHide, "hide", ResolveBoolEnv(false, "MANAGER_HIDE"), "hide lease from listings [env: MANAGER_HIDE]")
	flags.BoolVar(&flagDisableRelay, "disable-relay", ResolveBoolEnv(false, "MANAGER_DISABLE_RELAY"), "skip portal registration; serve local HTTP only [env: MANAGER_DISABLE_RELAY]")
}

func ResolveBoolEnv(fallback bool, envNames ...string) bool {
	for _, envName := range envNames {
		raw := strings.TrimSpace(os.Getenv(envName))
		if raw == "" {
			continue
		}
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			return fallback
		}
		return parsed
	}
	return fallback
}

type workerMetrics struct {
	CPUPercent  float64   `json:"cpuPercent"`
	MemoryBytes uint64    `json:"memoryBytes"`
	NetBytes    uint64    `json:"netBytes"`
	ActiveJobs  int64     `json:"activeJobs"`
	Timestamp   time.Time `json:"timestamp"`
}

type localObservation struct {
	CPUPercent  float64   `json:"managerCpu"`
	MemoryBytes uint64    `json:"managerMemory"`
	NetBytes    uint64    `json:"managerNet"`
	Timestamp   time.Time `json:"timestamp"`
}

type workerSnapshot struct {
	ID       string           `json:"id"`
	Endpoint string           `json:"endpoint"`
	Metrics  workerMetrics    `json:"metrics"`
	Observed localObservation `json:"observed"`
	Load     int64            `json:"load"`
}

type workerObject struct {
	ID        string
	Endpoint  string
	metrics   workerMetrics
	observed  localObservation
	loadScore int64
	lastShift time.Time
	mu        sync.RWMutex
}

type concurrencyLimiter struct {
	sem  chan struct{}
	wait time.Duration
}

func newConcurrencyLimiter(limit int, wait time.Duration) *concurrencyLimiter {
	if limit <= 0 {
		return nil
	}
	if wait <= 0 {
		wait = defaultLimiterWait
	}
	return &concurrencyLimiter{
		sem:  make(chan struct{}, limit),
		wait: wait,
	}
}

func (c *concurrencyLimiter) Acquire(ctx context.Context) bool {
	if c == nil {
		return true
	}
	select {
	case c.sem <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	case <-time.After(c.wait):
		return false
	}
}

func (c *concurrencyLimiter) Release() {
	if c == nil {
		return
	}
	select {
	case <-c.sem:
	default:
	}
}

type cpuShield struct {
	limit   float64
	current atomic.Uint32
}

func newCPUShield(limit float64) *cpuShield {
	if limit <= 0 {
		return nil
	}
	return &cpuShield{limit: limit}
}

func (c *cpuShield) Start(ctx context.Context) {
	if c == nil {
		return
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			percent, err := cpu.Percent(200*time.Millisecond, false)
			if err != nil || len(percent) == 0 {
				continue
			}
			scaled := uint32(percent[0] * 100)
			c.current.Store(scaled)
		}
	}
}

func (c *cpuShield) OverLimit() bool {
	if c == nil || c.limit <= 0 {
		return false
	}
	curr := float64(c.current.Load()) / 100
	return curr >= c.limit
}

func (w *workerObject) snapshot() workerSnapshot {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return workerSnapshot{
		ID:       w.ID,
		Endpoint: w.Endpoint,
		Metrics:  w.metrics,
		Observed: w.observed,
		Load:     w.loadScore,
	}
}

func (w *workerObject) updateMetrics(metrics workerMetrics) {
	w.mu.Lock()
	w.metrics = metrics
	w.mu.Unlock()
}

func (w *workerObject) updateObservation(obs localObservation) {
	w.mu.Lock()
	w.observed = obs
	w.mu.Unlock()
}

func (w *workerObject) load() int64 {
	return atomic.LoadInt64(&w.loadScore)
}

func (w *workerObject) addLoad(delta int64) {
	atomic.AddInt64(&w.loadScore, delta)
}

type heartbeatResponse struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
}

type peerState struct {
	Endpoint string    `json:"endpoint"`
	ID       string    `json:"id"`
	LastSeen time.Time `json:"lastSeen"`
}

type controlState struct {
	ManagerID string      `json:"managerId"`
	LeaderID  string      `json:"leaderId"`
	Peers     []peerState `json:"peers"`
	IsLeader  bool        `json:"isLeader"`
	Timestamp time.Time   `json:"timestamp"`
}

type controlPlane struct {
	id     string
	peers  []string
	logger zerolog.Logger
	client *http.Client
	mu     sync.RWMutex
	states map[string]peerState
	leader atomic.Value
}

func newControlPlane(id string, peers []string, logger zerolog.Logger) *controlPlane {
	cp := &controlPlane{
		id:     id,
		peers:  peers,
		logger: logger,
		client: &http.Client{Timeout: 1500 * time.Millisecond},
		states: make(map[string]peerState),
	}
	cp.leader.Store(id)
	return cp
}

func (c *controlPlane) run(ctx context.Context) {
	if len(c.peers) == 0 {
		<-ctx.Done()
		return
	}
	c.pollPeers(ctx)
	c.recomputeLeader()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.pollPeers(ctx)
			c.recomputeLeader()
		}
	}
}

func (c *controlPlane) pollPeers(ctx context.Context) {
	for _, endpoint := range c.peers {
		c.pingPeer(ctx, endpoint)
	}
}

func (c *controlPlane) pingPeer(parent context.Context, endpoint string) {
	reqCtx, cancel := context.WithTimeout(parent, 1*time.Second)
	defer cancel()
	url := strings.TrimSuffix(endpoint, "/") + "/control/heartbeat"
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		c.logger.Err(err).Msgf("control-plane heartbeat request build failed for %s", endpoint)
		return
	}
	resp, err := c.client.Do(req)
	if err != nil {
		c.markPeerDead(endpoint)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		c.markPeerDead(endpoint)
		return
	}
	var hb heartbeatResponse
	if err := json.NewDecoder(resp.Body).Decode(&hb); err != nil {
		c.logger.Err(err).Msgf("control-plane heartbeat decode failed for %s", endpoint)
		c.markPeerDead(endpoint)
		return
	}
	c.markPeerAlive(endpoint, hb.ID)
}

func (c *controlPlane) markPeerAlive(endpoint, id string) {
	c.mu.Lock()
	c.states[endpoint] = peerState{
		Endpoint: endpoint,
		ID:       id,
		LastSeen: time.Now(),
	}
	c.mu.Unlock()
}

func (c *controlPlane) markPeerDead(endpoint string) {
	c.mu.Lock()
	if st, ok := c.states[endpoint]; ok {
		st.LastSeen = time.Time{}
		c.states[endpoint] = st
	} else {
		c.states[endpoint] = peerState{Endpoint: endpoint}
	}
	c.mu.Unlock()
}

func (c *controlPlane) recomputeLeader() {
	c.mu.RLock()
	expiry := time.Now().Add(-5 * time.Second)
	candidates := []string{c.id}
	for _, st := range c.states {
		if st.ID == "" {
			continue
		}
		if st.LastSeen.IsZero() || st.LastSeen.Before(expiry) {
			continue
		}
		candidates = append(candidates, st.ID)
	}
	c.mu.RUnlock()

	sort.Strings(candidates)
	if len(candidates) == 0 {
		candidates = []string{c.id}
	}
	newLeader := candidates[0]

	current, _ := c.leader.Load().(string)
	if current != newLeader {
		c.logger.Info().Msgf("control-plane leader moved from %s to %s", current, newLeader)
		c.leader.Store(newLeader)
	}
}

func (c *controlPlane) LeaderID() string {
	if val, ok := c.leader.Load().(string); ok && val != "" {
		return val
	}
	return c.id
}

func (c *controlPlane) IsLeader() bool {
	return c.LeaderID() == c.id
}

func (c *controlPlane) snapshot() controlState {
	c.mu.RLock()
	peers := make([]peerState, 0, len(c.states))
	for _, st := range c.states {
		peers = append(peers, st)
	}
	c.mu.RUnlock()
	sort.Slice(peers, func(i, j int) bool {
		return peers[i].Endpoint < peers[j].Endpoint
	})
	return controlState{
		ManagerID: c.id,
		LeaderID:  c.LeaderID(),
		Peers:     peers,
		IsLeader:  c.IsLeader(),
		Timestamp: time.Now(),
	}
}

// olsScheduler produces an index stream based on an orthogonal Latin square matrix.
type olsScheduler struct {
	sequence []int
	order    int
	idx      uint32
}

func newOLSScheduler(order int) *olsScheduler {
	size := order * order
	seq := make([]int, 0, size)
	for row := 0; row < order; row++ {
		for col := 0; col < order; col++ {
			a := (row + col) % order
			b := (row + 2*col) % order
			seq = append(seq, a*order+b)
		}
	}
	return &olsScheduler{sequence: seq, order: order}
}

func (o *olsScheduler) next(total int) int {
	if total == 0 {
		return 0
	}
	i := atomic.AddUint32(&o.idx, 1)
	return o.sequence[int(i)%len(o.sequence)] % total
}

func (o *olsScheduler) rotate() {
	atomic.AddUint32(&o.idx, uint32(o.order))
}

type batchEntry struct {
	payload []byte
	resultC chan []byte
	errC    chan error
}

type workerRequest struct {
	MIME     string   `msgpack:"mime"`
	Payloads [][]byte `msgpack:"payloads"`
}

type bucketKey struct {
	mime   string
	bucket int
}

var windowPool = sync.Pool{
	New: func() any {
		return &batchWindow{
			entries: make([]*batchEntry, 0, 32),
		}
	},
}

type batchWindow struct {
	entries   []*batchEntry
	totalSize int
	timer     *time.Timer
}

type batchDispatcher interface {
	dispatch(ctx context.Context, jobs []*batchEntry, mimeType string) ([][]byte, error)
}

type mimeBatcher struct {
	mu            sync.Mutex
	windows       map[bucketKey]*batchWindow
	dispatcher    batchDispatcher
	flushBytes    int
	flushInterval time.Duration
}

func newMIMEBatcher(dispatcher batchDispatcher) *mimeBatcher {
	return &mimeBatcher{
		windows:       make(map[bucketKey]*batchWindow),
		dispatcher:    dispatcher,
		flushBytes:    defaultFlushBytes,
		flushInterval: defaultFlushInterval,
	}
}

func (b *mimeBatcher) key(mime string, size int) bucketKey {
	return bucketKey{mime: mime, bucket: size >> 10}
}

func (b *mimeBatcher) submit(ctx context.Context, mime string, payload []byte) ([]byte, error) {
	entry := &batchEntry{payload: payload, resultC: make(chan []byte, 1), errC: make(chan error, 1)}
	key := b.key(mime, len(payload))

	b.mu.Lock()
	win := b.windows[key]
	if win == nil {
		win = windowPool.Get().(*batchWindow)
		win.entries = win.entries[:0]
		win.totalSize = 0
		win.timer = time.AfterFunc(b.flushInterval, func() {
			b.flush(key)
		})
		b.windows[key] = win
	}
	win.entries = append(win.entries, entry)
	win.totalSize += len(payload)
	shouldFlush := win.totalSize >= b.flushBytes
	b.mu.Unlock()

	if shouldFlush {
		go b.flush(key)
	}

	select {
	case res := <-entry.resultC:
		return res, nil
	case err := <-entry.errC:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (b *mimeBatcher) flush(key bucketKey) {
	b.mu.Lock()
	win := b.windows[key]
	if win == nil {
		b.mu.Unlock()
		return
	}
	delete(b.windows, key)
	if win.timer != nil {
		win.timer.Stop()
		win.timer = nil
	}
	entries := win.entries
	win.entries = win.entries[:0]
	win.totalSize = 0
	b.mu.Unlock()

	if len(entries) == 0 {
		windowPool.Put(win)
		return
	}

	mimeType := key.mime
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	results, err := b.dispatcher.dispatch(ctx, entries, mimeType)
	if err != nil {
		for _, e := range entries {
			e.errC <- err
		}
		windowPool.Put(win)
		return
	}

	for i, e := range entries {
		if i < len(results) {
			e.resultC <- results[i]
		} else {
			e.resultC <- []byte{}
		}
	}
	windowPool.Put(win)
}

type workerDispatcher struct {
	logger    zerolog.Logger
	workers   []*workerObject
	scheduler *olsScheduler
	client    *http.Client
}

func newWorkerDispatcher(logger zerolog.Logger, workers []*workerObject, scheduler *olsScheduler) *workerDispatcher {
	transport := &http.Transport{
		MaxIdleConns:        128,
		MaxIdleConnsPerHost: 32,
		IdleConnTimeout:     30 * time.Second,
	}
	return &workerDispatcher{
		logger:    logger,
		workers:   workers,
		scheduler: scheduler,
		client: &http.Client{
			Timeout:   8 * time.Second,
			Transport: transport,
		},
	}
}

func (d *workerDispatcher) dispatch(ctx context.Context, jobs []*batchEntry, mimeType string) ([][]byte, error) {
	if len(d.workers) == 0 {
		return nil, errors.New("no workers configured")
	}
	idx := d.scheduler.next(len(d.workers))
	worker := d.workers[idx]
	worker.addLoad(int64(len(jobs)))
	defer worker.addLoad(-int64(len(jobs)))

	payloads := make([][]byte, len(jobs))
	for i, job := range jobs {
		payloads[i] = job.payload
	}

	reqBody, err := msgpack.Marshal(&workerRequest{MIME: mimeType, Payloads: payloads})
	if err != nil {
		return nil, err
	}

	endpoint := strings.TrimSuffix(worker.Endpoint, "/") + "/invoke"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/msgpack")

	resp, err := d.client.Do(request)
	if err != nil {
		d.logger.Error().Err(err).Msgf("worker %s unreachable, rotating", worker.ID)
		d.scheduler.rotate()
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		d.scheduler.rotate()
		return nil, fmt.Errorf("worker %s returned status %s", worker.ID, resp.Status)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}

	d.captureObservation(worker)
	d.considerRotation()

	outputs := splitOutputs(raw, len(jobs))
	return outputs, nil
}

func (d *workerDispatcher) considerRotation() {
	var total float64
	var maxLoad float64
	for _, w := range d.workers {
		load := float64(w.load())
		total += load
		if load > maxLoad {
			maxLoad = load
		}
		w.mu.RLock()
		cpuHot := w.metrics.CPUPercent > 80
		w.mu.RUnlock()
		if cpuHot {
			d.logger.Info().Msgf("worker %s hot, rotating", w.ID)
			d.scheduler.rotate()
			return
		}
	}
	avg := total / math.Max(1, float64(len(d.workers)))
	if maxLoad > avg*2+1 {
		d.logger.Info().Msg("load vector hotspot detected, square rotation triggered")
		d.scheduler.rotate()
	}
}

func (d *workerDispatcher) captureObservation(w *workerObject) {
	cpuPercents, _ := cpu.Percent(75*time.Millisecond, false)
	memStats, _ := mem.VirtualMemory()
	netStats, _ := net.IOCounters(false)
	obs := localObservation{Timestamp: time.Now()}
	if len(cpuPercents) > 0 {
		obs.CPUPercent = cpuPercents[0]
	}
	if memStats != nil {
		obs.MemoryBytes = memStats.Used
	}
	if len(netStats) > 0 {
		obs.NetBytes = netStats[0].BytesSent + netStats[0].BytesRecv
	}
	w.updateObservation(obs)
}

func splitOutputs(raw []byte, expected int) [][]byte {
	segments := bytes.Split(raw, []byte("\n"))
	outputs := make([][]byte, 0, expected)
	for _, seg := range segments {
		if len(outputs) == expected {
			break
		}
		copied := make([]byte, len(seg))
		copy(copied, seg)
		outputs = append(outputs, copied)
	}
	for len(outputs) < expected {
		outputs = append(outputs, []byte{})
	}
	return outputs
}

type manager struct {
	logger    zerolog.Logger
	workers   []*workerObject
	batcher   *mimeBatcher
	sanitizer *bluemonday.Policy
	control   *controlPlane
	id        string
	limiter   *concurrencyLimiter
	cpuShield *cpuShield
}

func newManager(logger zerolog.Logger, workers []*workerObject, batcher *mimeBatcher, managerID string, control *controlPlane, limiter *concurrencyLimiter, shield *cpuShield) *manager {
	return &manager{
		logger:    logger,
		workers:   workers,
		batcher:   batcher,
		sanitizer: bluemonday.StrictPolicy(),
		control:   control,
		id:        managerID,
		limiter:   limiter,
		cpuShield: shield,
	}
}

func (m *manager) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ingest", m.handleIngest)
	mux.HandleFunc("/workers", m.handleWorkers)
	mux.HandleFunc("/control/heartbeat", m.handleHeartbeat)
	mux.HandleFunc("/control/state", m.handleControlState)
	return mux
}

func (m *manager) runLocal(ctx context.Context, handler http.Handler, addr string) error {
	srv := &http.Server{Addr: addr, Handler: handler}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	m.logger.Info().Msgf("manager listening on %s", addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (m *manager) handleIngest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Access-Control-Allow-Methods", "POST,OPTIONS")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if m.control != nil && !m.control.IsLeader() {
		leader := m.control.LeaderID()
		w.Header().Set("X-Manager-Leader", leader)
		http.Error(w, "manager not leader", http.StatusServiceUnavailable)
		return
	}
	if m.cpuShield != nil && m.cpuShield.OverLimit() {
		http.Error(w, "manager overloaded (cpu)", http.StatusServiceUnavailable)
		return
	}
	if m.limiter != nil && !m.limiter.Acquire(r.Context()) {
		http.Error(w, "ingest backpressure", http.StatusServiceUnavailable)
		return
	}
	if m.limiter != nil {
		defer m.limiter.Release()
	}
	payload, err := io.ReadAll(io.LimitReader(r.Body, maxPayloadSize))
	if err != nil {
		http.Error(w, "failed to read payload", http.StatusBadRequest)
		return
	}
	if err := performDPI(payload); err != nil {
		http.Error(w, "payload rejected", http.StatusBadRequest)
		return
	}
	clean := m.sanitizer.SanitizeBytes(payload)
	mt := mimetype.Detect(clean)

	ctx := r.Context()
	response, err := m.batcher.submit(ctx, mt.String(), clean)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", mt.String())
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(response)
}

func (m *manager) handleWorkers(w http.ResponseWriter, _ *http.Request) {
	snapshots := make([]workerSnapshot, len(m.workers))
	for i, worker := range m.workers {
		snapshots[i] = worker.snapshot()
	}
	data, _ := json.MarshalIndent(snapshots, "", "  ")
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

func (m *manager) handleHeartbeat(w http.ResponseWriter, _ *http.Request) {
	resp := heartbeatResponse{
		ID:        m.id,
		Timestamp: time.Now(),
	}
	data, _ := json.Marshal(resp)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

func (m *manager) handleControlState(w http.ResponseWriter, _ *http.Request) {
	if m.control == nil {
		state := controlState{
			ManagerID: m.id,
			LeaderID:  m.id,
			IsLeader:  true,
			Timestamp: time.Now(),
		}
		data, _ := json.MarshalIndent(state, "", "  ")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
		return
	}
	state := m.control.snapshot()
	data, _ := json.MarshalIndent(state, "", "  ")
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

func performDPI(payload []byte) error {
	for _, re := range harmfulPatterns {
		if re.Match(payload) {
			return fmt.Errorf("payload blocked: %s", re.String())
		}
	}
	return nil
}

type managerConfig struct {
	ListenAddr      string
	WorkerEndpoints []string
	Order           int
	ManagerID       string
	PeerEndpoints   []string
	MaxInflight     int
	ManagerCPULimit float64
}

func loadConfig() (managerConfig, error) {
	cfg := managerConfig{ListenAddr: getEnv("MANAGER_ADDR", ":8080")}
	endpoints := getEnv("WORKERS", "")
	if endpoints == "" {
		cfg.WorkerEndpoints = []string{
			"http://worker1:8081",
			"http://worker2:8081",
			"http://worker3:8081",
			"http://worker4:8081",
		}
	} else {
		parts := strings.Split(endpoints, ",")
		for _, p := range parts {
			trimmed := strings.TrimSpace(p)
			if trimmed != "" {
				cfg.WorkerEndpoints = append(cfg.WorkerEndpoints, trimmed)
			}
		}
	}
	if len(cfg.WorkerEndpoints) == 0 {
		return cfg, errors.New("no workers configured")
	}
	orderEnv := getEnv("OLS_ORDER", "")
	if orderEnv != "" {
		if val, err := strconv.Atoi(orderEnv); err == nil && val > 0 {
			cfg.Order = val
		}
	}
	if cfg.Order == 0 {
		cfg.Order = int(math.Ceil(math.Sqrt(float64(len(cfg.WorkerEndpoints)))))
	}
	if cfg.Order*cfg.Order != len(cfg.WorkerEndpoints) {
		return cfg, fmt.Errorf("worker count %d must equal OLS order^2 (%d)", len(cfg.WorkerEndpoints), cfg.Order*cfg.Order)
	}
	managerID := getEnv("MANAGER_ID", "")
	if managerID == "" {
		if host, err := os.Hostname(); err == nil {
			managerID = host
		} else {
			managerID = fmt.Sprintf("manager-%d", time.Now().UnixNano())
		}
	}
	cfg.ManagerID = managerID
	peersEnv := getEnv("MANAGER_PEERS", "")
	if peersEnv != "" {
		parts := strings.Split(peersEnv, ",")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				cfg.PeerEndpoints = append(cfg.PeerEndpoints, p)
			}
		}
	}
	cfg.MaxInflight = parseIntEnv("MANAGER_MAX_INFLIGHT", defaultMaxInflight)
	cfg.ManagerCPULimit = parseFloatEnv("MANAGER_CPU_LIMIT", 0)
	return cfg, nil
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func parseIntEnv(key string, def int) int {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return def
	}
	if parsed, err := strconv.Atoi(val); err == nil && parsed >= 0 {
		return parsed
	}
	return def
}

func parseFloatEnv(key string, def float64) float64 {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return def
	}
	if parsed, err := strconv.ParseFloat(val, 64); err == nil && parsed >= 0 {
		return parsed
	}
	return def
}

func pollWorkerTelemetry(ctx context.Context, logger zerolog.Logger, worker *workerObject) {
	client := &http.Client{Timeout: 2 * time.Second}
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			url := strings.TrimSuffix(worker.Endpoint, "/") + "/telemetry"
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				logger.Err(err).Msg("telemetry request build failed")
				continue
			}
			resp, err := client.Do(req)
			if err != nil {
				logger.Err(err).Msgf("telemetry fetch failed for %s", worker.ID)
				continue
			}
			data, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				continue
			}
			var metrics workerMetrics
			if err := json.Unmarshal(data, &metrics); err != nil {
				logger.Err(err).Msg("telemetry decode failed")
				continue
			}
			worker.updateMetrics(metrics)
		}
	}
}

func runManagerCmd(cmd *cobra.Command, args []string) error {
	zerolog.TimeFieldFormat = time.RFC3339Nano
	logger := log.Output(zerolog.ConsoleWriter{Out: os.Stdout, NoColor: false})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if flagPort >= 0 {
		cfg.ListenAddr = fmt.Sprintf(":%d", flagPort)
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":8080"
	}

	workers := make([]*workerObject, len(cfg.WorkerEndpoints))
	for i, endpoint := range cfg.WorkerEndpoints {
		workers[i] = &workerObject{ID: fmt.Sprintf("worker-%d", i), Endpoint: endpoint}
	}

	scheduler := newOLSScheduler(cfg.Order)
	dispatcher := newWorkerDispatcher(logger, workers, scheduler)
	batcher := newMIMEBatcher(dispatcher)

	var control *controlPlane
	if len(cfg.PeerEndpoints) > 0 {
		control = newControlPlane(cfg.ManagerID, cfg.PeerEndpoints, logger)
	}
	var limiter *concurrencyLimiter
	if cfg.MaxInflight > 0 {
		limiter = newConcurrencyLimiter(cfg.MaxInflight, defaultLimiterWait)
	}
	var shield *cpuShield
	if cfg.ManagerCPULimit > 0 {
		shield = newCPUShield(cfg.ManagerCPULimit)
	}
	mgr := newManager(logger, workers, batcher, cfg.ManagerID, control, limiter, shield)

	if control != nil {
		go control.run(ctx)
	}
	if shield != nil {
		go shield.Start(ctx)
	}
	for _, w := range workers {
		go pollWorkerTelemetry(ctx, logger, w)
	}

	handler := mgr.handler()

	if flagDisableRelay {
		logger.Info().Strs("workers", cfg.WorkerEndpoints).Msg("manager running local-only (portal disabled)")
		return mgr.runLocal(ctx, handler, cfg.ListenAddr)
	}

	exposure, err := sdk.Expose(ctx, sdk.ExposeConfig{
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

	logger.Info().
		Str("portal_name", flagName).
		Strs("workers", cfg.WorkerEndpoints).
		Msg("manager registered with portal relay")

	if err := exposure.RunHTTP(ctx, handler, cfg.ListenAddr); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	logger.Info().Msg("manager shutdown complete")
	return nil
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal().Err(err).Msg("run manager command")
	}
}
