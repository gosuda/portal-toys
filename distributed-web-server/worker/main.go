package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
	"github.com/vmihailenco/msgpack/v5"
)

var newline = []byte{'\n'}

var (
	stdoutPool = sync.Pool{New: func() any { return new(bytes.Buffer) }}
	stderrPool = sync.Pool{New: func() any { return new(bytes.Buffer) }}
)

type concurrencyLimiter struct {
	sem  chan struct{}
	wait time.Duration
}

func newConcurrencyLimiter(limit int, wait time.Duration) *concurrencyLimiter {
	if limit <= 0 {
		return nil
	}
	if wait <= 0 {
		wait = 100 * time.Millisecond
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

type batchRequest struct {
	MIME     string   `msgpack:"mime"`
	Payloads [][]byte `msgpack:"payloads"`
}

type telemetrySnapshot struct {
	CPUPercent  float64   `json:"cpuPercent"`
	MemoryBytes uint64    `json:"memoryBytes"`
	NetBytes    uint64    `json:"netBytes"`
	ActiveJobs  int64     `json:"activeJobs"`
	Timestamp   time.Time `json:"timestamp"`
}

type workerServer struct {
	logger     zerolog.Logger
	binaryPath string
	telemetry  atomic.Pointer[telemetrySnapshot]
	activeJobs atomic.Int64
	limiter    *concurrencyLimiter
}

func newWorkerServer(logger zerolog.Logger, binary string, limiter *concurrencyLimiter) *workerServer {
	snapshot := &telemetrySnapshot{Timestamp: time.Now()}
	srv := &workerServer{logger: logger, binaryPath: binary, limiter: limiter}
	srv.telemetry.Store(snapshot)
	return srv
}

func (s *workerServer) handleInvoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	var batch batchRequest
	if err := msgpack.Unmarshal(body, &batch); err != nil {
		http.Error(w, "msgpack decode failed", http.StatusBadRequest)
		return
	}
	if len(batch.Payloads) == 0 {
		http.Error(w, "empty batch", http.StatusBadRequest)
		return
	}

	if s.limiter != nil && !s.limiter.Acquire(r.Context()) {
		http.Error(w, "worker saturated", http.StatusServiceUnavailable)
		return
	}
	if s.limiter != nil {
		defer s.limiter.Release()
	}
	s.activeJobs.Add(int64(len(batch.Payloads)))
	defer s.activeJobs.Add(-int64(len(batch.Payloads)))

	output, err := s.executeBatch(r.Context(), batch.Payloads)
	if err != nil {
		s.logger.Error().Err(err).Msg("binary execution failed")
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", batch.MIME)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(output)
}

func (s *workerServer) executeBatch(parent context.Context, payloads [][]byte) ([]byte, error) {
	deadline := 15 * time.Second
	ctx, cancel := context.WithTimeout(parent, deadline)
	defer cancel()

	cmd := exec.CommandContext(ctx, s.binaryPath)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe error: %w", err)
	}

	stdoutBuf := stdoutPool.Get().(*bytes.Buffer)
	stdoutBuf.Reset()
	stderrBuf := stderrPool.Get().(*bytes.Buffer)
	stderrBuf.Reset()
	defer func() {
		stdoutBuf.Reset()
		stderrBuf.Reset()
		stdoutPool.Put(stdoutBuf)
		stderrPool.Put(stderrBuf)
	}()

	cmd.Stdout = stdoutBuf
	cmd.Stderr = stderrBuf

	if err := cmd.Start(); err != nil {
		stdin.Close()
		return nil, err
	}

	if err := writeBatchToStdin(stdin, payloads); err != nil {
		stdin.Close()
		_ = cmd.Wait()
		return nil, fmt.Errorf("stdin write failed: %w", err)
	}
	stdin.Close()

	if err := cmd.Wait(); err != nil {
		errMsg := strings.TrimSpace(stderrBuf.String())
		return nil, fmt.Errorf("target execution error: %w (%s)", err, errMsg)
	}

	result := append([]byte(nil), stdoutBuf.Bytes()...)
	return result, nil
}

func (s *workerServer) handleTelemetry(w http.ResponseWriter, _ *http.Request) {
	snapshot := s.telemetry.Load()
	if snapshot == nil {
		http.Error(w, "telemetry unavailable", http.StatusServiceUnavailable)
		return
	}
	data, _ := json.Marshal(snapshot)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

func (s *workerServer) collectTelemetry(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			snapshot, err := s.readTelemetry()
			if err != nil {
				s.logger.Err(err).Msg("telemetry read failed")
				continue
			}
			snapshot.ActiveJobs = s.activeJobs.Load()
			snapshot.Timestamp = time.Now()
			s.telemetry.Store(snapshot)
		}
	}
}

func (s *workerServer) readTelemetry() (*telemetrySnapshot, error) {
	cpuPercents, err := cpu.Percent(50*time.Millisecond, false)
	if err != nil {
		return nil, err
	}
	memStats, err := mem.VirtualMemory()
	if err != nil {
		return nil, err
	}
	netStats, err := net.IOCounters(false)
	if err != nil {
		return nil, err
	}
	snapshot := &telemetrySnapshot{}
	if len(cpuPercents) > 0 {
		snapshot.CPUPercent = cpuPercents[0]
	}
	snapshot.MemoryBytes = memStats.Used
	if len(netStats) > 0 {
		snapshot.NetBytes = netStats[0].BytesSent + netStats[0].BytesRecv
	}
	return snapshot, nil
}

func writeBatchToStdin(w io.Writer, payloads [][]byte) error {
	for i, payload := range payloads {
		if i > 0 {
			if _, err := w.Write(newline); err != nil {
				return err
			}
		}
		if len(payload) == 0 {
			continue
		}
		if _, err := w.Write(payload); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	zerolog.TimeFieldFormat = time.RFC3339Nano
	logger := log.Output(zerolog.ConsoleWriter{Out: os.Stdout})

	binaryPath := getEnv("TARGET_BINARY", "/opt/target_binary.sh")
	if _, err := os.Stat(binaryPath); errors.Is(err, os.ErrNotExist) {
		logger.Fatal().Err(err).Msgf("target binary %s missing", binaryPath)
	}
	addr := ":" + getEnv("WORKER_PORT", "8081")

	maxParallel := parseIntEnv("WORKER_MAX_PARALLEL", runtime.NumCPU())
	limiter := newConcurrencyLimiter(maxParallel, 100*time.Millisecond)
	srv := newWorkerServer(logger, binaryPath, limiter)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	mux := http.NewServeMux()
	mux.HandleFunc("/invoke", srv.handleInvoke)
	mux.HandleFunc("/telemetry", srv.handleTelemetry)

	server := &http.Server{Addr: addr, Handler: mux}

	go srv.collectTelemetry(ctx)

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	logger.Info().Msgf("worker listening on %s", addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Fatal().Err(err).Msg("worker failed")
	}
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
	if parsed, err := strconv.Atoi(val); err == nil && parsed > 0 {
		return parsed
	}
	return def
}
