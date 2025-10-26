# relaydns-toys
relaydns-toys is a collection of simple demos that demonstrate how to build, publish, and interact with decentralized services through the [relaydns](https://github.com/gosuda/relaydns) network.
Each toy app can be generated or modified easily using LLMs allowing rapid prototyping and experimentation.

## Quick Start (Using LLM)
1) 선호하는 도구를 엽니다. (Codex/o1, Gemini, Claude Code)
2) 프롬프트로 예제 코드를 생성합니다. (~~ 하는 프로그램을 만들어줘. 다른 예시를 참고해줘 (또는 코드 복사))
3) 코드를 실행합니다. (`go run . --port 8081 --server-url http://relaydns.gosuda.org --name my-demo`)
4) 동작을 확인합니다. (`localhost:8081` 또는 `http://relaydns.gosuda.org`)
5) 잘 돌아가면 저장소에 푸시합니다.

## Examples
- [simple-chat](./simple-chat/)
- [music-recommand](./music-recommand/)
- [http-backend](./http-backend/)
- [gosuda-blog](./gosuda-blog/)
- [chatter-bbs](./chatter-bbs/)

## Tips
- 기본 RelayDNS 서버 URL은 예제마다 `http://relaydns.gosuda.org`로 되어 있습니다. 디도스 공격이 되지 않도록 유의해주세요.
- 광고가 성공하면 서버 UI(관리 페이지)에서 등록된 이름(`--name`)으로 보입니다.
- Go 버전은 `go.mod` 기준(Go 1.25+)을 권장합니다. 최신 Go면 대부분 문제 없습니다.
