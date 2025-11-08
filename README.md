# portal-toys
portal-toys is a collection of simple demos that demonstrate how to build, publish, and interact with decentralized services through the [portal](https://github.com/gosuda/portal) network.
Each toy app can be generated or modified easily using LLMs allowing rapid prototyping and experimentation.

## Quick Start (Using LLM)
1) 선호하는 도구를 엽니다. (Codex, Gemini, Claude Code)
2) 프롬프트로 예제 코드를 생성합니다. (~~ 하는 프로그램을 만들어줘. 다른 예시를 참고해줘 (또는 코드 복사))
3) 코드를 실행합니다. (`go run . --port 8081 --server-url wss://portal.gosuda.org/relay,wss://portal.thumbgo.kr/relay --name my-demo`)
4) 동작을 확인합니다. (`wss://portal.gosuda.org/relay`, `wss://portal.thumbgo.kr/relay`)
5) 잘 돌아가면 저장소에 푸시합니다.

## Golang Examples

- [chatter-bbs](./golang/chatter-bbs/)
- [openboard](./golang/openboard/)
- [doom](./golang/doom/)
- [gosuda-blog](./golang/gosuda-blog/)
- [http-backend](./golang/http-backend/)
- [paint](./golang/paint/)
- [rolling-paper](./golang/rolling-paper/)
- [simple-chat](./golang/simple-chat/)
- [tetris](./golang/tetris/)
- [youtube-chat](./golang/youtube-chat/)
- [vscode-chat](./golang/vscode-chat/)

## Javascript Examples

- [simple-example](./javascript/simple-example/)
- [rolling-paper](./javascript/rolling-paper/)

## Python Examples
Will be added soon

## Rust Examples
Will be added soon

## Tips
- Portal 에 대한 과도한 요청으로 **디도스 공격이 되지 않도록 주의**해주세요.
- 연결이 성공하면 서버 UI(관리 페이지)에서 등록된 이름(`--name`)으로 보입니다.
- Go 버전은 `go.mod` 기준(Go 1.25+)을 권장합니다. 최신 Go면 대부분 문제 없습니다.

## openboard (사용자 주도 HTML 커뮤니티)

- 실행(로컬만): `PORT=8082 DATA_DIR=./golang/openboard/data go run ./golang/openboard`
- 포털 노출: `go run ./golang/openboard --server-url wss://portal.gosuda.org/relay --name openboard --port 8082 --data-path ./golang/openboard/data`
- 기능
  - 폼으로 `slug`, `title`, `HTML`을 제출하면 `DATA_DIR/pages/{slug}.html`로 저장되고 목록에 반영됩니다.
  - 보기 모드 2종:
    - 샌드박스(`/p/{slug}`): iframe sandbox로 안전하게 확인
    - RAW(`/raw/{slug}`): 제출한 HTML을 그대로 응답
- 주의: RAW는 브라우저에서 그대로 실행됩니다. 신뢰하지 않는 콘텐츠는 샌드박스 모드로 보세요.
- 배포: `make tunnel`을 사용해 로컬 포트를 포털로 노출할 수 있습니다. 예) `PORT=8082 make tunnel`
- 변경 가능/불가능 경계
  - 변경 불가능(immutable): 앱 UI 정적 파일은 바이너리에 내장됨 — `golang/openboard/static/*.html`
  - 변경 가능(mutable): 사용자 페이지 데이터 — `DATA_DIR/pages/*.html`, `DATA_DIR/pages.json`

## noryangjin-prices (노량진 시세 보기)

- 실행: `PORT=8080 go run ./golang/noryangjin-prices`
- 외부 CSV 연동(선택): 공개 CSV 링크를 환경변수로 지정
  - 예: `PRICE_CSV_URL="https://docs.google.com/spreadsheets/d/e/.../pub?output=csv" PORT=8080 go run ./golang/noryangjin-prices`
  - CSV 헤더 예시: `name,price,unit,note,source`
  - 5분마다 자동 갱신(`/api/refresh`로 수동 새로고침 가능)
  - 기본값은 샘플 데이터가 표시됩니다.
