# Distributed Web Server

## Manager 만들기 & Worker Join (Quick Bootstrap)

- **Manager 만드는 법**
  1. `cd distributed-web-server`
  2. `go build ./manager`
  3. 실행 예시 (Portal relay 포함 단일 노드):  
     ```bash
     MANAGER_ID=manager-a WORKERS=http://localhost:8081 \ 
       go run ./manager --server-url https://portal.example.org/ --name distributed-lab --port 8080
     ```
  4. 다중 매니저(HA) 구성 시 각 인스턴스에 고유한 `MANAGER_ID`를 주고 서로를 `MANAGER_PEERS`에 등록합니다.  
     예) `MANAGER_PEERS=http://manager-a:8080,http://manager-b:8080`

- **Worker Join 하는 법**
  1. `go build ./worker`
  2. `TARGET_BINARY=./target_binary.sh WORKER_PORT=8081 WORKER_MAX_PARALLEL=4 go run ./worker`
  3. Manager의 `WORKERS` 환경 변수에 해당 워커의 HTTP 엔드포인트(`http://host:port`)를 추가합니다. HA 구성 시 모든 매니저의 `WORKERS` 값이 동일해야 합니다.

A reference "manager-worker" fabric implemented in pure Go. The manager performs ingress-level safety checks, maintains an Orthogonal Latin Square (OLS) scheduler, batches payloads by MIME/size, and dispatches workloads to workers. Workers load arbitrary binaries, push batched payloads through STDIN, and forward STDOUT back to the manager.

## HA Control Plane

- 복수의 매니저를 같은 애플리케이션에 띄우면 각 인스턴스는 `/control/heartbeat`를 통해 서로를 탐지하며, 가장 작은 `MANAGER_ID`를 가진 노드가 리더로 선출됩니다.
- `MANAGER_PEERS`에는 "다른" 매니저들의 베이스 URL을 쉼표로 구분하여 기입합니다. 예: `MANAGER_PEERS=http://manager-a:8080,http://manager-b:8080`.
- 리더만 `/ingest` 요청을 처리하고, 팔로워는 `503`과 함께 `X-Manager-Leader` 헤더를 반환하여 호출자가 리더로 재전송할 수 있게 합니다.
- `/control/state` 엔드포인트는 현재 리더, 피어 헬스, 그리고 이 노드가 리더인지 여부를 JSON으로 노출하므로 외부 관제(Control Plane)에서 바로 사용할 수 있습니다.
- `MANAGER_MAX_INFLIGHT`로 동시 인입 요청 수를 제한하고, `MANAGER_CPU_LIMIT`(예: `0.85`)를 설정하면 매니저 CPU가 한계에 다다를 경우 자동으로 백프레셔를 겁니다.
- Portal 미연결 모드가 필요하면 `--disable-relay` 플래그를 주거나 `MANAGER_DISABLE_RELAY=true`로 설정하여 순수 로컬 HTTP 모드로 실행할 수 있습니다.

## Components

### Manager
- **OLS Scheduler with Dynamic Rotation** – Generates an order-`N` orthogonal schedule, ensuring that every worker pair is exercised uniformly. When a load hotspot ("load vector") or CPU saturation is detected, the square is rotated to a different orthogonal phase, redistributing assignments across the `N x N` plane.
- **Worker Registry** – Tracks `WorkerObject` telemetry (CPU, memory, network, current load) via periodic `/telemetry` scrapes and a local gopsutil observer for manager-side metrics.
- **DPI & Sanitization** – Regex-based pre-filtering for SQLi/XSS/path traversal and final sanitization via `bluemonday.StrictPolicy()`.
- **MIME Batching** – Payloads sharing `mime | sizeBucket` are queued into a windowed buffer that flushes every 10 ms or at 1 MB. Batches are MsgPack-serialized and posted to workers.
- **Control Plane Coordination** – Multiple managers gossip via `/control/heartbeat`, electing the lexicographically smallest `MANAGER_ID` as leader and exposing `/control/state` for HA observability.

### Worker
- **Binary Loader** – Uses `os/exec` with `context.Context` deadlines to run the binary pointed to by `$TARGET_BINARY`. Input payloads are concatenated with `\n` separators and streamed through STDIN.
- **Telemetry** – Periodic CPU/memory/network sampling through `github.com/shirou/gopsutil/v3` plus active job counters. Exposed via `/telemetry` and fed back into the manager registry.
- **Response Path** – STDOUT bytes from the binary are flushed verbatim as the HTTP response, enabling the manager to split results back to individual callers.
- **Concurrency Guard** – `WORKER_MAX_PARALLEL` controls 얼마나 많은 바이너리를 동시에 실행할지 결정하여 온프렘 CPU를 보호합니다.

