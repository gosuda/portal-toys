## 99.99% 순금을 노려라!

`gold-digger` 폴더는 분산 Manager/Worker 패브릭 위에서 동작하는 정련 확률 예측 가챠 게임입니다. 모든 요청은 Manager의 `/ingest` 엔드포인트로 POST 되며, Worker는 `gold-digger` 바이너리를 실행해 JSON 명령을 처리합니다.

### 핵심 특징

* **회원가입/로그인** – SQLite에 bcrypt 해시를 저장합니다. 세션 토큰은 12시간 동안만 유효하며, 가입 즉시 500pt 지갑이 충전됩니다.
* **정련 베팅 게임** – 플레이어는 “성공” 혹은 “실패” 중 하나에 포인트를 베팅합니다. 엔진은 비밀 확률을 샘플링한 뒤 실제 성공 여부를 판정하고, 공정한 배당률(확률 기반)로 지갑을 증감합니다.
* **리더보드** – 상위 10명의 누적 포인트를 한 번에 조회할 수 있습니다.
* **Docker Compose 지원** – `docker compose up --build` 한 번으로 Manager/Worker/게임 DB가 모두 기동됩니다.

### 실행 방법

```bash
cd gold-digger
docker compose up --build
```

* Manager API: `http://localhost:8080/ingest`
* Worker Telemetry: `http://localhost:8081/telemetry`
* 웹 UI: `http://localhost:8090` (정적 HTML/JS, CORS 허용)
* SQLite 데이터는 `gold-data` 볼륨(`/data/gold-digger.db`)에 영구 저장됩니다.

### 브라우저 UI

`gold-digger/web/index.html` 은 가입/로그인 폼, 정련 버튼, VS 매칭 컨트롤, 티어별 배팅 UI, 리더보드까지 모두 포함된 단일 페이지입니다. docker compose 를 띄웠다면 `http://localhost:8090` 에 접속해 바로 사용할 수 있으며, 다른 환경에서 열어보고 싶다면 HTML 파일을 그대로 브라우저에 끌어다 놓아도 됩니다. (API 엔드포인트는 `http://localhost:8080/ingest` 로 기본 설정되어 있습니다.)

### API 사용 예시

모든 요청은 `Content-Type: application/json` 으로 `POST http://localhost:8080/ingest` 에 전송합니다.

| 액션 | 설명 |
|------|------|
| `signup` | `{ "username": "miner", "password": "Sup3rSecret!" }` |
| `signin` | `{ "username": "miner", "password": "Sup3rSecret!" }` 토큰이 반환됩니다. |
| `refine` | `{ "token": "...", "choice": "success|fail", "stake": 50 }` 정련 베팅 |
| `status` | `{ "token": "..." }` 리더보드, 지갑, VS 상황, 최근 베팅 |
| `vs_queue` | `{ "token": "..." }` VS 대기열 등록 혹은 기존 대기와 매칭 |
| `vs_move` | `{ "token": "...", "matchId": 12, "guess": 90.1 }` 매치에서 예측 제출 |
| `vs_status` | `{ "token": "..." }` (선택) VS/베팅 현황 조회 |
| `vs_bet` | `{ "token": "...", "matchId": 12, "betTarget": "A|B|username", "betTier": 10, "betAmount": 50 }` |

### VS & 배팅 규칙

1. VS 매치는 2명의 플레이어가 각각 40~100 사이의 정련 성공 확률을 예측하고, 같은 실제 값에 대해 더 가까운 쪽이 승리합니다. 승자에게는 +150pt, 패자에게는 위로금 +60pt가 지갑에 적립됩니다.
2. VS 에 참가하지 않은 사용자도 매치가 `active` 상태일 때 승자를 예측해 베팅할 수 있습니다. 베팅 금액은 즉시 지갑에서 잠기며, 결과에 따라 원금과 이자를 한 번에 정산합니다.
3. 베팅 티어 (성공 시 수익 / 실패 시 손실):
   * 5% 이율 / 실패 시 -5%
   * 10% 이율 / 실패 시 -15%
   * 20% 이율 / 실패 시 -30%
   * 30% 이율 / 실패 시 -40%
4. 실패 시 손실 폭만큼만 차감되고 원금은 그대로 반환되므로, 어느 티어든 최대 손실은 원금의 40% 입니다.

### 정련 베팅 규칙

1. 한 번의 베팅마다 **성공** 또는 **실패** 를 선택하고, 최소 1pt 이상의 금액을 걸어야 합니다.
2. 엔진은 55~95% 구간(드물게 99.99%)의 비밀 확률을 샘플링한 뒤, 그 확률에 맞춰 실제 정련 성공 여부를 결정합니다.
3. 베팅이 적중하면 공정 배당률로 정산합니다. 예: 성공 확률이 70%일 때 성공에 베팅하면 `100/70 ≈ 1.43` 배로 환급되고, 실패에 베팅해서 맞추면 `100/(30)` 배가 돌아옵니다. (원금 + 수익)
4. 적중하지 못하면 베팅 금액 전액을 잃습니다.
5. 모든 결과는 `refine_rounds` 테이블에 기록되고, `delta` 값이 리더보드의 누적 포인트로 합산됩니다.

### 환경 변수

| 변수 | 기본값 | 설명 |
|------|--------|------|
| `GOLD_DIGGER_DB` | `./gold-digger/data/goldsmiths.db` | 게임 서버가 사용할 SQLite 파일 경로 |
| `WORKER_MAX_PARALLEL` | `1` | Worker 컨테이너에서 동시에 실행할 바이너리 개수 |

필요 시 `docker-compose.yml` 에서 값을 조정하면 됩니다. UI 서비스가 별도로 추가되었으므로, 포트 충돌 시 `8090:80` 매핑을 원하는 값으로 바꾸면 됩니다.
