#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  npm run dev:tunnel -- 0xYOUR_SUI_ADDRESS
  PAY_TO=0xYOUR_SUI_ADDRESS npm run dev:tunnel

Options:
  --mainnet       Use Sui mainnet instead of testnet
  --name NAME     Portal public name, default: portal-payments-ts

Environment:
  PORT            Local app port, default: 3000
  HOST            Local bind host, default: 0.0.0.0
  PAYMENT_AMOUNT  USDC amount, default: 0.01
  PAY_TO          Sui USDC recipient address
EOF
}

MODE="testnet"
APP_NAME="${APP_NAME:-portal-payments-ts}"
PAY_TO="${PAY_TO:-}"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --mainnet)
      MODE="mainnet"
      shift
      ;;
    --testnet)
      MODE="testnet"
      shift
      ;;
    --name)
      if [ "$#" -lt 2 ]; then
        echo "--name requires a value" >&2
        exit 2
      fi
      APP_NAME="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      if [ -z "$PAY_TO" ]; then
        PAY_TO="$1"
        shift
      else
        echo "Unexpected argument: $1" >&2
        usage >&2
        exit 2
      fi
      ;;
  esac
done

if [ -z "$PAY_TO" ]; then
  echo "PAY_TO is required." >&2
  usage >&2
  exit 2
fi

if ! command -v portal >/dev/null 2>&1; then
  echo "portal CLI is not installed or not on PATH." >&2
  echo "Install: curl -fsSL https://github.com/gosuda/portal-tunnel/releases/latest/download/install.sh | bash" >&2
  exit 1
fi

if ! portal expose --help 2>&1 | grep -q -- "--x402-pay-to"; then
  echo "Your portal CLI does not support x402 paid routes." >&2
  echo "Reinstall latest: curl -fsSL https://github.com/gosuda/portal-tunnel/releases/latest/download/install.sh | bash" >&2
  echo "Current portal: $(command -v portal)" >&2
  portal version 2>/dev/null || true
  exit 1
fi

PORT="${PORT:-3000}"
HOST="${HOST:-0.0.0.0}"
PAYMENT_AMOUNT="${PAYMENT_AMOUNT:-0.01}"
PAYMENT_NETWORK="${PAYMENT_NETWORK:-}"
if [ -z "$PAYMENT_NETWORK" ] && [ "$MODE" = "testnet" ]; then
  PAYMENT_NETWORK="sui:testnet"
fi
if [ -z "$PAYMENT_NETWORK" ] && [ "$MODE" = "mainnet" ]; then
  PAYMENT_NETWORK="sui:mainnet"
fi

SERVER_PID=""
cleanup() {
  if [ -n "$SERVER_PID" ] && kill -0 "$SERVER_PID" >/dev/null 2>&1; then
    kill "$SERVER_PID" >/dev/null 2>&1 || true
    wait "$SERVER_PID" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT INT TERM

echo "[dev:tunnel] building TypeScript"
npm run build

echo "[dev:tunnel] starting local server on ${HOST}:${PORT}"
HOST="$HOST" PORT="$PORT" PAYMENT_AMOUNT="$PAYMENT_AMOUNT" PAYMENT_NETWORK="$PAYMENT_NETWORK" npm start &
SERVER_PID="$!"

echo "[dev:tunnel] waiting for http://127.0.0.1:${PORT}/api/status"
for _ in $(seq 1 80); do
  if curl -fsS "http://127.0.0.1:${PORT}/api/status" >/dev/null 2>&1; then
    break
  fi
  if ! kill -0 "$SERVER_PID" >/dev/null 2>&1; then
    echo "Local server exited before it became ready." >&2
    wait "$SERVER_PID" || true
    exit 1
  fi
  sleep 0.25
done

if ! curl -fsS "http://127.0.0.1:${PORT}/api/status" >/dev/null 2>&1; then
  echo "Local server did not become ready at http://127.0.0.1:${PORT}/api/status" >&2
  exit 1
fi

PORTAL_ARGS=(
  expose
  --name "$APP_NAME"
  --http-route "/paid=http://127.0.0.1:${PORT}/paid GET:${PAYMENT_AMOUNT}"
  --http-route "/api=http://127.0.0.1:${PORT}/api"
  --http-route "/=http://127.0.0.1:${PORT}"
  --x402-pay-to "$PAY_TO"
)

if [ "$MODE" = "testnet" ]; then
  PORTAL_ARGS+=(--x402-testnet)
fi

echo "[dev:tunnel] launching portal ${MODE} tunnel"
echo "[dev:tunnel] portal URL name: ${APP_NAME}"
portal "${PORTAL_ARGS[@]}"
