#!/usr/bin/env bash
# Read-only preflight for StreamGate development/acceptance machines.
# Usage: bash skills/dev-machine-acceptance/scripts/preflight.sh
set -u

fail=0
warn=0
pass()  { printf 'PASS  %s\n' "$*"; }
warnf() { printf 'WARN  %s\n' "$*"; warn=$((warn + 1)); }
bad()   { printf 'FAIL  %s\n' "$*"; fail=$((fail + 1)); }

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "$ROOT" || exit 2

echo "== StreamGate dev-machine preflight =="
echo "host: $(hostname 2>/dev/null || echo unknown)  os: $(uname -s) $(uname -m)"
echo "repo: $ROOT"
echo

# CPU
cpus=$(getconf _NPROCESSORS_ONLN 2>/dev/null || nproc 2>/dev/null || echo 0)
if [ "$cpus" -ge 8 ]; then pass "CPU: ${cpus} vCPU"
elif [ "$cpus" -ge 4 ]; then warnf "CPU: ${cpus} vCPU (4C is the floor for a single stage; 8C recommended)"
else bad "CPU: ${cpus} vCPU (need >=4 for a single stage, >=8 recommended)"; fi

# Memory
if [ -r /proc/meminfo ]; then
  mem_gib=$(awk '/MemTotal/ {printf "%d", $2/1024/1024}' /proc/meminfo)
else
  mem_gib=$(sysctl -n hw.memsize 2>/dev/null | awk '{printf "%d", $1/1024/1024/1024}')
fi
mem_gib=${mem_gib:-0}
if [ "$mem_gib" -ge 32 ]; then pass "RAM: ${mem_gib} GiB"
elif [ "$mem_gib" -ge 16 ]; then warnf "RAM: ${mem_gib} GiB (single stage ok; all/perf needs >=32)"
else bad "RAM: ${mem_gib} GiB (need >=16 for a single stage, >=32 for all/perf)"; fi

# Disk
target=/
if [ "$(uname -s)" = "Linux" ] && [ -d /home ]; then target=/home; fi
avail_gb=$(df -Pk "$target" 2>/dev/null | awk 'NR==2 {printf "%d", $4/1024/1024}')
avail_gb=${avail_gb:-0}
if [ "$avail_gb" -ge 100 ]; then pass "Disk: ${avail_gb} GiB free on $target"
elif [ "$avail_gb" -ge 50 ]; then warnf "Disk: ${avail_gb} GiB free on $target (100 recommended for all stages)"
else bad "Disk: ${avail_gb} GiB free on $target (need >=50, 100 recommended)"; fi

# Docker (required for acceptance)
if command -v docker >/dev/null 2>&1; then
  if dv=$(docker version --format '{{.Server.Version}}' 2>/dev/null) && [ -n "$dv" ]; then
    pass "Docker server: $dv"
  else
    bad "Docker CLI present but server unreachable (daemon/permissions?)"
  fi
  if docker compose version >/dev/null 2>&1; then pass "Docker compose: $(docker compose version --short 2>/dev/null || echo present)"
  else bad "Docker compose plugin: not available"; fi
else
  bad "Docker: not installed (required for make verify-docker)"
fi

# Host tools: fast pre-check only; not required by the Docker runner
if command -v go >/dev/null 2>&1; then
  gover=$(go version | sed -n 's/.*go\([0-9][0-9]*\.[0-9][0-9]*\).*/\1/p')
  if [ "${gover%%.*}" -ge 1 ] && [ "${gover#*.}" -ge 26 ] 2>/dev/null; then
    pass "Go: $(go version)"
  else
    warnf "Go: $(go version) (need go1.26.x for host pre-check; Docker runner is independent)"
  fi
else
  warnf "Go: not installed (host pre-check only; Docker runner still works)"
fi

if command -v git >/dev/null 2>&1; then pass "Git: $(git --version)"; else bad "Git: not installed (Docker runner needs it)"; fi

if command -v forge >/dev/null 2>&1 && command -v anvil >/dev/null 2>&1; then
  pass "Foundry: forge + anvil present"
else
  warnf "Foundry: forge/anvil missing (host test-anvil/contracts-test only)"
fi

if command -v ffmpeg >/dev/null 2>&1; then pass "FFmpeg: $(ffmpeg -version 2>/dev/null | head -1)"; else warnf "FFmpeg: not installed (media stage runs in Docker; host checks only)"; fi
if command -v node >/dev/null 2>&1; then
  node_major=$(node -v | sed 's/^v//' | cut -d. -f1)
  if [ "${node_major:-0}" -ge 20 ]; then pass "Node: $(node -v)"; else warnf "Node: $(node -v) (need >=20 for h5-demo)"; fi
else warnf "Node: not installed (h5-demo acceptance)"; fi
if command -v python3 >/dev/null 2>&1; then pass "Python: $(python3 --version 2>&1)"; else warnf "Python3: not installed (verify scripts)"; fi

# Repo assets
for f in Makefile docker-compose.fullchain.yml scripts/verify-docker.sh scripts/verify-controller.sh \
         deploy/docker/compose.acceptance.yml deploy/docker/compose.verify.yml \
         TRANSITION_PLAN.md skills/INDEX.md; do
  if [ -f "$f" ]; then pass "asset: $f"; else bad "asset missing: $f"; fi
done
[ -d contracts ] && pass "contracts/ present" || bad "contracts/ missing"
[ -d migrations ] && pass "migrations: $(find migrations -maxdepth 1 -name '*.sql' | wc -l | tr -d ' ') SQL files" || bad "migrations/ missing"
[ -d h5-demo ] && pass "h5-demo/ present" || warnf "h5-demo/ missing (F11)"

# Submodules
if git submodule status contracts/lib/forge-std contracts/lib/openzeppelin-contracts >/tmp/sg-submodules.$$ 2>&1; then
  if grep -q '^-' /tmp/sg-submodules.$$; then warnf "contracts submodules not initialized (run: git submodule update --init --recursive)"
  else pass "contracts submodules initialized"; fi
else
  warnf "contracts submodules status unavailable"
fi
rm -f /tmp/sg-submodules.$$

# Ports
echo
echo "-- listening ports of interest --"
if command -v ss >/dev/null 2>&1; then
  ss -tln 2>/dev/null | grep -E ':(8080|18000|5432|6379|9000|4222|8545|1926|1927|1937|8088|8089|9090)\b' || echo "none"
elif command -v lsof >/dev/null 2>&1; then
  lsof -nP -iTCP -sTCP:LISTEN 2>/dev/null | grep -E ':(8080|18000|5432|6379|9000|4222|8545|1926|1927|1937|8088|8089|9090)\b' || echo "none"
else
  echo "ss/lsof not available; check ports manually"
fi

echo
echo "NOTE: acceptance is Docker-first (make verify-docker). Host Go/Foundry are pre-check tools only."
echo "== summary: $fail FAIL, $warn WARN =="
[ "$fail" -eq 0 ] || exit 1
exit 0
