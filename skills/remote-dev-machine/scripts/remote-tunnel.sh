#!/usr/bin/env bash
# SSH port forwards for StreamGate on the remote dev machine.
# Usage:
#   bash remote-tunnel.sh --start 9099 8080
#   bash remote-tunnel.sh --status
#   bash remote-tunnel.sh --stop
set -euo pipefail

HOST="${REMOTE_HOST:-prod-srs}"
PIDFILE="${TUNNEL_PIDFILE:-/tmp/chainpulse-tunnel.pid}"
LOGFILE="${TUNNEL_LOG:-/tmp/chainpulse-tunnel.log}"
PORTS=()

usage() {
  echo "usage: $0 --start PORT... | --stop | --status" >&2
  exit 2
}

cmd="${1:-}"
shift || true
case "$cmd" in
  --start) PORTS=("$@") ;;
  --stop|--status) ;;
  *) usage ;;
esac

if [ "$cmd" = "--start" ]; then
  [ "${#PORTS[@]}" -gt 0 ] || usage
  if [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
    echo "tunnel already running pid=$(cat "$PIDFILE"); run --stop first" >&2
    exit 1
  fi
  args=()
  for p in "${PORTS[@]}"; do
    args+=(-L "127.0.0.1:$p:127.0.0.1:$p")
  done
  if command -v setsid >/dev/null 2>&1; then
    setsid ssh -N -o BatchMode=yes -o ExitOnForwardFailure=yes \
      -o ServerAliveInterval=30 "${args[@]}" "$HOST" > "$LOGFILE" 2>&1 &
  else
    nohup ssh -N -o BatchMode=yes -o ExitOnForwardFailure=yes \
      -o ServerAliveInterval=30 "${args[@]}" "$HOST" > "$LOGFILE" 2>&1 &
  fi
  pid=$!
  echo "$pid" > "$PIDFILE"
  sleep 1
  if kill -0 "$pid" 2>/dev/null; then
    echo "tunnel started pid=$pid ports=${PORTS[*]} log=$LOGFILE"
  else
    echo "tunnel failed; see $LOGFILE" >&2
    cat "$LOGFILE" >&2 || true
    exit 1
  fi
elif [ "$cmd" = "--stop" ]; then
  if [ -f "$PIDFILE" ]; then
    pid=$(cat "$PIDFILE")
    kill "$pid" 2>/dev/null || true
    rm -f "$PIDFILE"
    echo "tunnel stopped pid=$pid"
  else
    echo "no tunnel pidfile"
  fi
elif [ "$cmd" = "--status" ]; then
  if [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
    echo "tunnel running pid=$(cat "$PIDFILE")"
  else
    echo "tunnel not running"
  fi
fi
