#!/usr/bin/env bash
set -euo pipefail
[ "${2:-3}" = 3 ] || { echo 'candidate performance requires exactly three complete repeats' >&2; exit 1; }
python3 scripts/cd/perf_protocol.py "${1:?result directory required}" --mode matrix
