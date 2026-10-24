#!/usr/bin/env bash
set -euo pipefail

stage="${1:-all}"
project="${2:-}"
case ",${stage}," in
  *,all,*|*,perf,*|*,release,*) ;;
  *) printf '[env] stage=%s: no dedicated-runner requirement; skipping capacity gate\n' "$stage"; exit 0 ;;
esac

refuse() {
  printf '[env:FAIL] this environment cannot carry VERIFY_STAGE=%s:\n' "$stage" >&2
  printf '  - %s\n' "$@" >&2
  printf '  terminal acceptance stays BLOCKED here; use a qualified dedicated runner\n' >&2
  exit 1
}

info="$(docker info --format '{{.OSType}}|{{.Architecture}}|{{.NCPU}}|{{.MemTotal}}|{{.DockerRootDir}}|{{.OperatingSystem}}')" \
  || refuse 'could not read the Docker environment'
IFS='|' read -r os_type architecture cores memory docker_root operating_system <<< "$info"
failures=()
[ "$os_type" = linux ] || failures+=("daemon OS is '${os_type}'; Linux is required")
case "$architecture" in
  x86_64|amd64) ;;
  *) failures+=("daemon architecture is '${architecture}'; native linux/amd64 is required (no emulation)") ;;
esac
if [[ ! "$cores" =~ ^[0-9]{1,9}$ ]] || [ "$((10#${cores:-0}))" -lt 8 ]; then
  failures+=("${cores:-unknown} CPUs; at least 8 CPUs are required")
fi
if [[ ! "$memory" =~ ^[0-9]{1,18}$ ]] || [ "$((10#${memory:-0}))" -lt 34359738368 ]; then
  failures+=("${memory:-unknown} bytes RAM; at least 32 GiB are required")
fi

host_os="$(uname -s)" || refuse 'could not identify the host OS'
endpoint="${DOCKER_HOST:-}"
if [ -z "$endpoint" ]; then
  endpoint="$(docker context inspect --format '{{.Endpoints.docker.Host}}')" \
    || refuse 'could not identify the Docker endpoint'
fi
case "$endpoint" in
  unix://*) ;;
  *) failures+=('a local Docker Unix socket is required to verify its filesystem') ;;
esac
# A VM or remote daemon root is not the host filesystem. Never substitute /
# when DockerRootDir is unavailable: that would certify unrelated disk space.
if [ "$host_os" != Linux ] || [[ "$operating_system" == *'Docker Desktop'* ]]; then
  failures+=('a native Linux host is required; Docker Desktop is not a terminal runner')
elif [[ "$endpoint" == unix://* ]] && [[ "$docker_root" == /* ]] && [ -d "$docker_root" ]; then
  free_kib="$(LC_ALL=C df -Pk "$docker_root" | awk 'NR == 2 {print $4}')" \
    || refuse "could not measure the Docker filesystem (${docker_root})"
  if [[ ! "$free_kib" =~ ^[0-9]{1,18}$ ]] || [ "$((10#${free_kib:-0}))" -lt 104857600 ]; then
    failures+=("${free_kib:-unknown} KiB free on the Docker filesystem (${docker_root}); at least 100 GiB are required")
  fi
else
  failures+=("Docker filesystem is unavailable on this host (${docker_root}); disk capacity is unverified")
fi

containers="$(docker ps --format '{{.ID}}|{{.Label "com.streamgate.verify.run"}}')" \
  || refuse 'could not inspect competing containers'
while IFS='|' read -r identity owner; do
  [ -n "$identity" ] || continue
  if [ -z "$project" ] || [ "$owner" != "$project" ]; then
    failures+=("competing container ${identity} (run=${owner:-unlabelled}); a dedicated daemon is required")
  fi
done <<< "$containers"

if [ "${#failures[@]}" -ne 0 ]; then refuse "${failures[@]}"; fi
printf '[env] qualified runner: %s CPU, %s bytes RAM, %s KiB free, %s\n' \
  "$cores" "$memory" "$free_kib" "$architecture"
