#!/usr/bin/env bash
# Run a command on the remote dev machine with a timeout and a local log.
# Usage:
#   bash remote-run.sh -- go build ./...
#   bash remote-run.sh --timeout 3600 -- make test-integration
#   bash remote-run.sh -- 'uptime; free -h'
set -euo pipefail

HOST="${REMOTE_HOST:-prod-srs}"
DIR="${REMOTE_DIR:-/home/streamgate}"
TIMEOUT="${REMOTE_TIMEOUT:-1800}"
LOG_DIR="${LOG_DIR:-build/remote}"
CMD=()

usage() {
  echo "usage: $0 [--host H] [--dir D] [--timeout SEC] -- <shell command>" >&2
  exit 2
}

while [ $# -gt 0 ]; do
  case "$1" in
    --host) HOST="$2"; shift 2 ;;
    --dir) DIR="$2"; shift 2 ;;
    --timeout) TIMEOUT="$2"; shift 2 ;;
    --) shift; CMD=("$@"); break ;;
    *) CMD=("$@"); break ;;
  esac
done

[ "${#CMD[@]}" -gt 0 ] || usage
mkdir -p "$LOG_DIR"
ts=$(date +%Y%m%d-%H%M%S)
slug=$(printf '%s' "${CMD[*]}" | tr -cs 'A-Za-z0-9._-' '-' | cut -c1-60)
log="$LOG_DIR/$ts-$slug.log"

set +e
{
  echo 'set -euo pipefail'
  echo "cd '$DIR'"
  printf '%s\n' "${CMD[*]}"
} | ssh -o BatchMode=yes -o ConnectTimeout=8 -o ServerAliveInterval=30 \
      "$HOST" "timeout $TIMEOUT bash -s" 2>&1 | tee "$log"
rc=${PIPESTATUS[1]}
set -e

echo "REMOTE_EXIT=$rc LOG=$log"
exit "$rc"
