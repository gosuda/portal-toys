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
  - Start: `go run ./rolling-paper --server-url portal.gosuda.org,portal.thumbgo.kr,portal.iwanhae.kr --name my-rolling --port 8081`
  - Local access: open `http://127.0.0.1:8081`
  - Relay access: via your relay UI using the registered name (`--name`)

- Tunnel
  - python3 languagecat/main.py
  - curl -fsSL http://portal.gosuda.org/tunnel | PORT=3000 NAME=languagecat sh

## Repo Layout

Golang examples
- [chatter-bbs](chatter-bbs)
- [openboard](openboard)
- [doom](doom)
- [gosuda-blog](gosuda-blog)
- [http-backend](http-backend)
- [paint](paint)
- [rolling-paper](rolling-paper)
- [simple-chat](simple-chat)
- [simple-community](simple-community)
- [tetris](tetris)
- [youtube-chat](youtube-chat)
- [vscode-chat](vscode-chat)

Tunnel examples
- [js-simple-example](js-simple-example)
- [languagecat-paper](languagecat)

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
