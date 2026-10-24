#!/usr/bin/env bash
set -euo pipefail

stage="${1:-all}"
project="${2:-}"
profile="${VERIFY_ENVIRONMENT_PROFILE:-native-linux}"
case "$profile" in
  native-linux|desktop-local) ;;
  *) printf '[env:FAIL] unknown environment profile: %s\n' "$profile" >&2; exit 1 ;;
esac
case ",${stage}," in
  *,all,*|*,perf,*|*,release,*) ;;
  *) printf '[env] stage=%s: no dedicated-runner requirement; skipping capacity gate\n' "$stage"; exit 0 ;;
esac

refuse() {
  printf '[env:FAIL] this environment cannot carry VERIFY_STAGE=%s:\n' "$stage" >&2
  printf '  - %s\n' "$@" >&2
  printf '  acceptance stays BLOCKED for profile=%s; resolve the reported checks\n' "$profile" >&2
  exit 1
}

info="$(docker info --format '{{.OSType}}|{{.Architecture}}|{{.NCPU}}|{{.MemTotal}}|{{.DockerRootDir}}|{{.OperatingSystem}}')" \
  || refuse 'could not read the Docker environment'
IFS='|' read -r os_type architecture cores memory docker_root operating_system <<< "$info"
failures=()
[ "$os_type" = linux ] || failures+=("daemon OS is '${os_type}'; Linux is required")
case "$profile:$architecture" in
  native-linux:x86_64|native-linux:amd64|desktop-local:x86_64|desktop-local:amd64|desktop-local:aarch64|desktop-local:arm64) ;;
  *) failures+=("daemon architecture is '${architecture}'; native linux/amd64 is required (no emulation) outside desktop-local") ;;
esac
if [[ ! "$cores" =~ ^[0-9]{1,9}$ ]] || [ "$((10#${cores:-0}))" -lt 8 ]; then
  failures+=("${cores:-unknown} CPUs; at least 8 CPUs are required")
fi
minimum_memory=34359738368
minimum_memory_label='32 GiB'
if [ "$profile" = desktop-local ]; then
  # Desktop reports slightly less than the configured 16 GiB VM allocation.
  minimum_memory=16106127360
  minimum_memory_label='15 GiB usable (16 GiB configured VM)'
fi
if [[ ! "$memory" =~ ^[0-9]{1,18}$ ]] || [ "$((10#${memory:-0}))" -lt "$minimum_memory" ]; then
  failures+=("${memory:-unknown} bytes RAM; at least ${minimum_memory_label} are required")
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
free_kib=''
if [ "$profile" = desktop-local ]; then
  if [ "$host_os" != Darwin ] || [[ "$operating_system" != *'Docker Desktop'* ]] || [[ "$endpoint" != unix://* ]]; then
    failures+=('desktop-local requires macOS with a local Docker Desktop Unix socket')
  else
    # Measure the container backing filesystem IN the VM, never the Mac's /.
    # Reuse the digest-pinned Docker CLI image already used by Dockerfile.verify.
    probe_image='docker:28.4.0-cli@sha256:6a73c9433f2ba4279815be1e60f5739288b939dda1e48151d8c393537802de37'
    disk="$(docker run --rm --pull=missing --network none --read-only --cpus .1 --memory 32m --pids-limit 16 \
      --label "com.streamgate.verify.run=${project}" --entrypoint df "$probe_image" -Pk /)" \
      || refuse 'could not measure the Docker VM filesystem with the pinned probe'
    free_kib="$(printf '%s\n' "$disk" | awk 'NR == 2 {print $4}')"
  fi
elif [ "$host_os" != Linux ] || [[ "$operating_system" == *'Docker Desktop'* ]]; then
  failures+=('a native Linux host is required; Docker Desktop is not a terminal runner')
elif [[ "$endpoint" == unix://* ]] && [[ "$docker_root" == /* ]] && [ -d "$docker_root" ]; then
  free_kib="$(LC_ALL=C df -Pk "$docker_root" | awk 'NR == 2 {print $4}')" \
    || refuse "could not measure the Docker filesystem (${docker_root})"
else
  failures+=("Docker filesystem is unavailable on this host (${docker_root}); disk capacity is unverified")
fi
if [[ ! "$free_kib" =~ ^[0-9]{1,18}$ ]] || [ "$((10#${free_kib:-0}))" -lt 104857600 ]; then
  failures+=("${free_kib:-unknown} KiB free on the Docker filesystem (${docker_root}); at least 100 GiB are required")
fi

containers="$(docker ps --format '{{.ID}}|{{.Label "com.streamgate.verify.run"}}')" \
  || refuse 'could not inspect competing containers'
while IFS='|' read -r identity owner; do
  [ -n "$identity" ] || continue
  if [ -z "$project" ] || [ "$owner" != "$project" ]; then
    if [ "$profile" = desktop-local ] && [ -z "$owner" ]; then
      printf '[env:boundary] background container %s; performance is shared-Mac observation only\n' "$identity"
    else
      failures+=("competing container ${identity} (run=${owner:-unlabelled}); another acceptance is forbidden")
    fi
  fi
done <<< "$containers"

if [ "${#failures[@]}" -ne 0 ]; then refuse "${failures[@]}"; fi
printf '[env] qualified runner: profile=%s, %s CPU, %s bytes RAM, %s KiB free, daemon=%s, execution=%s\n' \
  "$profile" "$cores" "$memory" "$free_kib" "$architecture" "${VERIFY_PLATFORM:-linux/amd64}"
if [ "$profile" = desktop-local ]; then
  printf '[env:boundary] local Docker Desktop acceptance; emulation/shared host load may affect latency. No production capacity claim.\n'
fi
