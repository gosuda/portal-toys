#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
DIST_DIR="${REPO_ROOT}/dist"

targets=(
  "windows amd64"
  "darwin amd64"
  "darwin arm64"
  "linux amd64"
  "linux arm64"
)

mkdir -p "${DIST_DIR}"

cd "${REPO_ROOT}"

for entry in "${targets[@]}"; do
  read -r GOOS GOARCH <<<"${entry}"
  suffix=""
  if [[ "${GOOS}" == "windows" ]]; then
    suffix=".exe"
  fi
  output="${DIST_DIR}/p2p-file_${GOOS}_${GOARCH}${suffix}"
  echo "[build] ${GOOS}/${GOARCH} -> ${output}"
  GOOS="${GOOS}" GOARCH="${GOARCH}" CGO_ENABLED=0 \
    go build -trimpath -o "${output}" ./p2p-file
done

echo "Binaries written to ${DIST_DIR}"
