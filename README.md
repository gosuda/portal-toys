# portal-toys

portal-toys is a collection of small Portal demos. The repository is focused on Go examples that register directly with Portal relays using the Go SDK.

## Why This Repo
- Small, self-contained apps that are easy to read and modify.
- Consistent CLI flags across demos such as `--server-url`, `--discovery`, `--ban-mitm`, `--name`, and `--port`.
- Works both locally and over relays.

For non-Go apps, use `portal-tunnel` instead of maintaining separate language-specific examples in this repo.

## Prerequisites
- A Portal relay URL. Learn more: https://github.com/gosuda/portal

## Quick Start
- Start: `go run ./rolling-paper --name my-rolling --port 8081`
- Optional relay override: `go run ./rolling-paper --server-url https://portal.gosuda.org/ --discovery=false`
- Local access: open `http://127.0.0.1:8081`
- Relay access: open the registered name from your relay UI

## Repo Layout
- [chatter-bbs](chatter-bbs)
- [ceversi](ceversi)
- [doom](doom)
- [emulator-js](emulator-js)
- [ffmpeg-converter](ffmpeg-converter)
- [gosuda-blog](gosuda-blog)
- [http-backend](http-backend)
- [iframe-player](iframe-player)
- [mafia](mafia)
- [openboard](openboard)
- [p2p-file](p2p-file)
- [paint](paint)
- [portal-list](portal-list)
- [rolling-paper](rolling-paper)
- [simple-chat](simple-chat)
- [simple-community](simple-community)
- [tetris](tetris)
- [tools](tools)
- [vscode-chat](vscode-chat)
- [youtube-chat](youtube-chat)

## Tips
- Be considerate with traffic on shared relays.
- After successful connection, your service appears in the relay UI under the chosen `--name`.

## Troubleshooting
- Relay unreachable: check `--server-url` / `--discovery` and network/firewall.
- Local port busy: change `--port` or close the conflicting process.
- Tunnel not found: run `make tunnel-install`.
- Go build issues: ensure a recent Go toolchain and run commands from the repo root.

## License
MIT. See `LICENSE`.
