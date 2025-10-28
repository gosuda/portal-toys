#!/usr/bin/env bash
set -euo pipefail

# Simple launcher: auto-detect OpenVSCode Server or code-server, then advertise via RelayDNS.
# Usage: bash vscode-relay/run.sh [--name NAME] [--server-url URL] [--port 8100]

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
NAME="vscode-relay"
SERVER_URL="http://relaydns.gosuda.org"
HOST="127.0.0.1"
PORT=8100

while [[ $# -gt 0 ]]; do
  case "$1" in
    --name) NAME="$2"; shift 2;;
    --server-url) SERVER_URL="$2"; shift 2;;
    --port) PORT="$2"; shift 2;;
    -h|--help)
      echo "Usage: bash vscode-relay/run.sh [--name NAME] [--server-url URL] [--port 8100]"; exit 0;;
    *) echo "Unknown option: $1"; exit 1;;
  esac
done

gen() {
  if command -v openssl >/dev/null 2>&1; then openssl rand -base64 18 | tr -dc 'A-Za-z0-9' | head -c 24; else date +%s%N; fi
}

# Pick IDE: prefer OpenVSCode, then code-server. If none found, try to install code-server locally.
IDE_PID=""

ensure_codeserver_installed() {
  # Try local standalone install under script directory to avoid needing sudo
  local prefix="$SCRIPT_DIR/dist"
  local target_bin="$prefix/bin/code-server"
  if [[ -x "$target_bin" ]]; then echo "$target_bin"; return 0; fi
  if command -v curl >/dev/null 2>&1; then
    echo "[install] Installing code-server standalone to $prefix" >&2
    if curl -fsSL https://code-server.dev/install.sh | sh -s -- --method standalone --prefix "$prefix" >/dev/null 2>&1; then
      if [[ -x "$target_bin" ]]; then echo "$target_bin"; return 0; fi
    fi
  fi
  # Fallback to user-local default if installer chose that
  if [[ -x "$HOME/dist/bin/code-server" ]]; then echo "$HOME/dist/bin/code-server"; return 0; fi
  # As a last resort, check brew
  if command -v brew >/dev/null 2>&1; then
    echo "[install] Installing code-server via Homebrew" >&2
    brew install code-server >/dev/null 2>&1 || true
    if command -v code-server >/dev/null 2>&1; then echo "$(command -v code-server)"; return 0; fi
  fi
  return 1
}

if command -v openvscode-server >/dev/null 2>&1 || [[ -n "${OPENVSCODE_SERVER_BIN:-}" ]]; then
  BIN="${OPENVSCODE_SERVER_BIN:-$(command -v openvscode-server)}"
  export OPENVSCODE_SERVER_CONNECTION_TOKEN="${OPENVSCODE_SERVER_CONNECTION_TOKEN:-$(gen)}"
  echo "[openvscode] token: $OPENVSCODE_SERVER_CONNECTION_TOKEN"
  "$BIN" --host "$HOST" --port "$PORT" & IDE_PID=$!
elif command -v code-server >/dev/null 2>&1 || [[ -n "${CODE_SERVER_BIN:-}" ]]; then
  BIN="${CODE_SERVER_BIN:-$(command -v code-server)}"
  export PASSWORD="${PASSWORD:-$(gen)}"
  echo "[code-server] password: $PASSWORD"
  "$BIN" --bind-addr "${HOST}:${PORT}" --auth password & IDE_PID=$!
else
  # Attempt to install code-server and run
  if BIN="$(ensure_codeserver_installed)"; then
    export PASSWORD="${PASSWORD:-$(gen)}"
    echo "[code-server] password: $PASSWORD"
    "$BIN" --bind-addr "${HOST}:${PORT}" --auth password & IDE_PID=$!
  else
    echo "No IDE available and auto-install failed. Please install openvscode-server or code-server." >&2
    exit 1
  fi
fi

# wait until port is ready (max ~30s)
wait_port() {
  local host="$1" port="$2" attempts=60
  for i in $(seq 1 "$attempts"); do
    if command -v nc >/dev/null 2>&1; then
      if nc -z "$host" "$port" 2>/dev/null; then return 0; fi
    else
      if (exec 3<>"/dev/tcp/${host}/${port}") 2>/dev/null; then exec 3>&-; return 0; fi
    fi
    sleep 0.5
  done
  return 1
}

if ! wait_port "$HOST" "$PORT"; then
  echo "Failed to detect IDE on ${HOST}:${PORT} after waiting. Exiting." >&2
  exit 1
fi

# RelayDNS advertiser: prefer local binary, else go run
if [[ -x "$SCRIPT_DIR/vscode-relay" ]]; then RELAY_CMD=("$SCRIPT_DIR/vscode-relay")
elif [[ -x "$SCRIPT_DIR/vscode-relay.exe" ]]; then RELAY_CMD=("$SCRIPT_DIR/vscode-relay.exe")
elif command -v go >/dev/null 2>&1; then RELAY_CMD=(go run ./vscode-relay)
else echo "Need relay binary or Go toolchain (go run ./vscode-relay)" >&2; exit 1; fi

cleanup(){ [[ -n "$IDE_PID" ]] && kill "$IDE_PID" 2>/dev/null || true; }
trap cleanup INT TERM EXIT

"${RELAY_CMD[@]}" --name "$NAME" --server-url "$SERVER_URL" --target-host "$HOST" --target-port "$PORT"
