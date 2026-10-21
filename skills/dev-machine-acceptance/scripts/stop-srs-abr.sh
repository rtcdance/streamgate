#!/usr/bin/env bash
# Stop SRS ABR services on the 118 dev box so StreamGate acceptance can run.
# Default is dry-run. Use --yes to apply. Read-only inventory first.
#
# Usage:
#   bash scripts/stop-srs-abr.sh          # dry-run, prints the plan
#   bash scripts/stop-srs-abr.sh --yes    # stop ABR services
#
# What is stopped: abr_dev_stream.sh, rsrc_supervisor + src_resilience_proxy,
# prod_manage.sh (origin/edge/relay/transcode), ABR ffmpeg (b001/abrh/plubb),
# s0a_session_probe, and the ABR cron watchdogs (commented out).
# What is NOT stopped: nginx, srs.ingest.obs.conf, the product srsmedia process.
set -u

APPLY=0
[ "${1:-}" = "--yes" ] && APPLY=1
TRUNK="${TRUNK:-/home/srs-abr/srs/trunk}"
TS=$(date +%Y%m%d-%H%M%S)
BACKUP="/root/crontab.backup.$TS"

if [ ! -d "$TRUNK" ]; then
  echo "ERROR: $TRUNK not found; this script is for the SRS 118 dev box." >&2
  exit 2
fi

echo "target : $(hostname 2>/dev/null || echo unknown) ($TRUNK)"
echo "mode   : $([ "$APPLY" -eq 1 ] && echo APPLY || echo DRY-RUN)"
echo

if [ "$APPLY" -eq 1 ]; then
  crontab -l > "$BACKUP" 2>/dev/null || true
  echo "crontab backup: $BACKUP"
  # Comment ABR watchdogs with awk; never use a sed pipeline that can wipe crontab on failure.
  crontab -l 2>/dev/null | awk -v tag="$(date +%F)" '
    /(rsrc_watchdog|edge_manifest_watchdog|abrs_drift_watchdog|origin_watchdog|guard_storm_watchdog|s0a_tick)/ && $0 !~ /^#/ { print "# DISABLED " tag ": " $0; next }
    { print }
  ' > "/tmp/crontab.$TS.new"
  crontab "/tmp/crontab.$TS.new"
  rm -f "/tmp/crontab.$TS.new"
else
  echo "DRY-RUN: backup crontab and comment ABR watchdogs"
fi

# 1) abr_dev_stream.sh (parent + children); stop it before prod_manage,
#    otherwise its engine loop will restart the transcoder.
pids=$(pgrep -f 'abr_dev_stream\.sh' 2>/dev/null || true)
if [ -n "$pids" ]; then
  echo "abr_dev_stream pids: $pids"
  for p in $pids; do
    [ "$p" = "$$" ] && continue
    [ "$p" = "$PPID" ] && continue
    if [ "$APPLY" -eq 1 ]; then kill -TERM "$p" 2>/dev/null || true; else echo "DRY-RUN kill -TERM $p"; fi
  done
  [ "$APPLY" -eq 1 ] && sleep 2
  for p in $pids; do
    [ "$p" = "$$" ] && continue
    [ "$p" = "$PPID" ] && continue
    if [ "$APPLY" -eq 1 ]; then kill -KILL "$p" 2>/dev/null || true; else echo "DRY-RUN kill -KILL $p"; fi
  done
else
  echo "abr_dev_stream: not running"
fi

# 2) rsrc supervisor then proxy (order matters; supervisor respawns the proxy).
SPID=$(cat /var/run/srs-rsrc-supervisor.pid 2>/dev/null || true)
if [ -n "$SPID" ]; then
  echo "rsrc supervisor pid: $SPID"
  if [ "$APPLY" -eq 1 ]; then kill -TERM "$SPID" 2>/dev/null || true; sleep 2; kill -KILL "$SPID" 2>/dev/null || true
  else echo "DRY-RUN kill -TERM/-KILL $SPID"; fi
else
  echo "rsrc supervisor: not running"
fi
for p in $(pgrep -f 'src_resilience_proxy\.mjs' 2>/dev/null || true); do
  if [ "$APPLY" -eq 1 ]; then kill -9 "$p" 2>/dev/null || true; else echo "DRY-RUN kill -9 $p (src_resilience_proxy)"; fi
done

# 3) prod_manage stop all (transcode -> relay -> edge -> origin)
if [ "$APPLY" -eq 1 ]; then
  (cd "$TRUNK" && bash prod_manage.sh stop all) 2>&1 || true
else
  echo "DRY-RUN: (cd $TRUNK && bash prod_manage.sh stop all)"
fi

# 4) leftovers: match the cmdline, never a broad pkill -f.
if [ "$APPLY" -eq 1 ]; then
  for p in $(pgrep -x ffmpeg 2>/dev/null || true); do
    c=$(tr '\0' ' ' < "/proc/$p/cmdline" 2>/dev/null || true)
    case "$c" in
      *b001*|*abrh*|*filter_complex*|*plubb*) echo "kill ffmpeg $p"; kill -9 "$p" 2>/dev/null || true ;;
    esac
  done
  for p in $(pgrep -x srs 2>/dev/null || true); do
    c=$(tr '\0' ' ' < "/proc/$p/cmdline" 2>/dev/null || true)
    case "$c" in
      *srs.e2e.fmp4ws.conf*|*srs.oe.edge.conf*) echo "kill srs $p"; kill -9 "$p" 2>/dev/null || true ;;
    esac
  done
  for p in $(pgrep -f 's0a_session_probe\.mjs' 2>/dev/null || true); do
    echo "kill s0a probe $p"; kill -9 "$p" 2>/dev/null || true
  done
else
  echo "DRY-RUN: kill leftover b001/abrh/plubb ffmpeg, ABR srs, s0a probe"
fi

echo
if [ "$APPLY" -eq 1 ]; then
  echo "== verify =="
  (cd "$TRUNK" && bash prod_manage.sh status 2>&1 | head -8) || true
  ss -tln 2>/dev/null | grep -E ':(1926|1927|8088|8089)\b' || echo "1926/1927/8088/8089 free"
  echo "-- remaining ABR procs --"
  ps -eo pid,args | grep -E 'ffmpeg|objs/srs|abr_|rsrc|s0a' | grep -v grep || echo NONE
  echo "-- load --"
  uptime
  echo
  echo "restore cron : crontab $BACKUP"
  echo "restart ABR  : cd $TRUNK && bash prod_manage.sh start all && bash full_chain_restart.sh"
else
  echo "Dry-run only. Re-run with --yes to apply."
fi
