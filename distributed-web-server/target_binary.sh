#!/bin/sh
set -euo pipefail

while IFS= read -r line; do
    printf '%s\n' "${line^^}"
done