## Binary Interface Specification

1. **Transport contract** – The manager sends MsgPack payloads with schema `{ mime: string, payloads: [][]byte }` to `/invoke`.
2. **STDIN layout** – Workers join payloads using a single `\n` delimiter and pass the byte stream into the binary. Payload order is preserved.
3. **STDOUT expectations** – The binary must emit one line of output per input payload (also newline-delimited). The manager splits STDOUT on `\n` and maps each entry back to the originating request.
4. **Environment** – Workers honour `TARGET_BINARY` (absolute path to executable/script) and `WORKER_PORT`. The Docker image ships with `target_binary.sh`, an uppercase-echo reference implementation.
5. **Error propagation** – Non-zero exit codes bubble back as HTTP 502 with stderr attached to the error string.

## Vector Rotation Guide

- The cluster expects `N^2` workers. With four workers the order is 2, yielding a 2×2 matrix covering every orthogonal pair.
- Scheduler state is advanced on each dispatch; rotation jumps by `order` positions, effectively walking a perpendicular diagonal through the OLS.
- Rotation triggers:
  1. Any worker reporting CPU > 80 %.
  2. A detected load vector where `max(load)` exceeds `2×avg(load)+1`.
  3. Transport failures (worker offline) immediately advance the square to the next orthogonal dimension.
- Tune behaviour via `OLS_ORDER` (manager) and adjust thresholds in `manager/main.go` if the use case demands different rotation sensitivity.

## Performance Considerations
- Batch windows are keyed by `{MIME, sizeBucket}` and backed by a `sync.Pool`, minimizing allocations as windows open/close under bursty traffic.
- Worker dispatch uses a shared HTTP transport with generous idle pools so the manager can saturate on-prem links without thrashing TCP handshakes.
- Workers stream payloads directly into the target binary’s STDIN (no intermediate buffer copies) and reuse stdout/stderr buffers via pools for cache-friendly execution.
- All slow paths honor `context.Context` deadlines to prevent runaway binaries or stalled HTTP hops from blocking the pipeline.
- Manager ingress is protected by a concurrency limiter plus optional CPU shield (`MANAGER_CPU_LIMIT`), while workers gate execution slots via `WORKER_MAX_PARALLEL` to avoid draining shared CPUs.
- Portal relay exposure flows through the Go SDK (`--server-url`, `--name`, metadata flags) so remote clients reach the ingress directly while the same handler can optionally listen on a local TCP port via `--port`.

## Running Locally

### Native
```bash
cd distributed-web-server
MANAGER_ID=manager-a MANAGER_ADDR=:8080 MANAGER_MAX_INFLIGHT=256 MANAGER_CPU_LIMIT=0.85 WORKERS=http://localhost:8081 go run ./manager &
# 두 번째 매니저를 추가하려면 (예)
# MANAGER_ID=manager-b MANAGER_ADDR=:8082 MANAGER_PEERS=http://localhost:8080 MANAGER_MAX_INFLIGHT=256 WORKERS=http://localhost:8081 go run ./manager
TARGET_BINARY=./target_binary.sh WORKER_PORT=8081 WORKER_MAX_PARALLEL=4 go run ./worker
```

### Portal Relay 노출
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
`--disable-relay` 플래그를 주면 Portal 연결 없이도 동일한 HTTP 엔드포인트를 로컬에서만 노출할 수 있습니다.

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
- Manager API: `http://localhost:8080/ingest`
- Worker registry view: `http://localhost:8080/workers`

### Telemetry API
- `GET /workers` – manager-side snapshot including load and observed metrics.
- `GET /telemetry` (worker) – raw worker telemetry if you need to inspect workers directly.

## Security Notes
- DPI blocks SQL injection, XSS, and path traversal strings before batching.
- `bluemonday` sanitizes every payload to neutralize lingering HTML tags.
- Workers run binaries in separate processes with context-driven timeouts and no network access beyond what the binary itself performs.

## Repository Layout
- `manager/main.go` – ingress pipeline, scheduler, batching, DPI.
- `worker/main.go` – binary launcher, telemetry server.
- `target_binary.sh` – sample processing binary that uppercases each line.
- `Dockerfile` – multi-stage build producing `manager` and `worker` images plus the sample binary.
- `docker-compose.yml` – 1× manager + 4× workers (2×2 OLS) deployment blueprint.
