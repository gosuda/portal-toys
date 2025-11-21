# portal-toys

portal-toys is a collection of tiny demos that show how to build, publish, and interact with Portal relay network. All examples are intentionally simple and LLM‑friendly — you can generate or modify them with an LLM to prototype quickly.

If you’re new here, start with one example, run it locally, and (optionally) expose it via a Portal relay so others can access it.

## Why this repo (LLM‑based)
- Small, self‑contained apps that are easy for LLMs to read and change.
- Consistent CLI flags across demos: `--server-url`, `--name`, `--port`.
- Works both locally and over relays. Go demos use the Portal SDK directly; JS demos use a lightweight tunnel helper.

## Prerequisites
- A Portal relay URL (defaults provided). Learn more: https://github.com/gosuda/portal

## Quick Start

Pick one of the paths below.

- Go
  - `cd golang`
  - Start: `go run ./rolling-paper --server-url wss://portal.gosuda.org/relay,wss://portal.thumbgo.kr/relay --name my-rolling --port 8081`
  - Local access: open `http://127.0.0.1:8081`
  - Relay access: via your relay UI using the registered name (`--name`)

- JavaScript
  - `make tunnel && cd javascript/rolling-paper && npm install`
  - Start: `npm start --server-url wss://portal.gosuda.org/relay,wss://portal.thumbgo.kr/relay --name my-js-rolling --port 8082`
  - Local access: `http://127.0.0.1:8082`
  - On first run, it may ask you to install `portal-tunnel` (see Tunneling below).

- Python
  - `make tunnel && cd python/rolling-paper`
  - Start: `python main.py --server-url wss://portal.gosuda.org/relay,wss://portal.thumbgo.kr/relay --name my-py-rolling --port 8083`
  - Local access: `http://127.0.0.1:8083`
  - The script can auto-start a tunnel if `portal-tunnel` is installed (configure via `TUNNEL_ENABLED`, `TUNNEL_BIN`).

- Tunnel
  - go install gosuda.org/portal/cmd/portal-tunnel@latest
  - python3 tunnel/languagecat/main.py
  - use flag : `portal-tunnel expose -host localhost -port 8083 -relay portal.gosuda.org -name langcat`
  - use config : `portal-tunnel expose -config tunnel/languagecat/config.yaml`
## Common Flags and Environment
- `--server-url`: Relay websocket URL(s). Comma‑separated supported. Also respected from `RELAY` or `RELAY_URL`.
- `--name`: Display name for your backend on the relay.
- `--port`: Optional local HTTP port. Negative value disables local port.

## Repo Layout

Golang examples
- [golang/chatter-bbs](golang/chatter-bbs/)
- [golang/openboard](golang/openboard/)
- [golang/doom](golang/doom/)
- [golang/gosuda-blog](golang/gosuda-blog/)
- [golang/http-backend](golang/http-backend/)
- [golang/paint](golang/paint/)
- [golang/rolling-paper](golang/rolling-paper/)
- [golang/simple-chat](golang/simple-chat/)
- [golang/tetris](golang/tetris/)
- [golang/youtube-chat](golang/youtube-chat/)
- [golang/vscode-chat](golang/vscode-chat/)

JavaScript examples
- [javascript/simple-example](javascript/simple-example/)
- [javascript/rolling-paper](javascript/rolling-paper/)

Python examples
- [python/rolling-paper](python/rolling-paper/)
- [python/yt-dlp](python/yt-dlp/)

Rust
- Placeholders exist; more examples will be added over time.

## Tunneling (for JS examples)
Go demos expose directly over Portal using the SDK. JS demos use a tiny helper that shells out to the `portal-tunnel` binary.

- Install tunnel binary once: `make tunnel-install`
- Run a local tunnel manually (optional): `PORT=8080 RELAY=wss://portal.gosuda.org/relay make tunnel`
- JS rolling-paper auto‑starts the tunnel; configure with env: `TUNNEL_ENABLED`, `RELAY`, `TUNNEL_NAME`, `TUNNEL_BIN`.

## Tips
- Be considerate with traffic. Avoid excessive requests to shared relays.
- After successful connection, your service appears in the relay UI under the chosen `--name`.

## Troubleshooting
- Relay unreachable: check `--server-url` and network/firewall. Try `wss://portal.gosuda.org/relay`.
- Local port busy: change `--port` or close the conflicting process.
- JS tunnel not found: run `make tunnel-install` or set `TUNNEL_BIN`.
- Go build issues: ensure Go 1.25+; run from repo root using the per‑example path (`go run ./golang/<demo>`).

## License
MIT — see `LICENSE`.
