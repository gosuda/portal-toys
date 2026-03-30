# Distributed Web Server

## Create Manager & Join Workers (Quick Bootstrap)

* **Creating a Manager**

  1. `cd distributed-web-server`
  2. `go build ./manager`
  3. Example run (single node with Portal relay):

     ```bash
     MANAGER_ID=manager-a WORKERS=http://localhost:8081 \
       go run ./manager --server-url https://portal.example.org/ --name distributed-lab --port 8080
     ```
  4. For multi-manager (HA) setup, assign each instance a unique `MANAGER_ID` and register peers via `MANAGER_PEERS`.
     Example: `MANAGER_PEERS=http://manager-a:8080,http://manager-b:8080`

* **Joining Workers**

  1. `go build ./worker`
  2. `TARGET_BINARY=./target_binary.sh WORKER_PORT=8081 WORKER_MAX_PARALLEL=4 go run ./worker`
  3. Add each worker’s HTTP endpoint (`http://host:port`) to the Manager’s `WORKERS` environment variable. In HA mode, all managers must share the same `WORKERS` list.

A reference "manager-worker" fabric implemented in pure Go. The manager performs ingress-level safety checks, maintains an Orthogonal Latin Square (OLS) scheduler, batches payloads by MIME/size, and dispatches workloads to workers. Workers load arbitrary binaries, push batched payloads through STDIN, and forward STDOUT back to the manager.

## HA Control Plane

* When multiple managers run, each instance discovers others via `/control/heartbeat`, and the node with the smallest `MANAGER_ID` becomes the leader.
* `MANAGER_PEERS` should list the base URLs of other managers, comma-separated. Example: `MANAGER_PEERS=http://manager-a:8080,http://manager-b:8080`.
* Only the leader processes `/ingest` requests. Followers return `503` with an `X-Manager-Leader` header so clients can retry against the leader.
* `/control/state` exposes the current leader, peer health, and whether this node is the leader in JSON format for external control-plane integration.
* `MANAGER_MAX_INFLIGHT` limits concurrent ingress requests. `MANAGER_CPU_LIMIT` (e.g., `0.85`) enables automatic backpressure when CPU usage is high.
* To disable Portal relay and run purely in local HTTP mode, use `--disable-relay` or set `MANAGER_DISABLE_RELAY=true`.

## Components

### Manager

* **OLS Scheduler with Dynamic Rotation** – Generates an order-`N` orthogonal schedule, ensuring uniform coverage of worker pairs. On load hotspots or CPU saturation, the square rotates to redistribute assignments.
* **Worker Registry** – Tracks `WorkerObject` telemetry (CPU, memory, network, load) via `/telemetry` scraping and local gopsutil metrics.
* **DPI & Sanitization** – Regex-based filtering for SQLi/XSS/path traversal, followed by sanitization using `bluemonday.StrictPolicy()`.
* **MIME Batching** – Groups payloads by `mime | sizeBucket` into windowed buffers flushed every 10 ms or 1 MB. Batches are MsgPack-encoded and sent to workers.
* **Control Plane Coordination** – Managers communicate via `/control/heartbeat`, elect a leader, and expose `/control/state` for observability.

### Worker

* **Binary Loader** – Uses `os/exec` with `context.Context` deadlines to execute `$TARGET_BINARY`. Payloads are concatenated with `\n` and streamed via STDIN.
* **Telemetry** – Periodic CPU/memory/network sampling via `github.com/shirou/gopsutil/v3`, plus active job counts. Exposed through `/telemetry`.
* **Response Path** – STDOUT is returned verbatim as HTTP response, allowing the manager to map results back to requests.
* **Concurrency Guard** – `WORKER_MAX_PARALLEL` controls how many binaries run concurrently to protect on-prem CPU resources.

## Binary Interface Specification

1. **Transport contract** – Manager sends MsgPack payloads `{ mime: string, payloads: [][]byte }` to `/invoke`.
2. **STDIN layout** – Worker concatenates payloads with `\n`, preserving order.
3. **STDOUT expectations** – One line per input payload, newline-delimited. Manager splits and maps results back.
4. **Environment** – Workers use `TARGET_BINARY` and `WORKER_PORT`. The default image includes `target_binary.sh` as a reference implementation.
5. **Error propagation** – Non-zero exit codes return HTTP 502 with stderr included.

## Vector Rotation Guide

* The system expects `N^2` workers. For 4 workers, order = 2 (2×2 matrix).
* Scheduler advances per dispatch; rotation jumps by `order`, traversing orthogonal dimensions.
* Rotation triggers:

  1. Any worker CPU > 80%.
  2. Load imbalance where `max(load) > 2×avg(load)+1`.
  3. Worker failure (offline) triggers immediate rotation.
* Tune via `OLS_ORDER` and thresholds in `manager/main.go`.

## Performance Considerations

* Batch windows keyed by `{MIME, sizeBucket}` with `sync.Pool` to reduce allocations.
* Shared HTTP transport with large idle pools minimizes connection overhead.
* Workers stream directly into STDIN without intermediate copies.
* stdout/stderr buffers are reused for cache efficiency.
* All blocking paths respect `context.Context` deadlines.
* Manager uses concurrency limits and optional CPU throttling; workers enforce execution limits via `WORKER_MAX_PARALLEL`.
* Portal relay uses Go SDK (`--server-url`, `--name`, metadata flags) while optionally exposing local TCP via `--port`.

## Running Locally

### Native

```bash
cd distributed-web-server
MANAGER_ID=manager-a MANAGER_ADDR=:8080 MANAGER_MAX_INFLIGHT=256 MANAGER_CPU_LIMIT=0.85 WORKERS=http://localhost:8081 go run ./manager &
# Add a second manager (example)
# MANAGER_ID=manager-b MANAGER_ADDR=:8082 MANAGER_PEERS=http://localhost:8080 MANAGER_MAX_INFLIGHT=256 WORKERS=http://localhost:8081 go run ./manager
TARGET_BINARY=./target_binary.sh WORKER_PORT=8081 WORKER_MAX_PARALLEL=4 go run ./worker
```

### Portal Relay Exposure

```bash
cd distributed-web-server
MANAGER_ID=manager-a WORKERS=http://worker1:8081,http://worker2:8081 \
  go run ./manager \
  --server-url https://portal.gosuda.org/ \
  --name distributed-lab \
  --description "Distributed OLS manager" \
  --tags "distributed,ols,batcher" \
  --port 8080
```

Use `--disable-relay` to run without Portal connectivity.

Send traffic:

```bash
curl -XPOST http://localhost:8080/ingest -d 'sample payload'
```

### Docker Compose

```bash
cd distributed-web-server
docker compose up --build
```

Exposed endpoints:

* Manager API: `http://localhost:8080/ingest`
* Worker registry view: `http://localhost:8080/workers`

### Telemetry API

* `GET /workers` – Manager-side snapshot including load and metrics.
* `GET /telemetry` (worker) – Raw worker telemetry.

## Security Notes

* DPI blocks SQL injection, XSS, and path traversal before batching.
* `bluemonday` sanitizes payloads to neutralize HTML.
* Workers execute binaries in isolated processes with context timeouts and no enforced network beyond the binary itself.

## Repository Layout

* `manager/main.go` – ingress pipeline, scheduler, batching, DPI.
* `worker/main.go` – binary launcher, telemetry server.
* `target_binary.sh` – sample binary (uppercase echo).
* `Dockerfile` – multi-stage build for manager/worker images.
* `docker-compose.yml` – deployment blueprint (1 manager + 4 workers).
