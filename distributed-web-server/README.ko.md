# Distributed Web Server

## Manager 생성 및 Worker 연결 (Quick Bootstrap)

* **Manager 생성**

  1. `cd distributed-web-server`
  2. `go build ./manager`
  3. 실행 예시 (Portal relay 포함 단일 노드):

     ```bash
     MANAGER_ID=manager-a WORKERS=http://localhost:8081 \
       go run ./manager --server-url https://portal.example.org/ --name distributed-lab --port 8080
     ```
  4. 다중 매니저(HA) 구성 시 각 인스턴스에 고유한 `MANAGER_ID`를 지정하고 `MANAGER_PEERS`에 서로 등록

     ```
     MANAGER_PEERS=http://manager-a:8080,http://manager-b:8080
     ```

* **Worker 연결**

  1. `go build ./worker`
  2. `TARGET_BINARY=./target_binary.sh WORKER_PORT=8081 WORKER_MAX_PARALLEL=4 go run ./worker`
  3. Manager의 `WORKERS` 환경 변수에 워커 HTTP 엔드포인트(`http://host:port`) 추가
     HA 구성에서는 모든 매니저가 동일한 `WORKERS` 값을 사용해야 함

순수 Go로 구현된 manager-worker 구조의 참조 시스템이다. Manager는 ingress 단계에서 안전성 검사를 수행하고, Orthogonal Latin Square(OLS) 스케줄러를 유지하며, MIME/크기 기준으로 payload를 배치 처리한 뒤 Worker로 분배한다. Worker는 임의의 바이너리를 실행하고, 배치된 payload를 STDIN으로 전달하며, STDOUT 결과를 Manager로 반환한다.

## HA Control Plane

* 여러 Manager가 실행되면 `/control/heartbeat`를 통해 서로를 탐지하고, 가장 작은 `MANAGER_ID`를 가진 노드가 리더가 된다.
* `MANAGER_PEERS`에는 다른 Manager들의 base URL을 쉼표로 구분하여 설정
* 리더만 `/ingest` 요청을 처리하며, 팔로워는 `503`과 `X-Manager-Leader` 헤더를 반환하여 리더로 재요청하도록 유도
* `/control/state`는 현재 리더, 피어 상태, 자신이 리더인지 여부를 JSON으로 제공
* `MANAGER_MAX_INFLIGHT`로 동시 요청 수 제한, `MANAGER_CPU_LIMIT`으로 CPU 기반 백프레셔 적용
* `--disable-relay` 또는 `MANAGER_DISABLE_RELAY=true`로 Portal 없이 로컬 HTTP 모드 실행 가능

## 구성 요소

### Manager

* **OLS 스케줄러 (동적 회전)** – order-`N` 정사각 행렬 기반으로 모든 Worker 조합을 균등하게 사용. 부하 집중 또는 CPU 포화 시 회전하여 재분배
* **Worker Registry** – `/telemetry` 기반으로 CPU, 메모리, 네트워크, 로드 추적 및 gopsutil을 통한 로컬 관측
* **DPI 및 정화** – SQLi/XSS/경로 탐색 문자열을 정규식으로 차단하고 `bluemonday.StrictPolicy()`로 후처리
* **MIME 배칭** – `mime | sizeBucket` 기준으로 10ms 또는 1MB 단위로 배치 후 MsgPack으로 직렬화하여 전송
* **Control Plane** – `/control/heartbeat`로 상태 교환 및 리더 선출, `/control/state` 제공

### Worker

* **Binary Loader** – `os/exec`와 `context.Context`를 사용하여 `$TARGET_BINARY` 실행, payload를 `\n`으로 연결해 STDIN 전달
* **Telemetry** – gopsutil 기반 CPU/메모리/네트워크 측정 + 작업 수, `/telemetry`로 제공
* **응답 처리** – STDOUT을 그대로 HTTP 응답으로 반환, Manager가 라인 단위로 결과 매핑
* **동시성 제어** – `WORKER_MAX_PARALLEL`로 동시 실행 제한

## 바이너리 인터페이스 명세

1. **전송 구조** – Manager는 `{ mime: string, payloads: [][]byte }` 형식의 MsgPack 데이터를 `/invoke`로 전송
2. **STDIN 형식** – Worker는 payload를 `\n`으로 연결하여 전달 (순서 유지)
3. **STDOUT 규칙** – 입력 payload당 한 줄 출력, Manager가 분리 후 매핑
4. **환경 변수** – `TARGET_BINARY`, `WORKER_PORT` 사용. 기본 이미지에는 `target_binary.sh` 포함
5. **에러 처리** – exit code가 0이 아니면 HTTP 502와 stderr 반환

## Vector Rotation 가이드

* Worker 수는 `N^2` 형태를 기대 (예: 4개 → 2×2)
* 스케줄러는 dispatch마다 상태를 이동하며, 회전은 `order` 단위로 수행
* 회전 조건:

  1. Worker CPU > 80%
  2. `max(load) > 2×avg(load)+1`
  3. Worker 장애 발생 시 즉시 회전
* `OLS_ORDER` 및 `manager/main.go`에서 임계값 조정 가능

## 성능 고려사항

* `{MIME, sizeBucket}` 기반 배치 + `sync.Pool`로 메모리 할당 최소화
* HTTP keep-alive 및 idle pool 활용으로 연결 비용 절감
* Worker는 중간 버퍼 없이 STDIN으로 직접 스트리밍
* stdout/stderr 버퍼 재사용
* 모든 블로킹 경로는 `context.Context` 기반 timeout 적용
* Manager는 동시성 제한 및 CPU 보호, Worker는 병렬 실행 제한
* Portal relay는 Go SDK 기반으로 외부 접근 지원하며 `--port`로 로컬도 동시에 바인딩 가능

## 로컬 실행

### Native

```bash
cd distributed-web-server
MANAGER_ID=manager-a MANAGER_ADDR=:8080 MANAGER_MAX_INFLIGHT=256 MANAGER_CPU_LIMIT=0.85 WORKERS=http://localhost:8081 go run ./manager &
# 추가 매니저 예시
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

`--disable-relay` 사용 시 Portal 없이 로컬에서만 동작

요청 전송:

```bash
curl -XPOST http://localhost:8080/ingest -d 'sample payload'
```

### Docker Compose

```bash
cd distributed-web-server
docker compose up --build
```

노출 엔드포인트:

* Manager API: `http://localhost:8080/ingest`
* Worker 상태: `http://localhost:8080/workers`

### Telemetry API

* `GET /workers` – Manager 기준 Worker 상태
* `GET /telemetry` – Worker 직접 조회

## 보안

* SQL injection, XSS, 경로 탐색 문자열 사전 차단
* `bluemonday` 기반 HTML 정화
* Worker는 별도 프로세스로 실행되며 timeout 적용

## 저장소 구조

* `manager/main.go` – ingress, 스케줄러, 배칭, DPI
* `worker/main.go` – 바이너리 실행, telemetry
* `target_binary.sh` – 샘플 처리 바이너리
* `Dockerfile` – multi-stage 빌드
* `docker-compose.yml` – 1 Manager + 4 Worker 구성
