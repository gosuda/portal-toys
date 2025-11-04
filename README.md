# portal-toys
portal-toys is a collection of simple demos that demonstrate how to build, publish, and interact with decentralized services through the [portal](https://github.com/gosuda/portal) network.
Each toy app can be generated or modified easily using LLMs allowing rapid prototyping and experimentation.

## Quick Start (Using LLM)
1) 선호하는 도구를 엽니다. (Codex, Gemini, Claude Code)
2) 프롬프트로 예제 코드를 생성합니다. (~~ 하는 프로그램을 만들어줘. 다른 예시를 참고해줘 (또는 코드 복사))
3) 코드를 실행합니다. (`go run . --port 8081 --server-url http://portal.gosuda.org --name my-demo`)
4) 동작을 확인합니다. (`localhost:8081` 또는 `http://portal.gosuda.org`)
5) 잘 돌아가면 저장소에 푸시합니다.

## Golang Examples

- [chatter-bbs](./golang/chatter-bbs/)
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
Will be added soon

## Python Examples
Will be added soon

## Rust Examples
Will be added soon

## Tips
- 기본 Portal 서버 URL은 예제마다 `http://portal.gosuda.org`로 되어 있습니다. 과도한 요청으로 **디도스 공격이 되지 않도록 주의**해주세요.
- 광고가 성공하면 서버 UI(관리 페이지)에서 등록된 이름(`--name`)으로 보입니다.
- Go 버전은 `go.mod` 기준(Go 1.25+)을 권장합니다. 최신 Go면 대부분 문제 없습니다.
