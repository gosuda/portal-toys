#!/usr/bin/env bash
set -euo pipefail

# Simple ab benchmark runner for CWIST server
# Requirements: ab (apache2-utils), optionally openssl for HTTPS check

HOST="${HOST:-127.0.0.1}"
PORT="${PORT:-31744}"
PROTO="${PROTO:-http}"        # http or https
PATH_INDEX="${PATH_INDEX:-/}"
PATH_API="${PATH_API:-/api}"
PATH_POST="${PATH_POST:-/api}" # change if your POST endpoint differs

# Concurrency sweep
CONCURRENCY_LIST=(${CONCURRENCY_LIST:-"1 4 8 16 32 64"})
# Total requests per run
NREQ="${NREQ:-20000}"
# Repetitions per setting
REPEAT="${REPEAT:-3}"
# Keep-alive toggle per suite
KEEPALIVE="${KEEPALIVE:-1}"   # 1 => -k, 0 => no -k
# Timeout seconds
TIMEOUT="${TIMEOUT:-10}"
# Output dir
OUTDIR="${OUTDIR:-ab_out_$(date +%Y%m%d-%H%M%S)}"

URL_BASE="${PROTO}://${HOST}:${PORT}"

mkdir -p "${OUTDIR}"

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "Missing command: $1" >&2
    exit 1
  }
}

need_cmd ab

# Create a small JSON payload for POST
PAYLOAD_FILE="${OUTDIR}/payload.json"
cat > "${PAYLOAD_FILE}" <<'EOF'
{"op":"ping","ts":123456789,"note":"ab-test"}
EOF

common_ab_flags=()
common_ab_flags+=("-n" "${NREQ}")
common_ab_flags+=("-s" "${TIMEOUT}")
common_ab_flags+=("-r")   # Don't exit on socket receive errors
common_ab_flags+=("-S")   # Don't show progress for each request

if [[ "${KEEPALIVE}" == "1" ]]; then
  common_ab_flags+=("-k")
fi

# For HTTPS with self-signed certs, ab typically works, but if you hit TLS issues,
# consider: PROTO=https and ensure your OpenSSL/ab supports it.

run_one() {
  local name="$1"
  local url="$2"
  local c="$3"
  local extra_flags=("${@:4}")

  local out="${OUTDIR}/${name}_c${c}_n${NREQ}_k${KEEPALIVE}.txt"

  echo "[RUN] ${name} C=${c} N=${NREQ} K=${KEEPALIVE} -> ${url}"
  ab "${common_ab_flags[@]}" -c "${c}" "${extra_flags[@]}" "${url}" | tee "${out}" >/dev/null

  # Extract a few key metrics
  local rps tpr p50 p90 p99
  rps="$(grep -E 'Requests per second:' "${out}" | awk '{print $4}')"
  tpr="$(grep -E 'Time per request:' "${out}" | head -n 1 | awk '{print $4}')"  # mean per request
  p50="$(grep -E '  50% ' "${out}" | awk '{print $2}')"
  p90="$(grep -E '  90% ' "${out}" | awk '{print $2}')"
  p99="$(grep -E '  99% ' "${out}" | awk '{print $2}')"

  printf "%s\tC=%s\tRPS=%s\tTPR(ms)=%s\tP50(ms)=%s\tP90(ms)=%s\tP99(ms)=%s\n" \
    "${name}" "${c}" "${rps:-NA}" "${tpr:-NA}" "${p50:-NA}" "${p90:-NA}" "${p99:-NA}" \
    >> "${OUTDIR}/summary.tsv"
}

run_suite() {
  local suite="$1"
  local url="$2"
  shift 2
  local extra_flags=("$@")

  for c in "${CONCURRENCY_LIST[@]}"; do
    for r in $(seq 1 "${REPEAT}"); do
      run_one "${suite}_r${r}" "${url}" "${c}" "${extra_flags[@]}"
    done
  done
}

echo -e "suite\tC\tRPS\tTPR(ms)\tP50(ms)\tP90(ms)\tP99(ms)" > "${OUTDIR}/summary.tsv"

# Warm-up (short)
echo "[WARMUP]"
ab -n 2000 -c 8 -s "${TIMEOUT}" -r -S "${URL_BASE}${PATH_INDEX}" >/dev/null || true

# GET /
run_suite "GET_index" "${URL_BASE}${PATH_INDEX}"

# GET /api (expects JSON)
run_suite "GET_api" "${URL_BASE}${PATH_API}"

# POST JSON (change PATH_POST if needed)
# -p payload -T content-type
run_suite "POST_json" "${URL_BASE}${PATH_POST}" -p "${PAYLOAD_FILE}" -T "application/json"

echo
echo "[DONE] Results in: ${OUTDIR}"
echo "Summary: ${OUTDIR}/summary.tsv"
echo
echo "Tip: compare two runs by diffing summary.tsv (before vs after)."

